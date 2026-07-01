# Multi-Format Comics (PDF / EPUB / MOBI) Design

**Goal:** Let the library ingest and read image-based comics packaged as `.pdf`, `.epub`, and `.mobi`, alongside the existing archive formats (zip/cbz/rar/cbr/7z/cb7), with **no cgo, no external binaries, and no DB migration**.

**Architecture:** Extend the existing `internal/archive` package. Its `Reader` interface (`List() []string` of page entries, `Open(name) io.ReadCloser` → image bytes, `Close()`) is already the single seam every downstream consumer sees (cover, per-page thumbnails, the reader, the LRU reader-cache, the disk thumb-cache, the cache-sweeper). We add three **lazy** `Reader` implementations — one per format — and three cases to the `archive.Open` extension dispatch. Because content type is derived from the entry name's extension (`contentType(name)` in `fsutil.go`, default `image/jpeg`), each new reader names its page entries with the **correct image extension** for the bytes it will return. Nothing downstream changes; the node's format is still inferred from its file extension at open time, so there is no schema change.

**Tech Stack:** Go 1.24, pure-Go only. EPUB via stdlib `archive/zip` + `encoding/xml`; PDF via `github.com/pdfcpu/pdfcpu` (pure Go); MOBI via a small hand-written PalmDB record parser (stdlib only). `golang.org/x/image` (already a dependency) covers WEBP/BMP decode where needed.

## Global Constraints

- **Pure Go, single self-contained binary.** No cgo. No external binaries (no mutool/poppler/calibre). New dependencies must be pure Go. (User decision; matches the existing `modernc.org/sqlite` choice.)
- **Content is image comics only.** Each "page" is a picture. No text/reflow reader, no PDF vector rasterization.
- **No DB migration.** Format is derived from the file extension at open time, exactly as today.
- **Lazy per-page reads.** Files are large (PDF 25–125 MB, EPUB up to ~400 MB, MOBI ~45 MB). A reader must be able to report its page count and open page *k* without extracting the whole file. In particular, generating a cover at scan time must read only page 1.
- **Correct content type.** Each `Reader.List()` entry name carries the image extension matching the bytes `Open` returns (`.jpg`/`.png`/`.gif`), so `contentType(name)` is correct. Thumbnail generation sniffs bytes and does not depend on the name.
- **Graceful per-file failure.** A corrupt or unparseable file fails that one file (existing `CoverFailed` path → the book still lists with a placeholder cover; a page request 500s for that page) and never crashes the scan or the server.
- **Immutability & small files.** Follow the repo style: no in-place mutation of shared state; one focused file per reader (~200–400 lines).
- **Reuse existing infrastructure.** New readers plug into the current `ReaderCache.Acquire(ctx, key, path, mtime, cacheDir)`, the per-node `cacheDir`, the disk thumb-cache, and the cache-sweeper unchanged. PDF/MOBI memoize extracted page images under `cacheDir` so repeat reads are file reads; the sweeper already evicts it and `mtime`-keyed acquisition already self-heals staleness.

## Components

### 1. Format detection — `internal/archive/formats.go`

- Add `pdfExts`/`epubExts`/`mobiExts` (or extend one map) for `.pdf`, `.epub`, `.mobi`.
- Add `IsComic(name) bool` = `IsArchive(name)` **or** one of the three new extensions. The scanner switches from `IsArchive` to `IsComic` as its "is this a comic file?" test.
- `IsArchive` stays (still used for the archive-only dispatch branch). Existing `IsImage`/`IsJunk` unchanged.

### 2. Dispatch — `internal/archive/archive.go`

`Open(ctx, path, cacheDir)` gains three cases in its extension switch:
```
.zip/.cbz            → openZip           (existing, random access)
.rar/.cbr/.7z/.cb7   → openExtracted     (existing, extract-to-cache)
.epub                → openEPUB          (new, random access)
.pdf                 → openPDF           (new, lazy per-page extract)
.mobi                → openMOBI          (new, lazy per-record)
```
`FirstImage` is unchanged — it already opens a `Reader` and reads `List()[0]`, which for the lazy readers extracts only page 1.

### 3. EPUB reader — `internal/archive/epub.go`

EPUB is a zip. Reuse random access:
- Open the zip. Read `META-INF/container.xml` → locate the OPF (`content.opf`).
- Parse the OPF `<manifest>` (id → href, media-type) and `<spine>` (ordered `itemref idref`s). Build the page order:
  - spine item is an image media-type → use that image href directly;
  - spine item is an XHTML doc → parse it for its single `<img src>`/`<image xlink:href>` and use that image href;
  - resolve hrefs relative to the OPF's directory.
- **Fallback** (missing/unparseable OPF, or spine yields no images): natsort all image entries in the zip (same `natsort` used by archives).
- `List()` returns the ordered image entry names (real names already carry `.png`/`.jpg`). `Open(name)` reads that zip entry. `Close()` closes the zip. Fully lazy; cover reads one entry.

### 4. PDF reader — `internal/archive/pdf.go`

- On open, read the pdfcpu `Context` once (structure only, not pixels) to get the page count `N`.
- `List()` returns `N` synthetic names in document order, one entry per PDF page. The extension is chosen from each page's image-XObject **filter** in the PDF structure (cheap, no pixel decode): `DCTDecode`/`JPXDecode` → `.jpg`, otherwise → `.png`; a page with no image XObject defaults to `.jpg`. (Per the "all image comics" constraint, every page carries one full-page image; a page with no extractable image yields a broken image for that page only — acceptable and rare.)
- `Open("000k.jpg")` extracts page *k*'s embedded image with pdfcpu, **memoized** to `cacheDir/pdf/000k.<ext>`; if a page has multiple images, use the largest (full-page). Return a file reader over the cached image. The name's extension (from `List()`) and the cached file's extension agree because both come from the same page-filter inspection.
- `Close()` releases the pdfcpu context. Extraction runs at most once per page thanks to the `cacheDir` memo.

### 5. MOBI reader — `internal/archive/mobi.go`

- Parse the PalmDB header: 78-byte header + record-offset table; confirm the `BOOKMOBI` type at offset 0x3C.
- Read record 0 (the MOBI header) for the **First Image Index** and record count. Image records run from that index to the end, minus the trailing `FLIS`/`FCIS`/`SRCS`/`EOF` marker records (identified by their leading magic).
- For each image record, sniff the leading bytes (`FF D8`→JPEG, `\x89PNG`→PNG, `GIF8`→GIF); keep the ones that are images.
- `List()` returns synthetic names `"0001.jpg"` … in record order with the sniffed extension. `Open("000k.ext")` returns that record's bytes (slice into the mapped file, or memoize to `cacheDir/mobi/`). `Close()` releases the file. Header parse is cheap; cover reads one record.

## Data Flow

- **Scan/ingest** (`internal/library/scanner.go`): the walk selects files with `IsComic`; a comic node is created; the cover job calls `archive.FirstImage` → the lazy reader extracts page 1 → downscaled cover. `PageCount = len(reader.List())`. No change to node fields.
- **Cover / thumbnail / page serving** (`internal/server/api_nodes.go`): unchanged. `openPage` does `Acquire → List() → Open(names[n])`; `apiPage` streams with `contentType(name)`; `apiPageThumb` decodes bytes via `thumb.Generate` (byte-sniffing). All three formats flow through untouched.
- **Reader cache**: `Acquire` caches the open `Reader` (refcounted LRU); per-page `Open` is fast; `mtime` change re-acquires; the sweeper evicts `cacheDir`.

## Error Handling

- `archive.Open` returns a wrapped error for an unparseable/corrupt file; the scanner's existing cover-failure path marks the node and continues (book lists with placeholder). Validated at the boundary — no panics escape a reader.
- A file with zero image pages behaves like an empty archive (`FirstImage` → "no images" error; node has 0 pages), consistent with today.
- Per-page `Open` failure surfaces as a 500 for that page only; the reader-cache `Drop`/self-heal path already covers a stale `cacheDir`.
- `.qkdownloading` and other non-comic extensions are skipped by `IsComic` (extension-based), so partial downloads never ingest.

## Testing

- **Unit, per reader** (`internal/archive/*_test.go`): build a tiny fixture in-test and assert `List()` order + `Open` bytes:
  - EPUB: a minimal zip with `container.xml`, a 2-item OPF spine, and two 1×1 PNGs → asserts spine order and that fallback natsort triggers when the OPF is absent.
  - PDF: a minimal one-page PDF embedding one 1×1 JPEG (built with pdfcpu or a committed fixture) → asserts page count 1 and that `Open` returns the JPEG bytes.
  - MOBI: a minimal hand-built PalmDB with one JPEG image record → asserts detection, count, and record bytes.
- **Format detection**: `IsComic` true for the three new extensions and existing archives, false for `.txt`/`.qkdownloading`/loose images.
- **Real-file smoke** (controller-run, not in the unit suite — files are outside the repo): point a scan at `~/comic/迷宫饭` and verify a `.pdf` (`简中 话/*.pdf`), the `.epub` (`短篇集/涂鸦集…/*.epub`), and a `.mobi` (`迷宫饭漫画（mobi）/…`) each ingest, produce a cover, and serve page images end-to-end (headless-Chrome/CDP + screenshots, as with prior features).
- **Gate**: `go build/vet/test ./...`, `node --test` (unchanged), and `gofmt -l .` clean. New pure-Go deps tidied into `go.mod`/`go.sum`.

## Out of Scope

- Text / reflowable ebooks (novels): a text reader is a separate, much larger feature.
- PDF vector/text-page **rasterization** (would need MuPDF/cgo or an external tool — explicitly declined).
- Reflowable EPUB/MOBI text rendering; EPUB/MOBI DRM.
- Folder-of-loose-images as a comic (e.g. `公式书/…jpg/`) — existing/separate behavior, unchanged here.
- Any change to the reader UI, cover pipeline, or DB schema.

## Risks

- **MOBI** is the least-standard in pure Go; the hand-written parser targets the common image-MOBI (`BOOKMOBI`, First-Image-Index) layout. If a specific file's layout differs, it fails gracefully (placeholder cover) and we refine the parser against that sample. This is the task most likely to need a real-file iteration.
- **PDF** pages that are not a single embedded image (multi-image or text pages) are handled best-effort (largest image / broken page). Confirmed acceptable: the target content is scanned image comics.
- **First-scan cost**: 100+ large PDFs each generate a cover (parse + extract page 1 + downscale). Lazy reads keep this to one page per file; per-page thumbnails and full pages remain on-demand.

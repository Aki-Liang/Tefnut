# Multi-Format Comics (PDF / EPUB / MOBI) Design

**Goal:** Let the library ingest and read image-based comics packaged as `.pdf`, `.epub`, and `.mobi`, alongside the existing archive formats (zip/cbz/rar/cbr/7z/cb7), with **no cgo, no external binaries, and no DB migration**.

**Architecture:** Extend the existing `internal/archive` package. Its `Reader` interface (`List() []string` of page entries, `Open(name) io.ReadCloser` → image bytes, `Close()`) is already the single seam every downstream consumer sees (cover, per-page thumbnails, the reader, the LRU reader-cache, the disk thumb-cache, the cache-sweeper). We add three `Reader` implementations — one per format — and three cases to the `archive.Open` extension dispatch. Two families, mirroring what already exists:
- **Random-access** (like zip/cbz): **EPUB** — a zip; page images are read on demand, so the cover reads only one entry.
- **Extract-to-cache** (like rar/cbr/7z/cb7): **PDF** and **MOBI** — their page images are extracted into the node's `cacheDir` on first open, then served as files through the existing `dirReader`. (This is forced by reality: pdfcpu's per-page `ExtractPageImages` finds nothing on real scanned-manga PDFs, whereas whole-file `api.ExtractImagesFile` extracts every page image in order; see spikes. The cover therefore extracts the whole file once — a one-time scan cost identical to rar/7z today.)

Because content type is derived from the entry name's extension (`contentType(name)` in `fsutil.go`, default `image/jpeg`), each reader's page-entry names carry the **correct image extension** for the bytes returned (EPUB uses the real zip names; PDF/MOBI name the extracted files by the detected image type). Nothing downstream changes; the node's format is still inferred from its file extension at open time, so there is no schema change.

**Tech Stack:** Go 1.24, pure-Go only. EPUB via stdlib `archive/zip` + `encoding/xml`; PDF via `github.com/pdfcpu/pdfcpu` **v0.11.1** (pure Go, cgo-free; pinned to the go-1.23 line so the project's `go 1.24` floor is unchanged — v0.12.0+ require go 1.25); MOBI via a small hand-written PalmDB record parser (stdlib only). `golang.org/x/image` (already a dependency) covers WEBP/BMP decode where needed.

## Global Constraints

- **Pure Go, single self-contained binary.** No cgo. No external binaries (no mutool/poppler/calibre). New dependencies must be pure Go. (User decision; matches the existing `modernc.org/sqlite` choice.)
- **Content is image comics only.** Each "page" is a picture. No text/reflow reader, no PDF vector rasterization.
- **No DB migration.** Format is derived from the file extension at open time, exactly as today.
- **Cheap cover where the format allows.** Files are large (PDF 25–125 MB, EPUB up to ~400 MB, MOBI ~45 MB). EPUB (random-access) reads only one entry for its cover. PDF/MOBI (extract-to-cache) extract the whole file once on first open — a one-time cost identical to the existing rar/7z path — and every subsequent page/thumbnail is a cache file read.
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

### 4. PDF reader — `internal/archive/pdf.go` (extract-to-cache)

Depends on `github.com/pdfcpu/pdfcpu v0.11.1` (pure Go; the v0.11.x line targets `go 1.23`, compatible with this project's `go 1.24` — v0.12.0+ require `go 1.25`, so **pin v0.11.1**).

- `openPDF(ctx, path, cacheDir)` mirrors `openExtracted`: if `cacheDir` is empty, extract; then return a `dirReader` over `cacheDir` (shared helper — see below).
- Extraction: `api.ExtractImagesFile(path, cacheDir, nil, nil)` (nil pages = all, nil conf = default) writes every page image into `cacheDir` as `<pdfbase>_NN_image.<ext>`. Verified on a real 25-page scanned chapter: 25 JPEGs in page order. (pdfcpu's per-page `ExtractPageImages` returns nothing on these files — do **not** use it.)
- The `dirReader` walk filters `IsImage` and `SortNatural`s the names, so `_01_…_25_` land in reading order and each `.jpg`/`.png` name yields the right content type.
- Guarded by the existing "cacheDir already populated → skip" check, so extraction runs once per node.
- `Close()` releases the pdfcpu context. Extraction runs at most once per page thanks to the `cacheDir` memo.

### 5. MOBI reader — `internal/archive/mobi.go` (extract-to-cache)

Pure stdlib (`encoding/binary`, `os`) — no library.

- `openMOBI(ctx, path, cacheDir)` mirrors `openExtracted`: if `cacheDir` is empty, extract the image records; then return a `dirReader` over `cacheDir`.
- Extraction: parse the PalmDB header — confirm `BOOKMOBI` at offset 0x3C; read the record count (`uint16` big-endian at offset 76); read the record-offset table (8 bytes each from offset 78, first 4 = record data offset, big-endian). Each record spans `offset[i]..offset[i+1]` (last → EOF). For every record, sniff the leading bytes (`FF D8 FF`→jpg, `89 50 4E 47`→png, `GIF8`→gif); write each image record to `cacheDir/NNNN.<ext>` with a zero-padded running index (`0001.jpg`, `0002.jpg`, …). Non-image records (text, metadata, trailing markers) are skipped by the sniff. Verified on a real file: `BOOKMOBI`, 135 JPEG records isolated by sniffing.
- Byte-sniffing every record makes the header's First-Image-Index unnecessary and is robust to layout variation.
- The `dirReader` serves the zero-padded names in natural order.

### Shared helper — `internal/archive/archive.go`

Factor the "walk `cacheDir` for image files, `SortNatural`, return a `dirReader`" tail of `openExtracted` (currently its lines building `dirReader`) into `newDirReader(cacheDir) (Reader, error)`, and have `openExtracted`, `openPDF`, and `openMOBI` all call it. Keeps the extract-to-cache family DRY.

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
  - PDF: build a fixture in-test with pdfcpu (`api.ImportImagesFile` imports a 1×1 JPEG into a new one-page PDF), then `openPDF` it → asserts `List()` has one entry and `Open` returns image bytes.
  - MOBI: a minimal hand-built PalmDB byte slice (header + record table + one JPEG record + one non-image record) → asserts the sniffer keeps exactly the JPEG record, in order.
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

- **MOBI** is the least-standard in pure Go. The parser sniffs every PalmDB record for image magic rather than trusting header offsets — validated on a real file (135 JPEG records isolated cleanly). If a specific file stores images differently (e.g. HD-image side-record containers) it fails gracefully (placeholder cover) and we refine against that sample.
- **PDF image count/order** comes from `api.ExtractImagesFile` + `SortNatural` of the output names. If a producer emits extra images per page (masks/thumbnails) or out-of-page-order names, the page list could be off; the real-file smoke check catches this visually. (The one real chapter tested extracted exactly one image per page, in order.)
- **First-scan cost**: PDF/MOBI extract the whole file on first open (cover time), like rar/7z today — 100+ large PDFs make the initial scan I/O-heavy but it runs in the background; per-page reads afterward are cache-file reads.

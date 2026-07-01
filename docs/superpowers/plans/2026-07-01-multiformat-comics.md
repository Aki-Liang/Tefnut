# Multi-Format Comics (PDF / EPUB / MOBI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ingest and read image-based comics packaged as `.epub`, `.pdf`, and `.mobi`, alongside the existing archive formats, in pure Go with no DB change.

**Architecture:** Extend `internal/archive`. Its `Reader` interface (`List() []string`, `Open(name) (io.ReadCloser, error)`, `Close() error`) is the single seam all downstream code uses. Add three `Reader`s + three `archive.Open` dispatch cases. **EPUB** is random-access (a zip, ordered by the OPF spine, else natsort). **PDF** and **MOBI** are extract-to-cache (their page images are written into the node's `cacheDir` on first open, then served by the existing `dirReader` — mirroring rar/7z). Format is inferred from the file extension at open time, so there is no schema change.

**Tech Stack:** Go 1.24, pure Go. EPUB: stdlib `archive/zip` + `encoding/xml`. PDF: `github.com/pdfcpu/pdfcpu` **v0.11.1** (cgo-free). MOBI: hand-written PalmDB parser (stdlib). Spec: `docs/superpowers/specs/2026-07-01-multiformat-comics-design.md`.

## Global Constraints

- **Pure Go, single self-contained binary.** No cgo, no external binaries. pdfcpu **must be pinned to v0.11.1** — the v0.11.x line targets `go 1.23` (compatible with this project's `go 1.24`); v0.12.0+ require `go 1.25` and would bump the toolchain floor. Do not run `go get pdfcpu@latest`.
- **Content is image comics only.** No text/reflow reader, no PDF vector rasterization.
- **No DB migration.** Format derived from file extension at open time.
- **Content type from name:** `contentType(name)` (in `internal/server/fsutil.go`) maps by extension (default `image/jpeg`). Every `Reader.List()` entry name must carry an image extension matching its bytes. EPUB uses real zip names; PDF/MOBI name extracted files `.jpg`/`.png`/`.gif` by the detected type.
- **Graceful per-file failure.** A corrupt/unparseable file returns an error from `archive.Open`; the scanner's existing cover-failure path lists it with a placeholder — never panics, never aborts the scan.
- **Immutability & small files** (repo style): no in-place mutation of shared state; one focused file per reader.
- **Reuse existing infra:** the extract-to-cache formats plug into the current `ReaderCache.Acquire(ctx, key, path, mtime, cacheDir)`, the per-node `cacheDir`, the disk thumb-cache, and the cache-sweeper unchanged. The "cacheDir already populated → skip re-extract" guard (as in `ensureExtracted`) applies.
- **Gate every task:** `go build ./... && go vet ./... && go test ./... && gofmt -l .` (empty). `node --test internal/server/web/static/js/*.test.mjs` is unaffected but must stay green.

---

### Task 1: Format detection + scanner switch + EPUB reader

**Files:**
- Modify: `internal/archive/formats.go` (add `comicExts` map + `IsComic`)
- Modify: `internal/library/scanner.go:121` (`IsArchive` → `IsComic`)
- Modify: `internal/archive/archive.go` (dispatch `.epub`)
- Create: `internal/archive/epub.go`
- Test: `internal/archive/epub_test.go`, `internal/archive/formats_test.go` (extend if present, else create)

**Interfaces:**
- Consumes: `Reader` interface, `IsImage`, `IsJunk`, `SortNatural`, `IsArchive` (all in `internal/archive`).
- Produces:
  - `func IsComic(name string) bool`
  - `func openEPUB(epubPath string) (Reader, error)`

- [ ] **Step 1: Write the failing detection test**

Add to `internal/archive/formats_test.go` (create the file with `package archive` if it does not exist):
```go
func TestIsComic(t *testing.T) {
	cases := map[string]bool{
		"a.zip": true, "a.cbz": true, "a.rar": true, // existing archives
		"a.epub": true, "A.EPUB": true, // new (case-insensitive)
		"a.txt": false, "a.qkdownloading": false, "a.jpg": false, "dir": false,
	}
	for name, want := range cases {
		if got := IsComic(name); got != want {
			t.Errorf("IsComic(%q) = %v, want %v", name, got, want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/archive/ -run TestIsComic`
Expected: FAIL — `undefined: IsComic`.

- [ ] **Step 3: Add `comicExts` + `IsComic` to `formats.go`**

In `internal/archive/formats.go`, after the `imageExts` map, add:
```go
// comicExts holds the non-archive comic container extensions. It grows as each
// format's reader lands (epub → pdf → mobi).
var comicExts = map[string]bool{
	".epub": true,
}
```
And after `IsArchive`, add:
```go
// IsComic reports whether name is a comic container we can open — an archive or
// one of the document formats (epub/pdf/mobi).
func IsComic(name string) bool { return IsArchive(name) || comicExts[ext(name)] }
```

- [ ] **Step 4: Run detection test to verify it passes**

Run: `go test ./internal/archive/ -run TestIsComic`
Expected: PASS.

- [ ] **Step 5: Write the failing EPUB reader test**

Create `internal/archive/epub_test.go`:
```go
package archive

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func writeZip(t *testing.T, path string, entries [][2]any) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.Create(e[0].(string))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e[1].([]byte)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenEPUBSpineOrder(t *testing.T) {
	png := tinyPNG(t)
	container := []byte(`<?xml version="1.0"?><container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`)
	// spine lists img1(b.png) before img2(a.png) — reading order is the spine, NOT filename order.
	opf := []byte(`<?xml version="1.0"?><package><manifest>` +
		`<item id="img1" href="Images/b.png" media-type="image/png"/>` +
		`<item id="img2" href="Images/a.png" media-type="image/png"/>` +
		`</manifest><spine><itemref idref="img1"/><itemref idref="img2"/></spine></package>`)
	p := filepath.Join(t.TempDir(), "c.epub")
	writeZip(t, p, [][2]any{
		{"META-INF/container.xml", container},
		{"OEBPS/content.opf", opf},
		{"OEBPS/Images/a.png", png},
		{"OEBPS/Images/b.png", png},
	})

	r, err := openEPUB(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got := r.List()
	want := []string{"OEBPS/Images/b.png", "OEBPS/Images/a.png"} // spine order
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	rc, err := r.Open(got[0])
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
		t.Fatalf("page not decodable: %v", err)
	}
}

func TestOpenEPUBFallbackNatsort(t *testing.T) {
	png := tinyPNG(t)
	// no OPF → fallback to natural sort of image entries
	p := filepath.Join(t.TempDir(), "c.epub")
	writeZip(t, p, [][2]any{
		{"img/10.png", png},
		{"img/2.png", png},
		{"img/1.png", png},
	})
	r, err := openEPUB(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got := r.List()
	want := []string{"img/1.png", "img/2.png", "img/10.png"} // natural, not lexical
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("List() = %v, want %v", got, want)
		}
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/archive/ -run TestOpenEPUB`
Expected: FAIL — `undefined: openEPUB`.

- [ ] **Step 7: Implement `internal/archive/epub.go`**

```go
package archive

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// openEPUB opens an EPUB (a zip) for random access. Pages are ordered by the
// OPF spine when it lists images directly, else by natural sort of image
// entries (covers the common sequentially-named case, incl. XHTML-wrapper EPUBs).
func openEPUB(epubPath string) (Reader, error) {
	zc, err := zip.OpenReader(epubPath)
	if err != nil {
		return nil, fmt.Errorf("archive: open epub %s: %w", epubPath, err)
	}
	files := make(map[string]*zip.File, len(zc.File))
	for _, f := range zc.File {
		files[f.Name] = f
	}
	names := epubPageOrder(files)
	if len(names) == 0 {
		zc.Close()
		return nil, fmt.Errorf("archive: epub %s has no images", epubPath)
	}
	return &epubReader{zc: zc, files: files, names: names}, nil
}

type epubReader struct {
	zc    *zip.ReadCloser
	files map[string]*zip.File
	names []string
}

func (e *epubReader) List() []string { return e.names }

func (e *epubReader) Open(name string) (io.ReadCloser, error) {
	f, ok := e.files[name]
	if !ok {
		return nil, fmt.Errorf("archive: epub entry %q not found", name)
	}
	return f.Open()
}

func (e *epubReader) Close() error { return e.zc.Close() }

// epubPageOrder returns image entry names in reading order.
func epubPageOrder(files map[string]*zip.File) []string {
	if ordered := spineImages(files); len(ordered) > 0 {
		return ordered
	}
	var imgs []string
	for name := range files {
		if IsImage(name) && !IsJunk(name) {
			imgs = append(imgs, name)
		}
	}
	SortNatural(imgs)
	return imgs
}

type epubContainer struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type epubOPF struct {
	Manifest []struct {
		ID   string `xml:"id,attr"`
		Href string `xml:"href,attr"`
		Type string `xml:"media-type,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

// spineImages returns spine-ordered image hrefs, or nil if the OPF is missing,
// unparseable, or its spine lists no images.
func spineImages(files map[string]*zip.File) []string {
	opfPath := opfPathFrom(files["META-INF/container.xml"])
	if opfPath == "" {
		return nil
	}
	f, ok := files[opfPath]
	if !ok {
		return nil
	}
	var doc epubOPF
	if err := unmarshalZipXML(f, &doc); err != nil {
		return nil
	}
	base := path.Dir(opfPath)
	href := make(map[string]string, len(doc.Manifest))
	mtype := make(map[string]string, len(doc.Manifest))
	for _, it := range doc.Manifest {
		href[it.ID] = it.Href
		mtype[it.ID] = it.Type
	}
	var out []string
	for _, ref := range doc.Spine {
		h := href[ref.IDRef]
		if h == "" {
			continue
		}
		full := path.Clean(path.Join(base, h))
		if _, ok := files[full]; !ok {
			continue
		}
		if strings.HasPrefix(mtype[ref.IDRef], "image/") || IsImage(full) {
			out = append(out, full)
		}
	}
	return out
}

func opfPathFrom(f *zip.File) string {
	var c epubContainer
	if err := unmarshalZipXML(f, &c); err != nil {
		return ""
	}
	for _, rf := range c.Rootfiles {
		if rf.FullPath != "" {
			return rf.FullPath
		}
	}
	return ""
}

func unmarshalZipXML(f *zip.File, v any) error {
	if f == nil {
		return errors.New("archive: nil zip entry")
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return xml.NewDecoder(rc).Decode(v)
}
```

- [ ] **Step 8: Add the `.epub` dispatch case in `archive.go`**

In `internal/archive/archive.go`, in `Open`'s switch, add a case before `default`:
```go
	case ".epub":
		return openEPUB(archivePath)
```

- [ ] **Step 9: Switch the scanner to `IsComic`**

In `internal/library/scanner.go:121`, change:
```go
		if !isDir && !archive.IsArchive(e.Name()) {
```
to:
```go
		if !isDir && !archive.IsComic(e.Name()) {
```

- [ ] **Step 10: Run the full archive + build gate**

Run: `go test ./internal/archive/ && go build ./... && go vet ./... && gofmt -l internal/archive internal/library`
Expected: PASS; `gofmt` prints nothing.

- [ ] **Step 11: Commit**

```bash
git add internal/archive/formats.go internal/archive/epub.go internal/archive/epub_test.go internal/archive/formats_test.go internal/archive/archive.go internal/library/scanner.go
git commit -m "feat: read EPUB comics and scan by IsComic"
```

---

### Task 2: PDF reader (extract-to-cache) + pdfcpu dependency

**Files:**
- Modify: `go.mod` / `go.sum` (add pdfcpu v0.11.1)
- Modify: `internal/archive/formats.go` (add `.pdf` to `comicExts`)
- Modify: `internal/archive/archive.go` (factor `newDirReader`; dispatch `.pdf`)
- Create: `internal/archive/pdf.go`
- Test: `internal/archive/pdf_test.go`

**Interfaces:**
- Consumes: `Reader`, `dirReader`, `IsImage`, `SortNatural`, `newDirReader` (new, this task).
- Produces:
  - `func newDirReader(cacheDir string) (Reader, error)`
  - `func openPDF(ctx context.Context, pdfPath, cacheDir string) (Reader, error)`

- [ ] **Step 1: Add the pinned dependency**

Run (do NOT use `@latest`):
```bash
GOTOOLCHAIN=local go get github.com/pdfcpu/pdfcpu@v0.11.1
GOTOOLCHAIN=local go mod tidy
```
Verify `go.mod` shows `github.com/pdfcpu/pdfcpu v0.11.1` and the `go` directive is still `go 1.24` (unchanged). If `go mod tidy` tries to raise the go directive to 1.25, you fetched the wrong pdfcpu version — revert and re-pin v0.11.1.

- [ ] **Step 2: Factor `newDirReader` out of `openExtracted`**

In `internal/archive/archive.go`, replace the body of `openExtracted` (the walk that builds `dr`) so it delegates to a shared helper. Change `openExtracted` to:
```go
func openExtracted(ctx context.Context, archivePath, cacheDir string) (Reader, error) {
	if cacheDir == "" {
		return nil, errors.New("archive: cacheDir required for rar/7z")
	}
	if err := ensureExtracted(ctx, archivePath, cacheDir); err != nil {
		return nil, err
	}
	return newDirReader(cacheDir)
}

// newDirReader lists the image files under cacheDir (natural sort) and returns a
// Reader that serves them. Shared by every extract-to-cache format (rar/7z, pdf, mobi).
func newDirReader(cacheDir string) (Reader, error) {
	dr := &dirReader{dir: cacheDir}
	walkErr := filepath.WalkDir(cacheDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(cacheDir, p)
		if IsJunk(rel) || !IsImage(rel) {
			return nil
		}
		dr.names = append(dr.names, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("archive: walk cache %s: %w", cacheDir, walkErr)
	}
	SortNatural(dr.names)
	return dr, nil
}
```
(The `dirReader` type and its `List`/`Open`/`Close` methods stay where they are.)

- [ ] **Step 3: Run existing archive tests to confirm the refactor is behavior-preserving**

Run: `go test ./internal/archive/`
Expected: PASS (rar/7z tests still green — `newDirReader` is the old code, moved).

- [ ] **Step 4: Write the failing PDF reader test**

Create `internal/archive/pdf_test.go`:
```go
package archive

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestOpenPDFExtractsPages(t *testing.T) {
	dir := t.TempDir()
	// a real JPEG on disk
	jpgPath := filepath.Join(dir, "p.jpg")
	jf, err := os.Create(jpgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(jf, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	jf.Close()
	// import it into a one-page PDF (nil import cfg + nil model cfg = defaults)
	pdfPath := filepath.Join(dir, "c.pdf")
	if err := api.ImportImagesFile([]string{jpgPath}, pdfPath, nil, nil); err != nil {
		t.Fatalf("import images: %v", err)
	}

	cacheDir := filepath.Join(dir, "cache")
	r, err := openPDF(context.Background(), pdfPath, cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	names := r.List()
	if len(names) != 1 {
		t.Fatalf("List() = %v, want 1 page", names)
	}
	rc, err := r.Open(names[0])
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
		t.Fatalf("page not a decodable image: %v", err)
	}
}

func TestIsComicPDF(t *testing.T) {
	if !IsComic("x.pdf") || !IsComic("X.PDF") {
		t.Fatal("IsComic should accept .pdf")
	}
}
```

- [ ] **Step 5: Run it to verify it fails**

Run: `go test ./internal/archive/ -run 'TestOpenPDF|TestIsComicPDF'`
Expected: FAIL — `undefined: openPDF` and `IsComic(".pdf")` false.

- [ ] **Step 6: Add `.pdf` to `comicExts`**

In `internal/archive/formats.go`, extend the map:
```go
var comicExts = map[string]bool{
	".epub": true,
	".pdf":  true,
}
```

- [ ] **Step 7: Implement `internal/archive/pdf.go`**

```go
package archive

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// openPDF extracts every embedded page image from a PDF into cacheDir once, then
// serves them as files. pdfcpu's per-page ExtractPageImages misses images on many
// real scanned-manga PDFs, so we use the reliable whole-file ExtractImagesFile.
func openPDF(_ context.Context, pdfPath, cacheDir string) (Reader, error) {
	if cacheDir == "" {
		return nil, errors.New("archive: cacheDir required for pdf")
	}
	if err := ensurePDFExtracted(pdfPath, cacheDir); err != nil {
		return nil, err
	}
	return newDirReader(cacheDir)
}

// ensurePDFExtracted dumps all page images into cacheDir once; a populated
// cacheDir is treated as already extracted (same guard as ensureExtracted).
func ensurePDFExtracted(pdfPath, cacheDir string) error {
	if entries, err := os.ReadDir(cacheDir); err == nil && len(entries) > 0 {
		return nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	// nil selectedPages = all pages; nil conf = default configuration.
	if err := api.ExtractImagesFile(pdfPath, cacheDir, nil, nil); err != nil {
		return fmt.Errorf("archive: extract pdf %s: %w", pdfPath, err)
	}
	return nil
}
```

- [ ] **Step 8: Add the `.pdf` dispatch case in `archive.go`**

In `Open`'s switch, add before `default`:
```go
	case ".pdf":
		return openPDF(ctx, archivePath, cacheDir)
```

- [ ] **Step 9: Run PDF tests + gate**

Run: `go test ./internal/archive/ && go build ./... && go vet ./... && gofmt -l internal/archive`
Expected: PASS; `gofmt` prints nothing.

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum internal/archive/formats.go internal/archive/archive.go internal/archive/pdf.go internal/archive/pdf_test.go
git commit -m "feat: read PDF comics via pdfcpu image extraction"
```

---

### Task 3: MOBI reader (extract-to-cache)

**Files:**
- Modify: `internal/archive/formats.go` (add `.mobi` to `comicExts`)
- Modify: `internal/archive/archive.go` (dispatch `.mobi`)
- Create: `internal/archive/mobi.go`
- Test: `internal/archive/mobi_test.go`

**Interfaces:**
- Consumes: `Reader`, `newDirReader` (from Task 2).
- Produces:
  - `func openMOBI(ctx context.Context, mobiPath, cacheDir string) (Reader, error)`
  - `func mobiImageRecords(data []byte) ([]mobiImage, error)` (pure, unit-testable without disk)

- [ ] **Step 1: Write the failing MOBI parser test**

Create `internal/archive/mobi_test.go`:
```go
package archive

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildPalmDB assembles a minimal Palm database: 78-byte header (with the
// BOOKMOBI type at 0x3C and the record count at 76) + an 8-byte record-info
// entry per record + the record payloads concatenated.
func buildPalmDB(records [][]byte) []byte {
	n := len(records)
	headerLen := 78 + n*8
	buf := make([]byte, headerLen)
	copy(buf[0x3C:], []byte("BOOKMOBI"))
	binary.BigEndian.PutUint16(buf[76:78], uint16(n))
	off := headerLen
	for i, rec := range records {
		binary.BigEndian.PutUint32(buf[78+i*8:78+i*8+4], uint32(off))
		off += len(rec)
	}
	for _, rec := range records {
		buf = append(buf, rec...)
	}
	return buf
}

func TestMobiImageRecords(t *testing.T) {
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x11, 0x22, 0x33}
	text := []byte("this is a text/header record, not an image")
	// record 0 = text (MOBI header slot), record 1 = jpeg, record 2 = text
	data := buildPalmDB([][]byte{text, jpg, text})

	recs, err := mobiImageRecords(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 image record, got %d", len(recs))
	}
	if recs[0].ext != "jpg" {
		t.Fatalf("ext = %q, want jpg", recs[0].ext)
	}
	if !bytes.Equal(recs[0].data, jpg) {
		t.Fatalf("image bytes mismatch")
	}
}

func TestMobiRejectsNonMobi(t *testing.T) {
	if _, err := mobiImageRecords([]byte("not a palm db at all, definitely")); err == nil {
		t.Fatal("expected error for non-BOOKMOBI input")
	}
}

func TestIsComicMOBI(t *testing.T) {
	if !IsComic("x.mobi") || !IsComic("X.MOBI") {
		t.Fatal("IsComic should accept .mobi")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/archive/ -run 'TestMobi|TestIsComicMOBI'`
Expected: FAIL — `undefined: mobiImageRecords` and `IsComic(".mobi")` false.

- [ ] **Step 3: Add `.mobi` to `comicExts`**

In `internal/archive/formats.go`:
```go
var comicExts = map[string]bool{
	".epub": true,
	".pdf":  true,
	".mobi": true,
}
```

- [ ] **Step 4: Implement `internal/archive/mobi.go`**

```go
package archive

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// openMOBI extracts every embedded image record from a MOBI (a Palm database)
// into cacheDir once, then serves them as files.
func openMOBI(_ context.Context, mobiPath, cacheDir string) (Reader, error) {
	if cacheDir == "" {
		return nil, errors.New("archive: cacheDir required for mobi")
	}
	if err := ensureMOBIExtracted(mobiPath, cacheDir); err != nil {
		return nil, err
	}
	return newDirReader(cacheDir)
}

func ensureMOBIExtracted(mobiPath, cacheDir string) error {
	if entries, err := os.ReadDir(cacheDir); err == nil && len(entries) > 0 {
		return nil
	}
	data, err := os.ReadFile(mobiPath)
	if err != nil {
		return fmt.Errorf("archive: open mobi %s: %w", mobiPath, err)
	}
	recs, err := mobiImageRecords(data)
	if err != nil {
		return fmt.Errorf("archive: parse mobi %s: %w", mobiPath, err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	for i, rec := range recs {
		name := fmt.Sprintf("%04d.%s", i+1, rec.ext)
		if err := os.WriteFile(filepath.Join(cacheDir, name), rec.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

type mobiImage struct {
	ext  string
	data []byte
}

// mobiImageRecords returns a Palm-database MOBI's image records in record order,
// identified by sniffing each record's leading bytes for a known image magic.
// This does not rely on the MOBI header's First-Image-Index and is robust to
// layout variation.
func mobiImageRecords(data []byte) ([]mobiImage, error) {
	if len(data) < 78 || string(data[0x3C:0x44]) != "BOOKMOBI" {
		return nil, errors.New("archive: not a BOOKMOBI file")
	}
	n := int(binary.BigEndian.Uint16(data[76:78]))
	if n <= 0 || 78+n*8 > len(data) {
		return nil, errors.New("archive: bad mobi record count")
	}
	offs := make([]int, n)
	for i := 0; i < n; i++ {
		offs[i] = int(binary.BigEndian.Uint32(data[78+i*8 : 78+i*8+4]))
	}
	var out []mobiImage
	for i := 0; i < n; i++ {
		start := offs[i]
		end := len(data)
		if i+1 < n {
			end = offs[i+1]
		}
		if start < 0 || start > len(data) || end > len(data) || end <= start {
			continue
		}
		rec := data[start:end]
		if ext := imageMagic(rec); ext != "" {
			out = append(out, mobiImage{ext: ext, data: rec})
		}
	}
	return out, nil
}

// imageMagic returns the file extension for a byte slice that begins with a known
// image signature, or "" if it is not an image.
func imageMagic(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpg"
	case len(b) >= 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G':
		return "png"
	case len(b) >= 4 && string(b[0:4]) == "GIF8":
		return "gif"
	default:
		return ""
	}
}
```

- [ ] **Step 5: Run it to verify it passes**

Run: `go test ./internal/archive/ -run 'TestMobi|TestIsComicMOBI'`
Expected: PASS.

- [ ] **Step 6: Add the `.mobi` dispatch case in `archive.go`**

In `Open`'s switch, add before `default`:
```go
	case ".mobi":
		return openMOBI(ctx, archivePath, cacheDir)
```

- [ ] **Step 7: Full gate**

Run: `go build ./... && go vet ./... && go test ./... && gofmt -l .`
Expected: all pass; `gofmt` prints nothing.

- [ ] **Step 8: Commit**

```bash
git add internal/archive/formats.go internal/archive/archive.go internal/archive/mobi.go internal/archive/mobi_test.go
git commit -m "feat: read MOBI comics by extracting embedded image records"
```

---

## Post-plan verification (controller-run, after Task 3)

Real-file smoke on the user's library (not a unit test — files are outside the repo). Point a scan at `~/comic/迷宫饭`, then verify one file of each format ingests, gets a cover, and serves pages:
- PDF: `简中 话/*.pdf`
- EPUB: `短篇集/涂鸦集白日梦系列/*.epub`
- MOBI: `迷宫饭漫画（mobi）/…/*.mobi`

Use the headless-Chrome/CDP + screenshot harness from prior features: confirm each comic's cover renders on the browse page, the detail page's thumbnail grid populates, and the reader shows page images. Watch scan time for the 100+ large PDFs (extract-to-cache runs at cover time).

## Notes for the executor

- **pdfcpu version is load-bearing.** Pin v0.11.1. `@latest` (v0.13.0) requires go 1.25 and will bump the toolchain floor.
- **Image order for PDF/MOBI** comes from `SortNatural` of the extracted filenames (`_01_…`, `0001.jpg…`). The real-file smoke check is the visual confirmation that reading order is correct.
- **No downstream changes.** If a task tempts you to edit `internal/server/*` or the store, stop — the `Reader` interface is the only contract, and content type already flows from the entry-name extension.

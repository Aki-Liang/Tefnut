package archive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mholt/archiver/v4"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Probe returns a reader for the comic's cover (its naturally-first image)
// and the page count, WITHOUT extracting the archive to disk. It is the
// scan-time counterpart of Open: cheap metadata + cover only; full extraction
// is deferred to the first actual read.
func Probe(ctx context.Context, archivePath string) (io.ReadCloser, int, error) {
	switch ext(archivePath) {
	case ".zip", ".cbz", ".epub":
		return probeViaOpen(ctx, archivePath)
	case ".pdf":
		return probePDF(archivePath)
	case ".mobi":
		return probeMOBI(archivePath)
	default:
		return probeExtractFormat(ctx, archivePath)
	}
}

// probeViaOpen serves the random-access formats (zip/cbz/epub), which Open
// handles without a cache dir: list, then stream the first entry.
func probeViaOpen(ctx context.Context, archivePath string) (io.ReadCloser, int, error) {
	r, err := Open(ctx, archivePath, "")
	if err != nil {
		return nil, 0, err
	}
	names := r.List()
	if len(names) == 0 {
		r.Close()
		return nil, 0, fmt.Errorf("archive: %s has no images", archivePath)
	}
	rc, err := r.Open(names[0])
	if err != nil {
		r.Close()
		return nil, 0, err
	}
	// Wrap so closing the cover reader also closes the archive.
	return &closer{rc: rc, also: r}, len(names), nil
}

// errCoverFound aborts pdf image extraction as soon as the first image has
// been digested; it never escapes probePDF.
var errCoverFound = errors.New("archive: cover found")

// probePDF reads the page count from the xref and digests images in page
// order via the same reliable whole-file API openPDF's extraction uses
// (see the ExtractPageImages caveat there) — but aborts after the first
// image, so later pages are never decoded and nothing touches disk.
func probePDF(pdfPath string) (io.ReadCloser, int, error) {
	count, err := api.PageCountFile(pdfPath)
	if err != nil {
		return nil, 0, fmt.Errorf("archive: page count %s: %w", pdfPath, err)
	}
	f, err := os.Open(pdfPath)
	if err != nil {
		return nil, 0, fmt.Errorf("archive: open %s: %w", pdfPath, err)
	}
	defer f.Close()
	var cover bytes.Buffer
	found := false
	err = api.ExtractImages(f, nil, pdfCoverDigest(&cover, &found), nil)
	if err != nil && !errors.Is(err, errCoverFound) {
		return nil, 0, fmt.Errorf("archive: extract cover %s: %w", pdfPath, err)
	}
	if !found {
		return nil, 0, fmt.Errorf("archive: %s has no images", pdfPath)
	}
	return io.NopCloser(&cover), count, nil
}

// pdfCoverDigest returns an ExtractImages digest that captures the first
// renderable image into cover and aborts extraction with errCoverFound.
func pdfCoverDigest(cover *bytes.Buffer, found *bool) func(model.Image, bool, int) error {
	return func(img model.Image, _ bool, _ int) error {
		// pdfcpu hands filters it cannot render (e.g. JBIG2Decode) to the
		// digest as Image{Reader: nil}; skip them exactly like the upstream
		// WriteImageToDisk sink does — io.Copy on a nil reader panics.
		if img.Reader == nil {
			return nil
		}
		if _, err := io.Copy(cover, img); err != nil {
			return err
		}
		*found = true
		return errCoverFound
	}
}

// probeMOBI parses the Palm database in memory: count the image records and
// return the first one as the cover.
func probeMOBI(mobiPath string) (io.ReadCloser, int, error) {
	data, err := os.ReadFile(mobiPath)
	if err != nil {
		return nil, 0, fmt.Errorf("archive: open mobi %s: %w", mobiPath, err)
	}
	recs, err := mobiImageRecords(data)
	if err != nil {
		return nil, 0, fmt.Errorf("archive: parse mobi %s: %w", mobiPath, err)
	}
	if len(recs) == 0 {
		return nil, 0, fmt.Errorf("archive: %s has no images", mobiPath)
	}
	return io.NopCloser(bytes.NewReader(recs[0].data)), len(recs), nil
}

// probeExtractFormat handles the formats without random access (rar/cbr,
// 7z/cb7) in two streaming passes: list the image entry names without touching
// their data, then extract only the naturally-first entry — into memory.
func probeExtractFormat(ctx context.Context, archivePath string) (io.ReadCloser, int, error) {
	var names []string
	err := walkArchive(ctx, archivePath, nil, func(_ context.Context, f archiver.File) error {
		if f.IsDir() || IsJunk(f.NameInArchive) || !IsImage(f.NameInArchive) {
			return nil
		}
		names = append(names, f.NameInArchive)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if len(names) == 0 {
		return nil, 0, fmt.Errorf("archive: %s has no images", archivePath)
	}
	SortNatural(names)
	first := names[0]

	var cover bytes.Buffer
	found := false
	err = walkArchive(ctx, archivePath, []string{first}, func(_ context.Context, f archiver.File) error {
		if f.IsDir() || f.NameInArchive != first {
			return nil
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		if _, err := io.Copy(&cover, rc); err != nil {
			return err
		}
		found = true
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if !found {
		return nil, 0, fmt.Errorf("archive: cover entry %q vanished from %s between passes", first, archivePath)
	}
	return io.NopCloser(&cover), len(names), nil
}

// walkArchive identifies archivePath's format and streams its entries (all of
// them, or just pathsInArchive when non-nil) through handle. Nothing is
// written to disk unless handle does so itself.
func walkArchive(ctx context.Context, archivePath string, pathsInArchive []string, handle archiver.FileHandler) error {
	src, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("archive: open %s: %w", archivePath, err)
	}
	defer src.Close()
	format, reader, err := archiver.Identify(filepath.Base(archivePath), src)
	if err != nil {
		return fmt.Errorf("archive: identify %s: %w", archivePath, err)
	}
	ex, ok := format.(archiver.Extractor)
	if !ok {
		return fmt.Errorf("archive: %s not extractable", archivePath)
	}
	return ex.Extract(ctx, reader, pathsInArchive, handle)
}

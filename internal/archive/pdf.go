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

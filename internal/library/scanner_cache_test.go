package library

import (
	"context"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"Tefnut/internal/store"
)

// writeTestPDF builds a PDF at path with one page per JPEG in pages.
func writeTestPDF(t *testing.T, path string, pages int) {
	t.Helper()
	dir := t.TempDir()
	var imgs []string
	for i := range pages {
		p := filepath.Join(dir, "p"+itoa(int64(i))+".jpg")
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
			t.Fatal(err)
		}
		f.Close()
		imgs = append(imgs, p)
	}
	if err := api.ImportImagesFile(imgs, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

// TestScanDoesNotPopulateExtractCache reproduces the "whole comic cached
// without ever being read" bug: building a cover during scan must not leave a
// populated data/cache/<id> extract dir behind — full extraction belongs to
// the first actual read, not to the scan.
func TestScanDoesNotPopulateExtractCache(t *testing.T) {
	ctx := context.Background()
	sc, repo, paths, data := newTestScanner(t)
	libDir := t.TempDir()
	writeTestPDF(t, filepath.Join(libDir, "c.pdf"), 2)
	if _, err := paths.Add(ctx, "lib", libDir); err != nil {
		t.Fatal(err)
	}

	if err := sc.Scan(ctx); err != nil {
		t.Fatal(err)
	}

	comics, err := repo.ListChildren(ctx, rootID(t, repo, libDir), -1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(comics) != 1 {
		t.Fatalf("comics = %d, want 1", len(comics))
	}
	comic := comics[0]
	if comic.PageCount != 2 {
		t.Errorf("page count = %d, want 2", comic.PageCount)
	}
	if comic.CoverStatus != store.CoverReady {
		t.Errorf("cover status = %d, want ready", comic.CoverStatus)
	}
	if _, err := os.Stat(filepath.Join(data, "thumbs", itoa(comic.ID)+".jpg")); err != nil {
		t.Errorf("cover thumb not written: %v", err)
	}

	// The key assertion: scanning must not fill the extract cache.
	cacheDir := filepath.Join(data, "cache", itoa(comic.ID))
	if entries, err := os.ReadDir(cacheDir); err == nil && len(entries) > 0 {
		t.Fatalf("scan populated extract cache %s with %d file(s); extraction must be deferred to first read", cacheDir, len(entries))
	}
}

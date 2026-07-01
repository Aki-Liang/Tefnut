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

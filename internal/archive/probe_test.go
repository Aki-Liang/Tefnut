package archive

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestProbeZip(t *testing.T) {
	dir := t.TempDir()
	zp := makeZip(t, dir, "c.zip", map[string]string{"2.jpg": "two", "1.jpg": "one"})
	rc, count, err := Probe(context.Background(), zp)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != "one" {
		t.Fatalf("cover = %q, want naturally-first entry %q", b, "one")
	}
}

func TestProbeZipNoImages(t *testing.T) {
	dir := t.TempDir()
	zp := makeZip(t, dir, "c.zip", map[string]string{"notes.txt": "x"})
	if _, _, err := Probe(context.Background(), zp); err == nil {
		t.Fatal("expected error for archive without images")
	}
}

func TestProbePDF(t *testing.T) {
	imgDir := t.TempDir()
	var imgs []string
	for i := range 2 {
		p := filepath.Join(imgDir, fmt.Sprintf("p%d.jpg", i))
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
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "c.pdf")
	if err := api.ImportImagesFile(imgs, pdfPath, nil, nil); err != nil {
		t.Fatalf("import images: %v", err)
	}

	rc, count, err := Probe(context.Background(), pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if count != 2 {
		t.Fatalf("count = %d, want 2 (pdf page count)", count)
	}
	b, _ := io.ReadAll(rc)
	if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
		t.Fatalf("cover not a decodable image: %v", err)
	}
	// Probing must not extract anything next to the pdf (or anywhere else).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("probe wrote files to disk: %v", entries)
	}
}

func TestProbeMOBI(t *testing.T) {
	jpg1 := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x01}
	jpg2 := []byte{0xFF, 0xD8, 0xFF, 0xE1, 0x02}
	text := []byte("mobi header record, not an image")
	dir := t.TempDir()
	p := filepath.Join(dir, "c.mobi")
	if err := os.WriteFile(p, buildPalmDB([][]byte{text, jpg1, jpg2}), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, count, err := Probe(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	b, _ := io.ReadAll(rc)
	if !bytes.Equal(b, jpg1) {
		t.Fatalf("cover bytes = %v, want first image record %v", b, jpg1)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("probe wrote files to disk: %v", entries)
	}
}

// TestProbeArchiverBranch exercises the extract-format (rar/cbr/7z) branch.
// The fixture is zip content named .cbr: archiver identifies formats by
// stream magic, so it takes the same code path a real cbr would.
func TestProbeArchiverBranch(t *testing.T) {
	dir := t.TempDir()
	zp := makeZip(t, dir, "c.cbr", map[string]string{"10.jpg": "ten", "2.jpg": "two", "notes.txt": "junk"})
	rc, count, err := Probe(context.Background(), zp)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if count != 2 {
		t.Fatalf("count = %d, want 2 (non-image entries excluded)", count)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != "two" {
		t.Fatalf("cover = %q, want naturally-first entry %q", b, "two")
	}
	// Probing must not extract anything to disk.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("probe wrote files to disk: %v", entries)
	}
}

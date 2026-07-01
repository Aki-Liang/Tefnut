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
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
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
	if len(got) != len(want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("List() = %v, want %v", got, want)
		}
	}
}

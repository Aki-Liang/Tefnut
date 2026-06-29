package thumb

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func samplePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGenerateScalesWidth(t *testing.T) {
	src := samplePNG(t, 800, 1200)
	out, err := Generate(bytes.NewReader(src), 400)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output not jpeg: %v", err)
	}
	if img.Bounds().Dx() != 400 {
		t.Errorf("width = %d, want 400", img.Bounds().Dx())
	}
	if img.Bounds().Dy() != 600 {
		t.Errorf("height = %d, want 600 (proportional)", img.Bounds().Dy())
	}
}

func TestGenerateRejectsGarbage(t *testing.T) {
	if _, err := Generate(bytes.NewReader([]byte("not an image")), 400); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestPixelBudget(t *testing.T) {
	if withinPixelBudget(10000, 10000) { // 100,000,000 px == 100MP, over budget
		t.Fatal("100MP should be over budget")
	}
	if !withinPixelBudget(4000, 6000) { // 24MP, fine
		t.Fatal("24MP should be within budget")
	}
}

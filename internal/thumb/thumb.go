package thumb

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

func init() {
	// bmp registers itself via blank import below; reference to silence unused.
	_ = bmp.Decode
}

// maxPixels caps the source image area we will decode (guards against
// decompression/pixel-flood bombs). 80 MP comfortably exceeds real comic pages.
const maxPixels = 80 * 1000 * 1000

func withinPixelBudget(w, h int) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	return int64(w)*int64(h) <= maxPixels
}

// Generate decodes src, scales it to width px wide (preserving aspect ratio),
// and returns JPEG-encoded bytes. It rejects images whose pixel count exceeds
// maxPixels to guard against decompression-bomb attacks.
func Generate(src io.Reader, width int) ([]byte, error) {
	if width <= 0 {
		return nil, fmt.Errorf("thumb: width must be > 0")
	}
	raw, err := io.ReadAll(src)
	if err != nil {
		return nil, fmt.Errorf("thumb: read source: %w", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("thumb: decode config: %w", err)
	}
	if !withinPixelBudget(cfg.Width, cfg.Height) {
		return nil, fmt.Errorf("thumb: image too large (%dx%d)", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("thumb: decode: %w", err)
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return nil, fmt.Errorf("thumb: empty image")
	}
	height := int(float64(width) * float64(b.Dy()) / float64(b.Dx()))
	if height < 1 {
		height = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("thumb: encode: %w", err)
	}
	return buf.Bytes(), nil
}

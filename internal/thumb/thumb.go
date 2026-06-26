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

// Generate decodes src, scales it to width px wide (preserving aspect ratio),
// and returns JPEG-encoded bytes.
func Generate(src io.Reader, width int) ([]byte, error) {
	if width <= 0 {
		return nil, fmt.Errorf("thumb: width must be > 0")
	}
	img, _, err := image.Decode(src)
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

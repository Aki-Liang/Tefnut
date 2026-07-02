package archive

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// TestPDFCoverDigestSkipsNilReader reproduces the JBIG2 crash: pdfcpu's
// RenderImage yields model.Image{Reader: nil} for filters it cannot render
// (e.g. JBIG2Decode), and upstream WriteImageToDisk skips those. The probe
// digest must skip them too instead of panicking in io.Copy — a panic here
// happens inside a scan worker goroutine and would kill the whole process.
func TestPDFCoverDigestSkipsNilReader(t *testing.T) {
	var cover bytes.Buffer
	var found bool
	digest := pdfCoverDigest(&cover, &found)

	if err := digest(model.Image{}, false, 0); err != nil {
		t.Fatalf("nil-reader image must be skipped, got err %v", err)
	}
	if found || cover.Len() != 0 {
		t.Fatalf("nil-reader image must not count as cover (found=%v len=%d)", found, cover.Len())
	}

	// The next renderable image becomes the cover and aborts extraction.
	img := model.Image{Reader: strings.NewReader("jpegbytes")}
	err := digest(img, false, 0)
	if !errors.Is(err, errCoverFound) {
		t.Fatalf("renderable image should abort with errCoverFound, got %v", err)
	}
	if !found || cover.String() != "jpegbytes" {
		t.Fatalf("cover not captured (found=%v cover=%q)", found, cover.String())
	}
}

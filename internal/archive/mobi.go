package archive

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// openMOBI extracts every embedded image record from a MOBI (a Palm database)
// into cacheDir once, then serves them as files.
func openMOBI(_ context.Context, mobiPath, cacheDir string) (Reader, error) {
	if cacheDir == "" {
		return nil, errors.New("archive: cacheDir required for mobi")
	}
	if err := ensureMOBIExtracted(mobiPath, cacheDir); err != nil {
		return nil, err
	}
	return newDirReader(cacheDir)
}

func ensureMOBIExtracted(mobiPath, cacheDir string) error {
	if entries, err := os.ReadDir(cacheDir); err == nil && len(entries) > 0 {
		return nil
	}
	data, err := os.ReadFile(mobiPath)
	if err != nil {
		return fmt.Errorf("archive: open mobi %s: %w", mobiPath, err)
	}
	recs, err := mobiImageRecords(data)
	if err != nil {
		return fmt.Errorf("archive: parse mobi %s: %w", mobiPath, err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("archive: mkdir cache %s: %w", cacheDir, err)
	}
	for i, rec := range recs {
		name := fmt.Sprintf("%04d.%s", i+1, rec.ext)
		dst := filepath.Join(cacheDir, name)
		if err := os.WriteFile(dst, rec.data, 0o644); err != nil {
			return fmt.Errorf("archive: write %s: %w", dst, err)
		}
	}
	return nil
}

type mobiImage struct {
	ext  string
	data []byte
}

// mobiImageRecords returns a Palm-database MOBI's image records in record order,
// identified by sniffing each record's leading bytes for a known image magic.
// This does not rely on the MOBI header's First-Image-Index and is robust to
// layout variation.
func mobiImageRecords(data []byte) ([]mobiImage, error) {
	if len(data) < 78 || string(data[0x3C:0x44]) != "BOOKMOBI" {
		return nil, errors.New("archive: not a BOOKMOBI file")
	}
	n := int(binary.BigEndian.Uint16(data[76:78]))
	if n <= 0 || 78+n*8 > len(data) {
		return nil, errors.New("archive: bad mobi record count")
	}
	offs := make([]int, n)
	for i := 0; i < n; i++ {
		offs[i] = int(binary.BigEndian.Uint32(data[78+i*8 : 78+i*8+4]))
	}
	var out []mobiImage
	for i := 0; i < n; i++ {
		start := offs[i]
		end := len(data)
		if i+1 < n {
			end = offs[i+1]
		}
		if start < 0 || start > len(data) || end > len(data) || end <= start {
			continue
		}
		rec := data[start:end]
		if ext := imageMagic(rec); ext != "" {
			out = append(out, mobiImage{ext: ext, data: rec})
		}
	}
	return out, nil
}

// imageMagic returns the file extension for a byte slice that begins with a known
// image signature, or "" if it is not an image.
func imageMagic(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpg"
	case len(b) >= 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G':
		return "png"
	case len(b) >= 4 && string(b[0:4]) == "GIF8":
		return "gif"
	default:
		return ""
	}
}

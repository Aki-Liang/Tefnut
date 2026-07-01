package archive

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildPalmDB assembles a minimal Palm database: 78-byte header (with the
// BOOKMOBI type at 0x3C and the record count at 76) + an 8-byte record-info
// entry per record + the record payloads concatenated.
func buildPalmDB(records [][]byte) []byte {
	n := len(records)
	headerLen := 78 + n*8
	buf := make([]byte, headerLen)
	copy(buf[0x3C:], []byte("BOOKMOBI"))
	binary.BigEndian.PutUint16(buf[76:78], uint16(n))
	off := headerLen
	for i, rec := range records {
		binary.BigEndian.PutUint32(buf[78+i*8:78+i*8+4], uint32(off))
		off += len(rec)
	}
	for _, rec := range records {
		buf = append(buf, rec...)
	}
	return buf
}

func TestMobiImageRecords(t *testing.T) {
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x11, 0x22, 0x33}
	text := []byte("this is a text/header record, not an image")
	// record 0 = text (MOBI header slot), record 1 = jpeg, record 2 = text
	data := buildPalmDB([][]byte{text, jpg, text})

	recs, err := mobiImageRecords(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 image record, got %d", len(recs))
	}
	if recs[0].ext != "jpg" {
		t.Fatalf("ext = %q, want jpg", recs[0].ext)
	}
	if !bytes.Equal(recs[0].data, jpg) {
		t.Fatalf("image bytes mismatch")
	}
}

func TestMobiRejectsNonMobi(t *testing.T) {
	if _, err := mobiImageRecords([]byte("not a palm db at all, definitely")); err == nil {
		t.Fatal("expected error for non-BOOKMOBI input")
	}
}

func TestIsComicMOBI(t *testing.T) {
	if !IsComic("x.mobi") || !IsComic("X.MOBI") {
		t.Fatal("IsComic should accept .mobi")
	}
}

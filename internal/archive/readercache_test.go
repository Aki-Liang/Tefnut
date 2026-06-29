package archive

import (
	"context"
	"io"
	"testing"
)

func TestReaderCacheReusesAndRefcounts(t *testing.T) {
	dir := t.TempDir()
	zpath := makeZip(t, dir, "a.zip", map[string]string{
		"01.jpg": "x",
		"02.jpg": "y",
		"03.jpg": "z",
	})

	c := NewReaderCache(2)
	defer c.Close()
	ctx := context.Background()

	r1, rel1, err := c.Acquire(ctx, "1", zpath, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.List()) != 3 {
		t.Fatalf("list = %d, want 3", len(r1.List()))
	}
	// Second acquire of the same key+mtime returns the SAME underlying reader.
	r2, rel2, err := c.Acquire(ctx, "1", zpath, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 {
		t.Fatal("expected cache hit to reuse the reader")
	}
	// Read a page fully while both refs are held.
	rc, err := r2.Open(r2.List()[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatal(err)
	}
	rc.Close()
	rel1()
	rel2()

	// A changed mtime forces a reopen (new reader instance).
	r3, rel3, err := c.Acquire(ctx, "1", zpath, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	if r3 == r1 {
		t.Fatal("expected reopen on mtime change")
	}
	rel3()
}

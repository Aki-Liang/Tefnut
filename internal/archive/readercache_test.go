package archive

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
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

// fakeReader records Close calls so Drop's refcount invariant can be asserted
// deterministically without depending on real archive Close side effects.
type fakeReader struct{ closes int }

func (f *fakeReader) List() []string                     { return nil }
func (f *fakeReader) Open(string) (io.ReadCloser, error) { return nil, nil }
func (f *fakeReader) Close() error                       { f.closes++; return nil }

// TestReaderCacheDropHonorsRefcount proves Drop never closes a reader that
// still has a ref in flight: the entry is removed from the cache immediately,
// but the underlying reader is closed only by its last releaser.
func TestReaderCacheDropHonorsRefcount(t *testing.T) {
	c := NewReaderCache(2)
	fr := &fakeReader{}

	// Insert an entry exactly as Acquire would (white-box), holding one ref.
	c.mu.Lock()
	e := &cacheEntry{key: "k", mtime: 1, reader: fr, refs: 1}
	e.elem = c.lru.PushFront(e)
	c.entries["k"] = e
	c.mu.Unlock()
	rel := c.releaser(e)

	// Drop with the ref still held: the reader must NOT be closed yet.
	c.Drop("k")
	if fr.closes != 0 {
		t.Fatalf("Drop closed the reader with a ref in flight: closes=%d", fr.closes)
	}
	// The entry must be gone from the map so the next Acquire re-opens.
	c.mu.Lock()
	_, present := c.entries["k"]
	c.mu.Unlock()
	if present {
		t.Fatal("Drop left the entry in the cache map")
	}

	// Releasing the last ref closes the reader exactly once.
	rel()
	if fr.closes != 1 {
		t.Fatalf("last release should close exactly once, closes=%d", fr.closes)
	}
}

func TestAcquireSingleFlight(t *testing.T) {
	dir := t.TempDir()
	// build a 4-page PDF (4 imported JPEGs) → extract-to-cache path
	var jpgs []string
	for i := 0; i < 4; i++ {
		p := filepath.Join(dir, fmt.Sprintf("p%d.jpg", i))
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
			t.Fatal(err)
		}
		f.Close()
		jpgs = append(jpgs, p)
	}
	pdfPath := filepath.Join(dir, "c.pdf")
	if err := api.ImportImagesFile(jpgs, pdfPath, nil, nil); err != nil {
		t.Fatalf("import: %v", err)
	}
	cacheDir := filepath.Join(dir, "cache")

	cache := NewReaderCache(4)
	defer cache.Close()

	const N = 8
	readers := make([]Reader, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, rel, err := cache.Acquire(context.Background(), "k", pdfPath, 1, cacheDir)
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			defer rel()
			readers[i] = r
			if got := len(r.List()); got != 4 {
				t.Errorf("goroutine %d: List len = %d, want 4", i, got)
			}
		}(i)
	}
	wg.Wait()

	uniq := map[Reader]bool{}
	for _, r := range readers {
		if r != nil {
			uniq[r] = true
		}
	}
	if len(uniq) != 1 {
		t.Fatalf("expected 1 shared reader (single-flight), got %d distinct — Open ran multiple times", len(uniq))
	}
}

// TestReaderCacheDropForcesReopen proves that after Drop the next Acquire for
// the same key+mtime opens a FRESH reader, and that a reader dropped while a ref
// is in flight stays usable (never closed mid-stream).
func TestReaderCacheDropForcesReopen(t *testing.T) {
	dir := t.TempDir()
	zpath := makeZip(t, dir, "a.zip", map[string]string{"01.jpg": "x", "02.jpg": "y"})

	c := NewReaderCache(2)
	defer c.Close()
	ctx := context.Background()

	r1, rel1, err := c.Acquire(ctx, "1", zpath, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	// Drop the entry while the ref is in flight, then keep reading from r1: it
	// must still work (the reader is not closed until rel1 runs).
	c.Drop("1")
	rc, err := r1.Open(r1.List()[0])
	if err != nil {
		t.Fatalf("reader closed mid-stream after Drop: %v", err)
	}
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("read after Drop failed: %v", err)
	}
	rc.Close()

	// The next Acquire re-extracts: a fresh reader instance, not the dropped one.
	r2, rel2, err := c.Acquire(ctx, "1", zpath, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if r1 == r2 {
		t.Fatal("expected a fresh reader after Drop")
	}
	rel1()
	rel2()
}

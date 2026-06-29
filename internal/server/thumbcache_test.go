package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestThumbCacheMemAndDisk(t *testing.T) {
	dir := t.TempDir()
	c, err := newThumbCache(2, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.put("1:0", []byte("aaa")); err != nil {
		t.Fatal(err)
	}
	// Hot read.
	if b, ok := c.get("1:0"); !ok || string(b) != "aaa" {
		t.Fatalf("mem get = %q,%v", b, ok)
	}
	// Persisted to disk at the expected path.
	if _, err := os.Stat(filepath.Join(dir, "pages", "1", "0.jpg")); err != nil {
		t.Fatalf("expected disk file: %v", err)
	}
	// A fresh cache (cold memory) still finds it on disk.
	c2, err := newThumbCache(2, dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := c2.get("1:0"); !ok || string(b) != "aaa" {
		t.Fatalf("disk get = %q,%v", b, ok)
	}
}

func TestThumbCacheLRUEvictsMemNotDisk(t *testing.T) {
	dir := t.TempDir()
	c, _ := newThumbCache(1, dir)
	_ = c.put("1:0", []byte("a"))
	_ = c.put("1:1", []byte("b")) // evicts 1:0 from memory
	// 1:0 gone from memory but still served from disk.
	if b, ok := c.get("1:0"); !ok || string(b) != "a" {
		t.Fatalf("evicted-but-on-disk get = %q,%v", b, ok)
	}
}

package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClearRemovesAllAndReportsFreed(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, n int) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("1/a.jpg", 100)
	mk("1/b.jpg", 200)
	mk("2/a.jpg", 300)
	mk("stray.txt", 50) // top-level file, not a dir — must be removed too

	freed, err := Clear(root)
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if freed != 650 {
		t.Errorf("freed = %d, want 650", freed)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("root itself must survive: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("root not empty after Clear: %v", entries)
	}
}

func TestClearMissingRootNoop(t *testing.T) {
	freed, err := Clear(filepath.Join(t.TempDir(), "nope"))
	if err != nil || freed != 0 {
		t.Fatalf("missing root: freed=%d err=%v, want 0, nil", freed, err)
	}
}

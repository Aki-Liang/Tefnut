package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeDir(t *testing.T, root, name string, bytes int, mod time.Time) {
	t.Helper()
	d := filepath.Join(root, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "data"), make([]byte, bytes), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(d, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestEnforceEvictsOldestUntilUnderBudget(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeDir(t, root, "1", 1000, now.Add(-3*time.Hour)) // oldest
	writeDir(t, root, "2", 1000, now.Add(-2*time.Hour))
	writeDir(t, root, "3", 1000, now.Add(-1*time.Hour)) // newest
	evicted, err := Enforce(root, 2200)                 // keep ~2 dirs
	if err != nil {
		t.Fatal(err)
	}
	if evicted != 1 {
		t.Fatalf("evicted = %d, want 1", evicted)
	}
	if _, err := os.Stat(filepath.Join(root, "1")); !os.IsNotExist(err) {
		t.Fatal("oldest dir should be evicted")
	}
	if _, err := os.Stat(filepath.Join(root, "3")); err != nil {
		t.Fatal("newest dir should remain")
	}
}

func TestEnforceNoopWhenUnderBudget(t *testing.T) {
	root := t.TempDir()
	writeDir(t, root, "1", 100, time.Now())
	n, err := Enforce(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("evicted = %d, want 0", n)
	}
}

func TestEnforceDisabledWhenMaxBytesNonPositive(t *testing.T) {
	root := t.TempDir()
	writeDir(t, root, "1", 1<<20, time.Now()) // 1 MiB, well over a tiny budget
	for _, max := range []int64{0, -1} {
		n, err := Enforce(root, max)
		if err != nil {
			t.Fatalf("maxBytes=%d: %v", max, err)
		}
		if n != 0 {
			t.Fatalf("maxBytes=%d: evicted = %d, want 0 (eviction disabled)", max, n)
		}
		if _, err := os.Stat(filepath.Join(root, "1")); err != nil {
			t.Fatalf("maxBytes=%d: dir should be untouched: %v", max, err)
		}
	}
}

func TestEnforceMissingRootIsNoop(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	n, err := Enforce(missing, 1<<10)
	if err != nil {
		t.Fatalf("missing root should be a no-op, got err: %v", err)
	}
	if n != 0 {
		t.Fatalf("evicted = %d, want 0", n)
	}
}

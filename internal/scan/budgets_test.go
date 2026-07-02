package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeDirOfSize creates dir with a single file of n bytes and sets its mtime.
func writeDirOfSize(t *testing.T, dir string, n int, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "0.jpg"), make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// TestRunScanEnforcesPageThumbBudget: page thumbnails under data/thumbs/pages
// accumulate forever otherwise — after a scan the manager must evict
// oldest-modified per-comic thumb dirs until the budget holds, exactly like
// the extract cache.
func TestRunScanEnforcesPageThumbBudget(t *testing.T) {
	settings, paths := newRepos(t)
	dataDir := t.TempDir()
	old := filepath.Join(dataDir, "thumbs", "pages", "1")
	fresh := filepath.Join(dataDir, "thumbs", "pages", "2")
	writeDirOfSize(t, old, 600, time.Now().Add(-time.Hour))
	writeDirOfSize(t, fresh, 600, time.Now())

	m := New(&fakeScanner{}, settings, paths, dataDir, Budgets{PageThumbBytes: 1000})
	if err := m.runScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("oldest page-thumb dir should be evicted, stat err = %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("newest page-thumb dir should survive: %v", err)
	}
}

// TestRunScanStillEnforcesExtractCacheBudget guards the pre-existing behavior
// through the Budgets refactor.
func TestRunScanStillEnforcesExtractCacheBudget(t *testing.T) {
	settings, paths := newRepos(t)
	dataDir := t.TempDir()
	old := filepath.Join(dataDir, "cache", "1")
	fresh := filepath.Join(dataDir, "cache", "2")
	writeDirOfSize(t, old, 600, time.Now().Add(-time.Hour))
	writeDirOfSize(t, fresh, 600, time.Now())

	m := New(&fakeScanner{}, settings, paths, dataDir, Budgets{ExtractCacheBytes: 1000})
	if err := m.runScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("oldest extract cache dir should be evicted, stat err = %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("newest extract cache dir should survive: %v", err)
	}
}

// TestZeroBudgetsDisableEviction: a zero budget means "no limit", never
// "evict everything".
func TestZeroBudgetsDisableEviction(t *testing.T) {
	settings, paths := newRepos(t)
	dataDir := t.TempDir()
	thumbDir := filepath.Join(dataDir, "thumbs", "pages", "1")
	cacheDir := filepath.Join(dataDir, "cache", "1")
	writeDirOfSize(t, thumbDir, 600, time.Now().Add(-time.Hour))
	writeDirOfSize(t, cacheDir, 600, time.Now().Add(-time.Hour))

	m := New(&fakeScanner{}, settings, paths, dataDir, Budgets{})
	if err := m.runScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(thumbDir); err != nil {
		t.Errorf("zero budget must not evict page thumbs: %v", err)
	}
	if _, err := os.Stat(cacheDir); err != nil {
		t.Errorf("zero budget must not evict extract cache: %v", err)
	}
}

// UI 保存的预算(DB)必须覆盖启动时的 defaults,且下次扫描即生效。
func TestEnforceBudgetsUsesDBOverrides(t *testing.T) {
	settings, paths := newRepos(t)
	dataDir := t.TempDir()
	oldCache := filepath.Join(dataDir, "cache", "1")
	oldThumb := filepath.Join(dataDir, "thumbs", "pages", "1")
	writeDirOfSize(t, oldCache, 600, time.Now().Add(-time.Hour))
	writeDirOfSize(t, filepath.Join(dataDir, "cache", "2"), 600, time.Now())
	writeDirOfSize(t, oldThumb, 600, time.Now().Add(-time.Hour))
	writeDirOfSize(t, filepath.Join(dataDir, "thumbs", "pages", "2"), 600, time.Now())

	// defaults 大到不会触发清理;DB 里保存小预算 → 必须按 DB 清。
	if err := settings.SetBudgets(context.Background(), 1000, 1000); err != nil {
		t.Fatal(err)
	}
	m := New(&fakeScanner{}, settings, paths, dataDir,
		Budgets{ExtractCacheBytes: 1 << 40, PageThumbBytes: 1 << 40})
	if err := m.runScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldCache); !os.IsNotExist(err) {
		t.Errorf("DB budget must evict oldest extract dir, stat err = %v", err)
	}
	if _, err := os.Stat(oldThumb); !os.IsNotExist(err) {
		t.Errorf("DB budget must evict oldest page-thumb dir, stat err = %v", err)
	}
}

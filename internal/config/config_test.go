package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "COMIC")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "config.yaml")
	body = body + "\nlibrary:\n  rootPath: " + root + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	p := writeTemp(t, "dataDir: "+t.TempDir()+"\nserver:\n  addr: \":9000\"\nscan:\n  interval: \"3m\"\nthumbnail:\n  width: 300")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":9000" {
		t.Errorf("addr = %q", cfg.Server.Addr)
	}
	d, err := cfg.ScanInterval()
	if err != nil || d != 3*time.Minute {
		t.Errorf("interval = %v, err %v", d, err)
	}
}

func TestLoadRejectsMissingRoot(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("library:\n  rootPath: /no/such/path\ndataDir: "+dir), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for missing rootPath")
	}
}

func TestLoadRejectsBadInterval(t *testing.T) {
	p := writeTemp(t, "dataDir: "+t.TempDir()+"\nscan:\n  interval: \"notaduration\"")
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for bad interval")
	}
}

// When allowedRoots is unset it must default to exactly [rootPath] — this is
// what guarantees the path-add gate is populated (and not broader) in production.
func TestLoadDefaultsAllowedRootsToRootPath(t *testing.T) {
	p := writeTemp(t, "dataDir: "+t.TempDir()+"\nthumbnail:\n  width: 300")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Library.AllowedRoots) != 1 || cfg.Library.AllowedRoots[0] != cfg.Library.RootPath {
		t.Fatalf("allowedRoots = %v, want [%q]", cfg.Library.AllowedRoots, cfg.Library.RootPath)
	}
}

// A relative allowedRoots entry must be resolved to an absolute path, so the
// gate's containment check (which compares resolved absolute paths) works.
func TestLoadAbsResolvesAllowedRoots(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "COMIC")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "config.yaml")
	body := "dataDir: " + t.TempDir() + "\nthumbnail:\n  width: 300\n" +
		"library:\n  rootPath: " + root + "\n  allowedRoots:\n    - " + root + "\n    - some/relative/dir\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Library.AllowedRoots) != 2 {
		t.Fatalf("allowedRoots = %v, want 2 entries", cfg.Library.AllowedRoots)
	}
	for _, r := range cfg.Library.AllowedRoots {
		if !filepath.IsAbs(r) {
			t.Fatalf("allowedRoots entry %q is not absolute", r)
		}
	}
}

func TestLoadPageThumbBudget(t *testing.T) {
	// default when unset
	p := writeTemp(t, "dataDir: "+t.TempDir())
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Thumbnail.PagesMaxBytes != 512<<20 {
		t.Errorf("default pagesMaxBytes = %d, want %d", cfg.Thumbnail.PagesMaxBytes, 512<<20)
	}
	// yaml override
	p = writeTemp(t, "dataDir: "+t.TempDir()+"\nthumbnail:\n  pagesMaxBytes: 1048576")
	cfg, err = Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Thumbnail.PagesMaxBytes != 1<<20 {
		t.Errorf("pagesMaxBytes = %d, want %d", cfg.Thumbnail.PagesMaxBytes, 1<<20)
	}
}

func TestEnvOverridesBudgets(t *testing.T) {
	p := writeTemp(t, "dataDir: "+t.TempDir()+"\ncache:\n  maxBytes: 1\nthumbnail:\n  pagesMaxBytes: 1")
	t.Setenv("TEFNUT_CACHE_MAX_BYTES", "1GiB")
	t.Setenv("TEFNUT_THUMB_PAGES_MAX_BYTES", "64MiB")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cache.MaxBytes != 1<<30 {
		t.Errorf("cache.maxBytes = %d, want %d (env must beat yaml)", cfg.Cache.MaxBytes, int64(1<<30))
	}
	if cfg.Thumbnail.PagesMaxBytes != 64<<20 {
		t.Errorf("thumbnail.pagesMaxBytes = %d, want %d (env must beat yaml)", cfg.Thumbnail.PagesMaxBytes, int64(64<<20))
	}
}

func TestEnvOverrideRejectsGarbage(t *testing.T) {
	p := writeTemp(t, "dataDir: "+t.TempDir())
	t.Setenv("TEFNUT_CACHE_MAX_BYTES", "lots")
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for unparseable size")
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"0", 0, false},
		{"123", 123, false},
		{"2GiB", 2 << 30, false},
		{"512MiB", 512 << 20, false},
		{"1gb", 1 << 30, false},
		{"10k", 10 << 10, false},
		{"4TiB", 4 << 40, false},
		{" 8 MiB ", 8 << 20, false},
		{"", 0, true},
		{"-1", 0, true},
		{"1.5G", 0, true},
		{"1XB", 0, true},
		{"999999999999GiB", 0, true}, // overflow
	}
	for _, c := range cases {
		got, err := parseSize(c.in)
		if c.err != (err != nil) {
			t.Errorf("parseSize(%q): err = %v, want err=%v", c.in, err, c.err)
			continue
		}
		if !c.err && got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 模板本身必须可解析且值正确(不经 validate,因 /comics 在测试机不存在)。
func TestDefaultTemplateParses(t *testing.T) {
	cfg := defaults()
	if err := yaml.Unmarshal([]byte(defaultTemplate), cfg); err != nil {
		t.Fatalf("template does not parse: %v", err)
	}
	if cfg.Library.RootPath != "/comics" || cfg.DataDir != "/data" {
		t.Errorf("paths = %q/%q", cfg.Library.RootPath, cfg.DataDir)
	}
	if int64(cfg.Cache.MaxBytes) != 2<<30 || int64(cfg.Thumbnail.PagesMaxBytes) != 512<<20 {
		t.Errorf("budgets = %d/%d", cfg.Cache.MaxBytes, cfg.Thumbnail.PagesMaxBytes)
	}
	if strings.Contains(defaultTemplate, "scan:") {
		t.Error("template must not contain the dead scan: section")
	}
}

func TestLoadOrInitSeedsMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfgdir", "config.yaml")
	_, err := LoadOrInit(p)
	// /comics 不存在于测试机 → 期望 rootPath 校验错误;但文件必须已生成。
	if err == nil || !strings.Contains(err.Error(), "rootPath") {
		t.Fatalf("expected rootPath validation error on this machine, got %v", err)
	}
	b, rerr := os.ReadFile(p)
	if rerr != nil {
		t.Fatalf("template file not seeded: %v", rerr)
	}
	if !strings.Contains(string(b), "pagesMaxBytes") || !strings.Contains(string(b), "maxBytes") {
		t.Errorf("seeded file missing budget keys:\n%s", b)
	}
}

func TestLoadOrInitReadOnlyDirErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	_, err := LoadOrInit(filepath.Join(dir, "config.yaml"))
	if err == nil || !strings.Contains(err.Error(), "检查配置目录挂载与权限") {
		t.Fatalf("expected write-failure error mentioning mount/permissions, got %v", err)
	}
}

func TestLoadOrInitKeepsExistingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	custom := "library:\n  rootPath: " + dir + "\ndataDir: " + dir + "\ncache:\n  maxBytes: 7\n"
	if err := os.WriteFile(p, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadOrInit(p)
	if err != nil {
		t.Fatalf("LoadOrInit: %v", err)
	}
	if int64(cfg.Cache.MaxBytes) != 7 {
		t.Errorf("existing file must be used untouched, maxBytes = %d", cfg.Cache.MaxBytes)
	}
	b, _ := os.ReadFile(p)
	if string(b) != custom {
		t.Error("existing file content was rewritten")
	}
}

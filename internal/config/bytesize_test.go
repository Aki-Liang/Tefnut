package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestByteSizeUnmarshalYAML(t *testing.T) {
	var v struct {
		N ByteSize `yaml:"n"`
	}
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"n: 2147483648", 2147483648, false},
		{"n: 2GiB", 2 << 30, false},
		{"n: \"512MiB\"", 512 << 20, false},
		{"n: 0", 0, false},
		{"n: banana", 0, true},
		{"n: [1,2]", 0, true},
	}
	for _, c := range cases {
		v.N = 0
		err := yaml.Unmarshal([]byte(c.in), &v)
		if c.err != (err != nil) {
			t.Errorf("%q: err = %v, want err=%v", c.in, err, c.err)
			continue
		}
		if !c.err && int64(v.N) != c.want {
			t.Errorf("%q = %d, want %d", c.in, v.N, c.want)
		}
	}
}

func TestLoadAcceptsSuffixedBudgets(t *testing.T) {
	p := writeTemp(t, "dataDir: "+t.TempDir()+"\ncache:\n  maxBytes: 2GiB\nthumbnail:\n  pagesMaxBytes: 512MiB")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if int64(cfg.Cache.MaxBytes) != 2<<30 || int64(cfg.Thumbnail.PagesMaxBytes) != 512<<20 {
		t.Errorf("budgets = %d/%d", cfg.Cache.MaxBytes, cfg.Thumbnail.PagesMaxBytes)
	}
}

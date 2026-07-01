package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Library struct {
	RootPath     string   `yaml:"rootPath"`
	AllowedRoots []string `yaml:"allowedRoots"`
}

type Server struct {
	Addr string `yaml:"addr"`
}

type Scan struct {
	Interval string `yaml:"interval"`
}

type Thumbnail struct {
	Width     int `yaml:"width"`
	PageWidth int `yaml:"pageWidth"`
}

type Cache struct {
	MaxBytes int64 `yaml:"maxBytes"`
}

type Config struct {
	Library   Library   `yaml:"library"`
	DataDir   string    `yaml:"dataDir"`
	Server    Server    `yaml:"server"`
	Scan      Scan      `yaml:"scan"`
	Thumbnail Thumbnail `yaml:"thumbnail"`
	Cache     Cache     `yaml:"cache"`
}

func defaults() *Config {
	return &Config{
		DataDir:   "./data",
		Server:    Server{Addr: ":8086"},
		Scan:      Scan{Interval: "1h"},
		Thumbnail: Thumbnail{Width: 400, PageWidth: 120},
		Cache:     Cache{MaxBytes: 2 << 30},
	}
}

// Load reads YAML at path on top of defaults, then validates.
func Load(path string) (*Config, error) {
	cfg := defaults()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Library.RootPath == "" {
		return errors.New("config: library.rootPath is required")
	}
	info, err := os.Stat(c.Library.RootPath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("config: library.rootPath %q is not a readable directory", c.Library.RootPath)
	}
	// AllowedRoots gate which directories the path API may add as libraries.
	// Default to the configured library root; resolve all entries to absolute.
	if len(c.Library.AllowedRoots) == 0 {
		c.Library.AllowedRoots = []string{c.Library.RootPath}
	}
	for i, r := range c.Library.AllowedRoots {
		abs, err := filepath.Abs(r)
		if err != nil {
			return fmt.Errorf("config: library.allowedRoots %q: %w", r, err)
		}
		c.Library.AllowedRoots[i] = abs
	}
	if c.DataDir == "" {
		return errors.New("config: dataDir is required")
	}
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return fmt.Errorf("config: cannot create dataDir %q: %w", c.DataDir, err)
	}
	if c.Thumbnail.Width <= 0 {
		return errors.New("config: thumbnail.width must be > 0")
	}
	if c.Thumbnail.PageWidth <= 0 {
		c.Thumbnail.PageWidth = 120
	}
	if _, err := c.ScanInterval(); err != nil {
		return fmt.Errorf("config: scan.interval %q invalid: %w", c.Scan.Interval, err)
	}
	return nil
}

// ScanInterval parses Scan.Interval into a Duration.
func (c *Config) ScanInterval() (time.Duration, error) {
	return time.ParseDuration(c.Scan.Interval)
}

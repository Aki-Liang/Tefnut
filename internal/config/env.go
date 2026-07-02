// Env overrides let containers tune the disk budgets without mounting a
// config file: env > yaml > defaults.
package config

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	envCacheMax      = "TEFNUT_CACHE_MAX_BYTES"       // cache.maxBytes
	envPageThumbsMax = "TEFNUT_THUMB_PAGES_MAX_BYTES" // thumbnail.pagesMaxBytes
)

// applyEnv overrides the disk budgets from the process environment. Values
// accept raw bytes or a binary-unit suffix ("2GiB", "512MiB", "0" = no limit).
func (c *Config) applyEnv() error {
	if v, ok := os.LookupEnv(envCacheMax); ok {
		n, err := parseSize(v)
		if err != nil {
			return fmt.Errorf("config: %s: %w", envCacheMax, err)
		}
		c.Cache.MaxBytes = n
	}
	if v, ok := os.LookupEnv(envPageThumbsMax); ok {
		n, err := parseSize(v)
		if err != nil {
			return fmt.Errorf("config: %s: %w", envPageThumbsMax, err)
		}
		c.Thumbnail.PagesMaxBytes = n
	}
	return nil
}

var sizeRE = regexp.MustCompile(`^([0-9]+) ?([A-Za-z]*)$`)

// parseSize parses "123", "512MiB", "2gb" … into bytes. All units are binary
// (K = KiB = 1024); fractional values are rejected.
func parseSize(s string) (int64, error) {
	m := sizeRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("invalid size %q (use e.g. 1073741824, 512MiB, 2GiB, 0)", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	var mult int64
	switch strings.ToLower(m[2]) {
	case "", "b":
		mult = 1
	case "k", "kb", "kib":
		mult = 1 << 10
	case "m", "mb", "mib":
		mult = 1 << 20
	case "g", "gb", "gib":
		mult = 1 << 30
	case "t", "tb", "tib":
		mult = 1 << 40
	default:
		return 0, fmt.Errorf("invalid size unit %q in %q", m[2], s)
	}
	if n > math.MaxInt64/mult {
		return 0, fmt.Errorf("size %q overflows", s)
	}
	return n * mult, nil
}

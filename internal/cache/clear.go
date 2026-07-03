package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Clear removes everything under root (whole per-comic subdirectories and any
// stray top-level files), keeping root itself, and returns the bytes freed.
// A missing root is a no-op.
func Clear(root string) (int64, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("cache: read %s: %w", root, err)
	}
	var freed int64
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		var sz int64
		if e.IsDir() {
			sz, err = dirSize(p)
			if err != nil {
				return freed, fmt.Errorf("cache: size %s: %w", p, err)
			}
		} else if info, err := e.Info(); err == nil {
			sz = info.Size()
		}
		if err := os.RemoveAll(p); err != nil {
			return freed, fmt.Errorf("cache: clear %s: %w", p, err)
		}
		freed += sz
	}
	return freed, nil
}

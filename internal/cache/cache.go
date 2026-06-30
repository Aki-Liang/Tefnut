// Package cache bounds the on-disk extract cache so it cannot grow without limit.
package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type dirInfo struct {
	path  string
	size  int64
	mtime int64
}

func dirSize(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// Enforce evicts whole top-level subdirectories of root, oldest-modified first,
// until root's total size is <= maxBytes. maxBytes <= 0 disables eviction.
func Enforce(root string, maxBytes int64) (int, error) {
	if maxBytes <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("cache: read %s: %w", root, err)
	}
	var dirs []dirInfo
	var total int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		sz, err := dirSize(p)
		if err != nil {
			return 0, fmt.Errorf("cache: size %s: %w", p, err)
		}
		info, err := e.Info()
		if err != nil {
			return 0, fmt.Errorf("cache: stat %s: %w", p, err)
		}
		dirs = append(dirs, dirInfo{path: p, size: sz, mtime: info.ModTime().Unix()})
		total += sz
	}
	if total <= maxBytes {
		return 0, nil
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].mtime < dirs[j].mtime })
	evicted := 0
	for _, d := range dirs {
		if total <= maxBytes {
			break
		}
		if err := os.RemoveAll(d.path); err != nil {
			return evicted, fmt.Errorf("cache: evict %s: %w", d.path, err)
		}
		total -= d.size
		evicted++
	}
	return evicted, nil
}

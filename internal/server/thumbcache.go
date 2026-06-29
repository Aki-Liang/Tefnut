package server

import (
	"os"
	"path/filepath"
	"strings"

	lru "github.com/hashicorp/golang-lru/v2"
)

// thumbCache is a two-layer page-thumbnail cache: a bounded in-memory LRU in
// front of durable per-page JPEG files under <dir>/pages/<id>/<n>.jpg.
type thumbCache struct {
	mem *lru.Cache[string, []byte]
	dir string
}

func newThumbCache(maxEntries int, dir string) (*thumbCache, error) {
	mem, err := lru.New[string, []byte](maxEntries)
	if err != nil {
		return nil, err
	}
	return &thumbCache{mem: mem, dir: dir}, nil
}

// path maps "<id>:<n>" to <dir>/pages/<id>/<n>.jpg.
func (c *thumbCache) path(key string) string {
	id, n, ok := strings.Cut(key, ":")
	if !ok {
		id, n = "0", key
	}
	return filepath.Join(c.dir, "pages", id, n+".jpg")
}

func (c *thumbCache) get(key string) ([]byte, bool) {
	if b, ok := c.mem.Get(key); ok {
		return b, true
	}
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	c.mem.Add(key, b)
	return b, true
}

func (c *thumbCache) put(key string, b []byte) error {
	c.mem.Add(key, b)
	p := c.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

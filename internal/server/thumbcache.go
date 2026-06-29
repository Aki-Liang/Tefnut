package server

import "sync"

// thumbCache is a tiny bounded in-memory byte cache. When it would exceed max
// entries it clears itself (simple, allocation-free eviction adequate for a
// single-user home app).
type thumbCache struct {
	mu  sync.Mutex
	m   map[string][]byte
	max int
}

func newThumbCache(max int) *thumbCache {
	return &thumbCache{m: make(map[string][]byte), max: max}
}

func (c *thumbCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.m[key]
	return b, ok
}

func (c *thumbCache) put(key string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.max {
		c.m = make(map[string][]byte)
	}
	c.m[key] = b
}

func (c *thumbCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}

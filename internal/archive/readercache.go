package archive

import (
	"container/list"
	"context"
	"sync"
)

type cacheEntry struct {
	key     string
	mtime   int64
	reader  Reader
	refs    int
	evicted bool
	elem    *list.Element
}

// ReaderCache keeps up to max open archive Readers, keyed by a caller-supplied
// key (e.g. node id). Readers are refcounted: an entry chosen for eviction is
// closed only once its last in-flight reader is released, so a reader is never
// closed mid-stream.
type ReaderCache struct {
	mu      sync.Mutex
	max     int
	entries map[string]*cacheEntry
	lru     *list.List // front = most recently used
}

func NewReaderCache(max int) *ReaderCache {
	if max < 1 {
		max = 1
	}
	return &ReaderCache{max: max, entries: make(map[string]*cacheEntry), lru: list.New()}
}

func (c *ReaderCache) Acquire(ctx context.Context, key, path string, mtime int64, cacheDir string) (Reader, func(), error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && e.mtime == mtime && !e.evicted {
		e.refs++
		c.lru.MoveToFront(e.elem)
		c.mu.Unlock()
		return e.reader, c.releaser(e), nil
	}
	// Stale (mtime changed) or missing: drop the old entry from the map and open fresh.
	if e, ok := c.entries[key]; ok {
		c.dropLocked(e)
	}
	c.mu.Unlock()

	r, err := Open(ctx, path, cacheDir)
	if err != nil {
		return nil, nil, err
	}

	c.mu.Lock()
	// Another goroutine may have inserted concurrently; if so, use ours but it
	// will simply be an extra short-lived reader — acceptable. Insert ours.
	e := &cacheEntry{key: key, mtime: mtime, reader: r, refs: 1}
	e.elem = c.lru.PushFront(e)
	c.entries[key] = e
	c.evictLocked()
	c.mu.Unlock()
	return r, c.releaser(e), nil
}

func (c *ReaderCache) releaser(e *cacheEntry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			e.refs--
			if e.evicted && e.refs == 0 {
				c.mu.Unlock()
				e.reader.Close()
				return
			}
			c.mu.Unlock()
		})
	}
}

// Drop evicts the entry for key if present so the next Acquire re-opens it.
// Safe with readers in flight: the entry is marked evicted and closed by its
// last releaser, never mid-stream.
func (c *ReaderCache) Drop(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		c.dropLocked(e)
	}
}

// dropLocked removes e from the map/lru and marks it evicted; it is closed now
// if idle, else closed by the last releaser.
func (c *ReaderCache) dropLocked(e *cacheEntry) {
	delete(c.entries, e.key)
	c.lru.Remove(e.elem)
	e.evicted = true
	if e.refs == 0 {
		// Close outside the lock to avoid holding it during IO.
		go e.reader.Close()
	}
}

func (c *ReaderCache) evictLocked() {
	for c.lru.Len() > c.max {
		back := c.lru.Back()
		if back == nil {
			return
		}
		e := back.Value.(*cacheEntry)
		c.dropLocked(e)
	}
}

func (c *ReaderCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		e.evicted = true
		if e.refs == 0 {
			e.reader.Close()
		}
	}
	c.entries = make(map[string]*cacheEntry)
	c.lru.Init()
}

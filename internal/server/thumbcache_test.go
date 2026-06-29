package server

import "testing"

func TestThumbCachePutGet(t *testing.T) {
	c := newThumbCache(2)
	c.put("a", []byte("x"))
	if b, ok := c.get("a"); !ok || string(b) != "x" {
		t.Fatalf("get a = %q %v", b, ok)
	}
	if _, ok := c.get("missing"); ok {
		t.Fatal("missing should not be present")
	}
}

func TestThumbCacheBounded(t *testing.T) {
	c := newThumbCache(2)
	c.put("a", []byte("1"))
	c.put("b", []byte("2"))
	c.put("c", []byte("3")) // exceeds max → cache cleared, then c stored
	if c.size() > 2 {
		t.Fatalf("cache size %d exceeds max", c.size())
	}
	if _, ok := c.get("c"); !ok {
		t.Fatal("most recent put must be retained")
	}
}

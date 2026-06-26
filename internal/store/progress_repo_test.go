package store

import (
	"context"
	"testing"
)

func TestProgressDefaultZero(t *testing.T) {
	r := NewProgressRepo(openTemp(t))
	p, err := r.Get(context.Background(), 1)
	if err != nil || p != 0 {
		t.Fatalf("expected 0/nil, got %d/%v", p, err)
	}
}

func TestProgressSetGet(t *testing.T) {
	ctx := context.Background()
	r := NewProgressRepo(openTemp(t))
	if err := r.Set(ctx, 7, 12); err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, 7, 15); err != nil {
		t.Fatal(err)
	}
	p, _ := r.Get(ctx, 7)
	if p != 15 {
		t.Fatalf("expected 15, got %d", p)
	}
}

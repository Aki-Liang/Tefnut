package store

import (
	"context"
	"errors"
	"testing"
)

func TestLibraryPathAddListGet(t *testing.T) {
	ctx := context.Background()
	r := NewLibraryPathRepo(openTemp(t))
	lp, err := r.Add(ctx, "我的漫画", "/Users/x/comic")
	if err != nil {
		t.Fatal(err)
	}
	if lp.ID == 0 || lp.Name != "我的漫画" {
		t.Fatalf("got %+v", lp)
	}
	got, err := r.Get(ctx, lp.ID)
	if err != nil || got.Path != "/Users/x/comic" {
		t.Fatalf("get %+v err %v", got, err)
	}
	list, _ := r.List(ctx)
	if len(list) != 1 {
		t.Fatalf("list len %d", len(list))
	}
}

func TestLibraryPathDuplicate(t *testing.T) {
	ctx := context.Background()
	r := NewLibraryPathRepo(openTemp(t))
	r.Add(ctx, "a", "/p")
	if _, err := r.Add(ctx, "b", "/p"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestLibraryPathRenameDelete(t *testing.T) {
	ctx := context.Background()
	r := NewLibraryPathRepo(openTemp(t))
	lp, _ := r.Add(ctx, "old", "/p")
	if err := r.Rename(ctx, lp.ID, "new"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, lp.ID)
	if got.Name != "new" {
		t.Fatalf("name = %s", got.Name)
	}
	if err := r.Delete(ctx, lp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, lp.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected gone, got %v", err)
	}
}

package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathWithinRoots(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "manga", "vol1")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()     // a separate temp dir, not under root
	sibling := root + "-other" // shares root's textual prefix but is NOT inside it
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sibling) })

	cases := []struct {
		name string
		dir  string
		want bool
	}{
		{"subdir inside root", sub, true},
		{"root itself", root, true},
		{"outside root", outside, false},
		{"prefix-trap sibling", sibling, false},
		{"parent of root via ..", filepath.Join(root, ".."), false},
	}
	for _, c := range cases {
		got, err := PathWithinRoots(c.dir, []string{root})
		if err != nil {
			t.Fatalf("%s: unexpected err %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestPathWithinRootsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	got, err := PathWithinRoots(link, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("a symlink inside root pointing outside must be rejected")
	}
}

func TestPathWithinRootsMultipleAndEmpty(t *testing.T) {
	r1 := t.TempDir()
	r2 := t.TempDir()
	if got, _ := PathWithinRoots(r2, []string{r1, r2}); !got {
		t.Fatal("second root should match")
	}
	if got, _ := PathWithinRoots(r1, nil); got {
		t.Fatal("empty roots must reject")
	}
}

func TestPathWithinRootsMissingDirErrors(t *testing.T) {
	root := t.TempDir()
	_, err := PathWithinRoots(filepath.Join(root, "does-not-exist"), []string{root})
	if err == nil {
		t.Fatal("a non-existent dir should return an error so the caller fails closed")
	}
}

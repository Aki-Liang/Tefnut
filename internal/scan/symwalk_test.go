package scan

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func collectDirs(t *testing.T, root string) []string {
	t.Helper()
	var got []string
	walkDirsFollowingSymlinks(root, func(dir string) { got = append(got, dir) })
	sort.Strings(got)
	return got
}

func TestWalkDirsIncludesNestedAndSymlinkedDirs(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "A", "B"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}

	got := collectDirs(t, root)
	want := []string{
		root,
		filepath.Join(root, "A"),
		filepath.Join(root, "A", "B"),
		filepath.Join(root, "linked"),
		filepath.Join(root, "linked", "inner"),
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("dirs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dirs = %v, want %v", got, want)
		}
	}
}

func TestWalkDirsRootItselfSymlink(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	root := filepath.Join(base, "link")
	if err := os.Symlink(target, root); err != nil {
		t.Fatal(err)
	}

	got := collectDirs(t, root)
	want := []string{root, filepath.Join(root, "sub")}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("dirs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dirs = %v, want %v", got, want)
		}
	}
}

func TestWalkDirsTerminatesOnLoop(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "A")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(sub, "loop")); err != nil {
		t.Fatal(err)
	}

	got := collectDirs(t, root)
	want := []string{root, sub}
	if len(got) != len(want) {
		t.Fatalf("dirs = %v, want %v (loop must not be followed)", got, want)
	}
}

func TestWalkDirsMidChainLoopTerminates(t *testing.T) {
	root := t.TempDir()
	b := filepath.Join(root, "A", "B")
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	// Loop back at a non-root ancestor: exercises onPath entries pushed
	// during recursion, not just the seeded root.
	if err := os.Symlink(filepath.Join(root, "A"), filepath.Join(b, "loop")); err != nil {
		t.Fatal(err)
	}
	got := collectDirs(t, root)
	want := []string{root, filepath.Join(root, "A"), b}
	if len(got) != len(want) {
		t.Fatalf("dirs = %v, want %v (mid-chain loop must not be followed)", got, want)
	}
}

func TestWalkDirsSiblingLinksToSameDirBothWalked(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	for _, name := range []string{"One", "Two"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	got := collectDirs(t, root)
	want := []string{root, filepath.Join(root, "One"), filepath.Join(root, "Two")}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("dirs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dirs = %v, want %v", got, want)
		}
	}
}

func TestWalkDirsSkipsBrokenSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	got := collectDirs(t, root)
	if len(got) != 1 || got[0] != root {
		t.Fatalf("dirs = %v, want just root", got)
	}
}

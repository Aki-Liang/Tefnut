package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"Tefnut/internal/store"
)

func TestScanFollowsSymlinkedDir(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	root := t.TempDir()
	target := t.TempDir()
	writeZip(t, filepath.Join(target, "Vol1.zip"), map[string][]byte{
		"001.png": pngBytes(t),
	})
	if err := os.Symlink(target, filepath.Join(root, "Series")); err != nil {
		t.Fatal(err)
	}
	paths.Add(ctx, "Lib", root)
	if err := sc.Scan(ctx); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	libs, _ := nodes.ListChildren(ctx, 0, -1, 0)
	if len(libs) != 1 {
		t.Fatalf("expected 1 library node, got %d", len(libs))
	}
	series, _ := nodes.ListChildren(ctx, libs[0].ID, -1, 0)
	if len(series) != 1 || series[0].Type != store.NodeDir || series[0].Name != "Series" {
		t.Fatalf("symlinked dir not scanned as dir node: %+v", series)
	}
	kids, _ := nodes.ListChildren(ctx, series[0].ID, -1, 0)
	if len(kids) != 1 || kids[0].Type != store.NodeComic {
		t.Fatalf("comic inside symlinked dir not found: %+v", kids)
	}
	if kids[0].PageCount != 1 || kids[0].CoverStatus != store.CoverReady {
		t.Errorf("comic pages=%d cover=%d, want 1/ready", kids[0].PageCount, kids[0].CoverStatus)
	}
}

func TestScanSymlinkedComicTracksTargetChanges(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	root := t.TempDir()
	target := t.TempDir()
	real := filepath.Join(target, "real.cbz")
	writeTestZip(t, real, []string{"01.png", "02.png"})
	link := filepath.Join(root, "linked.cbz")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	paths.Add(ctx, "Lib", root)
	if err := sc.Scan(ctx); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := pageCountOf(t, nodes, link); got != 2 {
		t.Fatalf("page count after first scan = %d, want 2", got)
	}

	// Size/mtime must come from the target, so replacing it triggers a rebuild.
	writeTestZip(t, real, []string{"01.png", "02.png", "03.png"})
	if err := sc.Scan(ctx); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if got := pageCountOf(t, nodes, link); got != 3 {
		t.Fatalf("page count after target replace = %d, want 3", got)
	}
}

func TestScanSkipsBrokenSymlink(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "dangling.cbz")); err != nil {
		t.Fatal(err)
	}
	paths.Add(ctx, "Lib", root)
	if err := sc.Scan(ctx); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	libs, _ := nodes.ListChildren(ctx, 0, -1, 0)
	if len(libs) != 1 {
		t.Fatalf("expected 1 library node, got %d", len(libs))
	}
	kids, _ := nodes.ListChildren(ctx, libs[0].ID, -1, 0)
	if len(kids) != 0 {
		t.Fatalf("broken symlink should be skipped, got %+v", kids)
	}
}

func TestScanFollowsSymlinkChain(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	root := t.TempDir()
	target := t.TempDir()
	writeZip(t, filepath.Join(target, "Vol1.zip"), map[string][]byte{"001.png": pngBytes(t)})
	hop := filepath.Join(t.TempDir(), "hop")
	if err := os.Symlink(target, hop); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hop, filepath.Join(root, "Series")); err != nil {
		t.Fatal(err)
	}
	paths.Add(ctx, "Lib", root)
	if err := sc.Scan(ctx); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	libs, _ := nodes.ListChildren(ctx, 0, -1, 0)
	series, _ := nodes.ListChildren(ctx, libs[0].ID, -1, 0)
	if len(series) != 1 || series[0].Type != store.NodeDir {
		t.Fatalf("chained symlink dir not scanned: %+v", series)
	}
	kids, _ := nodes.ListChildren(ctx, series[0].ID, -1, 0)
	if len(kids) != 1 || kids[0].Type != store.NodeComic {
		t.Fatalf("comic behind symlink chain not found: %+v", kids)
	}
}

func TestScanSiblingSymlinksToSameDirBothScanned(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	root := t.TempDir()
	target := t.TempDir()
	writeZip(t, filepath.Join(target, "Vol1.zip"), map[string][]byte{"001.png": pngBytes(t)})
	for _, name := range []string{"One", "Two"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	paths.Add(ctx, "Lib", root)
	if err := sc.Scan(ctx); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	libs, _ := nodes.ListChildren(ctx, 0, -1, 0)
	dirs, _ := nodes.ListChildren(ctx, libs[0].ID, -1, 0)
	if len(dirs) != 2 {
		t.Fatalf("sibling links to same target are distinct entries, got %+v", dirs)
	}
	for _, d := range dirs {
		kids, _ := nodes.ListChildren(ctx, d.ID, -1, 0)
		if len(kids) != 1 || kids[0].Type != store.NodeComic {
			t.Fatalf("dir %s children = %+v", d.Name, kids)
		}
	}
}

func TestScanSymlinkLoopTerminates(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	root := t.TempDir()
	sub := filepath.Join(root, "A")
	writeZip(t, filepath.Join(sub, "a.zip"), map[string][]byte{"001.png": pngBytes(t)})
	// A/loop points back at the library root: must be skipped, not recursed.
	if err := os.Symlink(root, filepath.Join(sub, "loop")); err != nil {
		t.Fatal(err)
	}
	paths.Add(ctx, "Lib", root)
	if err := sc.Scan(ctx); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	libs, _ := nodes.ListChildren(ctx, 0, -1, 0)
	if len(libs) != 1 {
		t.Fatalf("expected 1 library node, got %d", len(libs))
	}
	subs, _ := nodes.ListChildren(ctx, libs[0].ID, -1, 0)
	if len(subs) != 1 || subs[0].Name != "A" {
		t.Fatalf("expected only dir A under root, got %+v", subs)
	}
	kids, _ := nodes.ListChildren(ctx, subs[0].ID, -1, 0)
	if len(kids) != 1 || kids[0].Type != store.NodeComic {
		t.Fatalf("expected only the comic under A (loop link skipped), got %+v", kids)
	}
}

func TestScanSymlinkMidChainLoopTerminates(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	root := t.TempDir()
	b := filepath.Join(root, "A", "B")
	writeZip(t, filepath.Join(b, "a.zip"), map[string][]byte{"001.png": pngBytes(t)})
	// B/loop points at a non-root ancestor: exercises the onPath entries
	// pushed during recursion, not just the seeded library root.
	if err := os.Symlink(filepath.Join(root, "A"), filepath.Join(b, "loop")); err != nil {
		t.Fatal(err)
	}
	paths.Add(ctx, "Lib", root)
	if err := sc.Scan(ctx); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	libs, _ := nodes.ListChildren(ctx, 0, -1, 0)
	as, _ := nodes.ListChildren(ctx, libs[0].ID, -1, 0)
	if len(as) != 1 || as[0].Name != "A" {
		t.Fatalf("expected only dir A under root, got %+v", as)
	}
	bs, _ := nodes.ListChildren(ctx, as[0].ID, -1, 0)
	if len(bs) != 1 || bs[0].Name != "B" {
		t.Fatalf("expected only dir B under A, got %+v", bs)
	}
	kids, _ := nodes.ListChildren(ctx, bs[0].ID, -1, 0)
	if len(kids) != 1 || kids[0].Type != store.NodeComic {
		t.Fatalf("expected only the comic under B (loop link skipped), got %+v", kids)
	}
}

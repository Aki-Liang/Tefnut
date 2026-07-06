package library

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	"Tefnut/internal/archive"
	"Tefnut/internal/store"
	"Tefnut/internal/thumb"
)

type buildTask struct {
	node  *store.Node
	size  int64
	mtime int64
}

type Scanner struct {
	repo       *store.NodeRepo
	paths      *store.LibraryPathRepo
	dataDir    string
	thumbWidth int
	mu         sync.Mutex
}

func NewScanner(repo *store.NodeRepo, paths *store.LibraryPathRepo, dataDir string, thumbWidth int) *Scanner {
	return &Scanner{repo: repo, paths: paths, dataDir: dataDir, thumbWidth: thumbWidth}
}

func (s *Scanner) thumbPath(id int64) string {
	return filepath.Join(s.dataDir, "thumbs", strconv.FormatInt(id, 10)+".jpg")
}

func (s *Scanner) cacheDir(id int64) string {
	return filepath.Join(s.dataDir, "cache", strconv.FormatInt(id, 10))
}

// Scan performs a full idempotent sync of all configured library paths.
func (s *Scanner) Scan(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var builds []buildTask

	libs, err := s.paths.List(ctx)
	if err != nil {
		return fmt.Errorf("scanner: list library paths: %w", err)
	}
	roots, err := s.repo.ListChildren(ctx, 0, -1, 0)
	if err != nil {
		return err
	}
	byPath := map[string]*store.Node{}
	for _, n := range roots {
		byPath[n.Path] = n
	}

	for _, lib := range libs {
		abs, err := filepath.Abs(lib.Path)
		if err != nil {
			log.Printf("scanner: abs %s: %v", lib.Path, err)
			continue
		}
		node, seen := byPath[abs]
		if seen {
			delete(byPath, abs)
			if node.Name != lib.Name {
				if err := s.repo.UpdateName(ctx, node.ID, lib.Name); err != nil {
					log.Printf("scanner: rename root %d: %v", node.ID, err)
				}
			}
		} else {
			node, err = s.repo.Create(ctx, &store.Node{
				ParentID: 0, Name: lib.Name, Path: abs, Type: store.NodeDir,
			})
			if err != nil {
				log.Printf("scanner: create library node %s: %v", abs, err)
				continue
			}
		}
		if _, err := os.Stat(abs); err != nil {
			log.Printf("scanner: library path %s unavailable: %v", abs, err)
			continue
		}
		realRoot, err := filepath.EvalSymlinks(abs)
		if err != nil {
			log.Printf("scanner: resolve library path %s: %v", abs, err)
			continue
		}
		onPath := map[string]bool{realRoot: true}
		if err := s.scanDir(ctx, abs, node.ID, &builds, realRoot, onPath); err != nil {
			log.Printf("scanner: scan library %s: %v", abs, err)
		}
	}

	for _, n := range byPath {
		s.removeNode(ctx, n)
	}
	s.runBuilds(ctx, builds)
	return nil
}

// scanDir syncs one directory level. realDir is dir with all symlinks
// resolved; onPath holds the resolved path of every directory on the current
// recursion chain, so a directory symlink pointing back at an ancestor is
// skipped instead of recursed into forever. Non-ancestor repeats are allowed
// on purpose: two links to the same directory are two library entries, each
// scanned with its own nodes.
func (s *Scanner) scanDir(ctx context.Context, dir string, parentID int64, builds *[]buildTask, realDir string, onPath map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("scanner: read dir %s: %w", dir, err)
	}

	existing, err := s.repo.ListChildren(ctx, parentID, -1, 0)
	if err != nil {
		return err
	}
	byPath := map[string]*store.Node{}
	for _, n := range existing {
		byPath[n.Path] = n
	}

	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		isLink := e.Type()&os.ModeSymlink != 0
		if !isLink && !e.IsDir() && !archive.IsComic(e.Name()) {
			continue
		}
		// Follow symlinks: classify by the target so linked dirs recurse, and
		// take size/mtime from the target so change detection sees updates.
		var info os.FileInfo
		var err error
		if isLink {
			info, err = os.Stat(p)
		} else {
			info, err = e.Info()
		}
		if err != nil {
			log.Printf("scanner: stat %s: %v", p, err)
			continue
		}
		isDir := info.IsDir()
		if !isDir && !archive.IsComic(e.Name()) {
			continue
		}
		realChild := filepath.Join(realDir, e.Name())
		if isDir && isLink {
			realChild, err = filepath.EvalSymlinks(p)
			if err != nil {
				log.Printf("scanner: resolve symlink %s: %v", p, err)
				continue
			}
			if onPath[realChild] {
				log.Printf("scanner: symlink loop %s -> %s; skipping", p, realChild)
				continue
			}
		}

		node, seen := byPath[p]
		if seen {
			delete(byPath, p)
		} else {
			typ := store.NodeComic
			if isDir {
				typ = store.NodeDir
			}
			node, err = s.repo.Create(ctx, &store.Node{
				ParentID: parentID, Name: e.Name(), Path: p, Type: typ,
				Size: info.Size(), MTime: info.ModTime().Unix(),
			})
			if err != nil {
				log.Printf("scanner: create %s: %v", p, err)
				continue
			}
		}

		if isDir {
			onPath[realChild] = true
			err := s.scanDir(ctx, p, node.ID, builds, realChild, onPath)
			delete(onPath, realChild)
			if err != nil {
				log.Printf("scanner: recurse %s: %v", p, err)
			}
			continue
		}

		// Comic: enqueue cover build if new or changed.
		if !seen || node.Size != info.Size() || node.MTime != info.ModTime().Unix() {
			*builds = append(*builds, buildTask{node: node, size: info.Size(), mtime: info.ModTime().Unix()})
		}
	}

	// Anything still in byPath no longer exists on disk: remove subtree.
	for _, n := range byPath {
		s.removeNode(ctx, n)
	}
	return nil
}

func (s *Scanner) runBuilds(ctx context.Context, tasks []buildTask) {
	if len(tasks) == 0 {
		return
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > len(tasks) {
		workers = len(tasks)
	}
	ch := make(chan buildTask)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range ch {
				s.safeBuild(ctx, t)
			}
		}()
	}
	for _, t := range tasks {
		ch <- t
	}
	close(ch)
	wg.Wait()
}

// safeBuild shields the worker pool from decoder panics on malformed
// archives: one comic's panic must mark that comic CoverFailed, never kill
// the process (these goroutines have no other recover between them and main).
func (s *Scanner) safeBuild(ctx context.Context, t buildTask) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("scanner: build %s panicked: %v", t.node.Path, r)
			if err := s.repo.UpdateFileAttrs(ctx, t.node.ID, t.size, t.mtime, 0, store.CoverFailed); err != nil {
				log.Printf("scanner: mark cover failed %d: %v", t.node.ID, err)
			}
		}
	}()
	s.buildComic(ctx, t.node, t.size, t.mtime)
}

func (s *Scanner) buildComic(ctx context.Context, node *store.Node, size, mtime int64) {
	// Reset any stale extract cache so page count reflects current file.
	if err := os.RemoveAll(s.cacheDir(node.ID)); err != nil {
		log.Printf("scanner: evict extract cache %d: %v", node.ID, err)
	}
	// Drop stale page thumbs (keyed only on id:n) so they get regenerated.
	pagesDir := filepath.Join(s.dataDir, "thumbs", "pages", strconv.FormatInt(node.ID, 10))
	if err := os.RemoveAll(pagesDir); err != nil {
		log.Printf("scanner: evict page thumbs %d: %v", node.ID, err)
	}

	rc, count, err := archive.Probe(ctx, node.Path)
	if err != nil {
		log.Printf("scanner: first image %s: %v", node.Path, err)
		s.repo.UpdateFileAttrs(ctx, node.ID, size, mtime, 0, store.CoverFailed)
		return
	}
	defer rc.Close()
	coverStatus := store.CoverReady
	if err := s.writeThumb(node.ID, rc); err != nil {
		log.Printf("scanner: thumb %s: %v", node.Path, err)
		coverStatus = store.CoverFailed
	}
	if err := s.repo.UpdateFileAttrs(ctx, node.ID, size, mtime, count, coverStatus); err != nil {
		log.Printf("scanner: update attrs %s: %v", node.Path, err)
	}
}

func (s *Scanner) writeThumb(id int64, rc interface{ Read([]byte) (int, error) }) error {
	data, err := thumb.Generate(readerOnly{rc}, s.thumbWidth)
	if err != nil {
		return err
	}
	dst := s.thumbPath(id)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

type readerOnly struct {
	r interface{ Read([]byte) (int, error) }
}

func (r readerOnly) Read(p []byte) (int, error) { return r.r.Read(p) }

func (s *Scanner) removeNode(ctx context.Context, n *store.Node) {
	if n.Type == store.NodeDir {
		kids, err := s.repo.ListChildren(ctx, n.ID, -1, 0)
		if err != nil {
			log.Printf("scanner: list children of %d for deletion: %v; skipping subtree removal", n.ID, err)
			return
		}
		for _, k := range kids {
			s.removeNode(ctx, k)
		}
	}
	if err := os.Remove(s.thumbPath(n.ID)); err != nil && !os.IsNotExist(err) {
		log.Printf("scanner: remove thumb %d: %v", n.ID, err)
	}
	if err := os.RemoveAll(s.cacheDir(n.ID)); err != nil {
		log.Printf("scanner: remove extract cache %d: %v", n.ID, err)
	}
	pagesDir := filepath.Join(s.dataDir, "thumbs", "pages", strconv.FormatInt(n.ID, 10))
	if err := os.RemoveAll(pagesDir); err != nil {
		log.Printf("scanner: remove page thumbs %d: %v", n.ID, err)
	}
	if err := s.repo.Delete(ctx, n.ID); err != nil {
		log.Printf("scanner: delete node %d: %v", n.ID, err)
	}
}

package library

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"Tefnut/internal/archive"
	"Tefnut/internal/store"
	"Tefnut/internal/thumb"
)

type Scanner struct {
	repo       *store.NodeRepo
	root       string
	dataDir    string
	thumbWidth int
	mu         sync.Mutex
}

func NewScanner(repo *store.NodeRepo, root, dataDir string, thumbWidth int) *Scanner {
	return &Scanner{repo: repo, root: root, dataDir: dataDir, thumbWidth: thumbWidth}
}

func (s *Scanner) thumbPath(id int64) string {
	return filepath.Join(s.dataDir, "thumbs", strconv.FormatInt(id, 10)+".jpg")
}

func (s *Scanner) cacheDir(id int64) string {
	return filepath.Join(s.dataDir, "cache", strconv.FormatInt(id, 10))
}

// Scan performs a full idempotent sync of root into the DB tree.
func (s *Scanner) Scan(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	abs, err := filepath.Abs(s.root)
	if err != nil {
		return fmt.Errorf("scanner: abs root: %w", err)
	}
	return s.scanDir(ctx, abs, 0)
}

func (s *Scanner) scanDir(ctx context.Context, dir string, parentID int64) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("scanner: read dir %s: %w", dir, err)
	}

	existing, err := s.repo.ListChildren(ctx, parentID)
	if err != nil {
		return err
	}
	byPath := map[string]*store.Node{}
	for _, n := range existing {
		byPath[n.Path] = n
	}

	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		isDir := e.IsDir()
		if !isDir && !archive.IsArchive(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			log.Printf("scanner: stat %s: %v", p, err)
			continue
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
			if err := s.scanDir(ctx, p, node.ID); err != nil {
				log.Printf("scanner: recurse %s: %v", p, err)
			}
			continue
		}

		// Comic: (re)build cover + page count if new or changed.
		if !seen || node.Size != info.Size() || node.MTime != info.ModTime().Unix() {
			s.buildComic(ctx, node, info.Size(), info.ModTime().Unix())
		}
	}

	// Anything still in byPath no longer exists on disk: remove subtree.
	for _, n := range byPath {
		s.removeNode(ctx, n)
	}
	return nil
}

func (s *Scanner) buildComic(ctx context.Context, node *store.Node, size, mtime int64) {
	// Reset any stale extract cache so page count reflects current file.
	os.RemoveAll(s.cacheDir(node.ID))

	rc, _, count, err := archive.FirstImage(ctx, node.Path, s.cacheDir(node.ID))
	if err != nil {
		log.Printf("scanner: first image %s: %v", node.Path, err)
		s.repo.UpdateFileAttrs(ctx, node.ID, size, mtime, 0, store.CoverFailed)
		return
	}
	coverStatus := store.CoverReady
	if err := s.writeThumb(node.ID, rc); err != nil {
		log.Printf("scanner: thumb %s: %v", node.Path, err)
		coverStatus = store.CoverFailed
	}
	rc.Close()
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

type readerOnly struct{ r interface{ Read([]byte) (int, error) } }

func (r readerOnly) Read(p []byte) (int, error) { return r.r.Read(p) }

func (s *Scanner) removeNode(ctx context.Context, n *store.Node) {
	if n.Type == store.NodeDir {
		kids, err := s.repo.ListChildren(ctx, n.ID)
		if err == nil {
			for _, k := range kids {
				s.removeNode(ctx, k)
			}
		}
	}
	os.Remove(s.thumbPath(n.ID))
	os.RemoveAll(s.cacheDir(n.ID))
	if err := s.repo.Delete(ctx, n.ID); err != nil {
		log.Printf("scanner: delete node %d: %v", n.ID, err)
	}
}

package scan

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debouncer calls fn once after d of quiet following the last trigger().
type debouncer struct {
	d     time.Duration
	fn    func()
	mu    sync.Mutex
	timer *time.Timer
}

func newDebouncer(d time.Duration, fn func()) *debouncer {
	return &debouncer{d: d, fn: fn}
}

func (db *debouncer) trigger() {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.timer != nil {
		db.timer.Stop()
	}
	db.timer = time.AfterFunc(db.d, db.fn)
}

func (db *debouncer) stop() {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.timer != nil {
		db.timer.Stop()
		db.timer = nil
	}
}

// startWatchLocked must be called with m.mu held.
func (m *Manager) startWatchLocked(ctx context.Context) error {
	libs, err := m.paths.List(ctx)
	if err != nil {
		return err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	addTree := func(root string) {
		if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if aerr := w.Add(p); aerr != nil {
					log.Printf("scan: watch add %s: %v", p, aerr)
				}
			}
			return nil
		}); err != nil {
			log.Printf("scan: walk %s: %v", root, err)
		}
	}
	for _, lib := range libs {
		addTree(lib.Path)
	}

	// Route through ScanNow (guarded) so a watch-triggered scan never overlaps
	// an in-flight scan — notably the initial background scan started by Start.
	deb := newDebouncer(m.debounce, func() { m.ScanNow() })

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				// newly created directories must be watched too
				if ev.Op&fsnotify.Create == fsnotify.Create {
					if fi, statErr := os.Stat(ev.Name); statErr == nil && fi.IsDir() {
						addTree(ev.Name)
					}
				}
				deb.trigger()
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Printf("scan: watcher error: %v", err)
			}
		}
	}()

	m.stopMode = func() {
		close(done)
		deb.stop()
		w.Close()
	}
	return nil
}

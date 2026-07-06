package scan

import (
	"context"
	"log"
	"os"
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
		walkDirsFollowingSymlinks(root, func(dir string) {
			if err := w.Add(dir); err != nil {
				log.Printf("scan: watch add %s: %v", dir, err)
			}
		})
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

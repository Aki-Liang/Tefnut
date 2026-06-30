package scan

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"Tefnut/internal/store"
)

type fakeScanner struct {
	mu sync.Mutex
	n  int
	ch chan struct{}
}

func (f *fakeScanner) Scan(ctx context.Context) error {
	f.mu.Lock()
	f.n++
	f.mu.Unlock()
	if f.ch != nil {
		select {
		case f.ch <- struct{}{}:
		default:
		}
	}
	return nil
}
func (f *fakeScanner) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.n }

func newRepos(t *testing.T) (*store.SettingsRepo, *store.LibraryPathRepo) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return store.NewSettingsRepo(db), store.NewLibraryPathRepo(db)
}

func TestCronSpec(t *testing.T) {
	cases := []struct {
		in   store.ScanSettings
		want string
		err  bool
	}{
		{store.ScanSettings{Mode: "interval", Interval: "30m"}, "@every 30m", false},
		{store.ScanSettings{Mode: "interval", Interval: "bad"}, "", true},
		{store.ScanSettings{Mode: "daily", DailyTime: "03:05"}, "5 3 * * *", false},
		{store.ScanSettings{Mode: "daily", DailyTime: "9:99"}, "", true},
	}
	for _, c := range cases {
		got, err := cronSpec(c.in)
		if c.err && err == nil {
			t.Errorf("%+v: expected error", c.in)
		}
		if !c.err && got != c.want {
			t.Errorf("%+v: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestStartRunsInitialScan(t *testing.T) {
	settings, paths := newRepos(t)
	fs := &fakeScanner{}
	m := New(fs, settings, paths, t.TempDir(), 0)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if fs.count() < 1 {
		t.Fatalf("expected initial scan, count=%d", fs.count())
	}
}

func TestReconfigureTriggersScan(t *testing.T) {
	settings, paths := newRepos(t)
	fs := &fakeScanner{ch: make(chan struct{}, 4)}
	m := New(fs, settings, paths, t.TempDir(), 0)
	m.Start(context.Background())
	defer m.Stop()
	<-fs.ch // initial
	settings.SetScan(context.Background(), store.ScanSettings{Mode: "interval", Interval: "1h", DailyTime: "03:00"})
	if err := m.Reconfigure(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fs.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("reconfigure did not trigger a scan")
	}
}

func TestReconfigureUsesBaseContextNotRequestContext(t *testing.T) {
	settings, paths := newRepos(t)
	fs := &fakeScanner{ch: make(chan struct{}, 4)}
	m := New(fs, settings, paths, t.TempDir(), 0)
	m.Start(context.Background())
	defer m.Stop()
	<-fs.ch // initial scan
	// Caller passes an ALREADY-CANCELLED request context:
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Reconfigure(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fs.ch:
		// good: scan ran despite the cancelled request ctx (it used baseCtx)
	case <-time.After(2 * time.Second):
		t.Fatal("reconfigure rescan did not run — it must use the base context, not the cancelled request context")
	}
}

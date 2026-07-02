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
	mu    sync.Mutex
	n     int
	ch    chan struct{}
	block chan struct{} // if non-nil, Scan waits on it before returning
}

func (f *fakeScanner) Scan(ctx context.Context) error {
	f.mu.Lock()
	f.n++
	b := f.block
	f.mu.Unlock()
	if f.ch != nil {
		select {
		case f.ch <- struct{}{}:
		default:
		}
	}
	if b != nil {
		<-b
	}
	return nil
}
func (f *fakeScanner) count() int                { f.mu.Lock(); defer f.mu.Unlock(); return f.n }
func (f *fakeScanner) setBlock(ch chan struct{}) { f.mu.Lock(); f.block = ch; f.mu.Unlock() }

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
	fs := &fakeScanner{ch: make(chan struct{}, 1)}
	m := New(fs, settings, paths, t.TempDir(), Budgets{})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	select {
	case <-fs.ch: // the initial scan runs (in the background)
	case <-time.After(2 * time.Second):
		t.Fatalf("expected an initial scan, count=%d", fs.count())
	}
}

// A large first-run scan must not block startup: Start applies the mode and
// kicks the initial scan in the background, so the HTTP server can come up
// immediately. Regression for the "container unreachable on first boot" bug.
func TestStartDoesNotBlockOnInitialScan(t *testing.T) {
	settings, paths := newRepos(t)
	block := make(chan struct{})
	defer close(block) // release the background initial scan on exit
	fs := &fakeScanner{ch: make(chan struct{}, 1)}
	fs.setBlock(block) // the initial scan blocks until released
	m := New(fs, settings, paths, t.TempDir(), Budgets{})
	defer m.Stop()

	done := make(chan error, 1)
	go func() { done <- m.Start(context.Background()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start blocked on the initial scan (it must return before the scan finishes)")
	}
	// the initial scan should be running in the background
	select {
	case <-fs.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("initial scan did not start in the background")
	}
}

func TestReconfigureTriggersScan(t *testing.T) {
	settings, paths := newRepos(t)
	fs := &fakeScanner{ch: make(chan struct{}, 4)}
	m := New(fs, settings, paths, t.TempDir(), Budgets{})
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
	m := New(fs, settings, paths, t.TempDir(), Budgets{})
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

func TestScanNowGuard(t *testing.T) {
	settings, paths := newRepos(t)
	fs := &fakeScanner{}
	m := New(fs, settings, paths, t.TempDir(), Budgets{})

	block := make(chan struct{})
	fs.setBlock(block)

	if !m.ScanNow() {
		t.Fatal("first ScanNow should start a scan")
	}
	if m.ScanNow() {
		t.Fatal("second ScanNow should be skipped while a scan is running")
	}

	close(block) // release the held scan so the goroutine can clear the flag

	started := false
	for i := 0; i < 200; i++ {
		if m.ScanNow() {
			started = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !started {
		t.Fatal("ScanNow should start again after the prior scan finished")
	}
}

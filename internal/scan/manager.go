package scan

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"Tefnut/internal/cache"
	"Tefnut/internal/store"
)

// Scanner is the subset of library.Scanner the manager drives.
type Scanner interface {
	Scan(ctx context.Context) error
}

type Manager struct {
	scanner       Scanner
	settings      *store.SettingsRepo
	paths         *store.LibraryPathRepo
	dataDir       string
	cacheMaxBytes int64

	mu       sync.Mutex
	baseCtx  context.Context
	cron     *cron.Cron
	stopMode func() // tears down the current mode (cron stop / watcher close)
	debounce time.Duration
	scanning bool // guarded by mu; true while a ScanNow scan is in flight
}

func New(sc Scanner, settings *store.SettingsRepo, paths *store.LibraryPathRepo, dataDir string, cacheMaxBytes int64) *Manager {
	return &Manager{
		scanner:       sc,
		settings:      settings,
		paths:         paths,
		dataDir:       dataDir,
		cacheMaxBytes: cacheMaxBytes,
		debounce:      3 * time.Second,
	}
}

func (m *Manager) runScan(ctx context.Context) error {
	if err := m.scanner.Scan(ctx); err != nil {
		return err
	}
	if n, err := cache.Enforce(filepath.Join(m.dataDir, "cache"), m.cacheMaxBytes); err != nil {
		log.Printf("scan: enforce cache budget: %v", err)
	} else if n > 0 {
		log.Printf("scan: evicted %d cached extract dir(s)", n)
	}
	return nil
}

// Start applies the configured scan mode, then kicks the initial scan in the
// background. The initial scan must NOT block startup: a large first-run library
// can take minutes to extract every cover, and the HTTP server (started by the
// caller after Start returns) has to come up immediately. ScanNow's in-flight
// guard keeps this initial scan from overlapping a scheduled or manual one.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.baseCtx = ctx
	m.mu.Unlock()

	if err := m.applyMode(); err != nil {
		return err
	}
	m.ScanNow()
	return nil
}

// baseContext returns the long-lived context set by Start, or context.Background() as fallback.
func (m *Manager) baseContext() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.baseCtx == nil {
		return context.Background()
	}
	return m.baseCtx
}

// ScanNow starts a background scan of the configured libraries unless one is
// already running. It returns true if it started a scan, false if one was
// already in flight. The scan uses the long-lived base context, never a
// request context.
func (m *Manager) ScanNow() bool {
	m.mu.Lock()
	if m.scanning {
		m.mu.Unlock()
		return false
	}
	m.scanning = true
	base := m.baseCtx
	m.mu.Unlock()
	if base == nil {
		base = context.Background()
	}
	go func() {
		defer func() {
			m.mu.Lock()
			m.scanning = false
			m.mu.Unlock()
		}()
		if err := m.runScan(base); err != nil {
			log.Printf("scan: manual scan: %v", err)
		}
	}()
	return true
}

// Reconfigure tears down the current mode, starts the mode from current
// settings, then triggers an async rescan.
// ctx is accepted for interface compatibility but must NOT be used for scan/scheduled work.
func (m *Manager) Reconfigure(ctx context.Context) error {
	if err := m.applyMode(); err != nil {
		return err
	}
	base := m.baseContext()
	go func() {
		if err := m.runScan(base); err != nil {
			log.Printf("scan: reconfigure rescan: %v", err)
		}
	}()
	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teardownLocked()
}

func (m *Manager) teardownLocked() {
	if m.stopMode != nil {
		m.stopMode()
		m.stopMode = nil
	}
	m.cron = nil
}

func (m *Manager) applyMode() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teardownLocked()

	base := m.baseCtx
	if base == nil {
		base = context.Background()
	}

	settings, err := m.settings.GetScan(base)
	if err != nil {
		return fmt.Errorf("scan: read settings: %w", err)
	}

	switch settings.Mode {
	case "watch":
		return m.startWatchLocked(base)
	case "interval", "daily":
		spec, err := cronSpec(settings)
		if err != nil {
			return err
		}
		c := cron.New()
		if _, err := c.AddFunc(spec, func() {
			if err := m.runScan(base); err != nil {
				log.Printf("scan: scheduled scan: %v", err)
			}
		}); err != nil {
			return fmt.Errorf("scan: schedule %q: %w", spec, err)
		}
		c.Start()
		m.cron = c
		m.stopMode = func() { <-c.Stop().Done() }
		return nil
	default:
		return fmt.Errorf("scan: unknown mode %q", settings.Mode)
	}
}

// cronSpec converts settings into a robfig/cron spec. Watch mode returns "".
func cronSpec(s store.ScanSettings) (string, error) {
	switch s.Mode {
	case "interval":
		d, err := time.ParseDuration(s.Interval)
		if err != nil || d <= 0 {
			return "", fmt.Errorf("scan: invalid interval %q", s.Interval)
		}
		return "@every " + s.Interval, nil
	case "daily":
		h, min, err := parseHHMM(s.DailyTime)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d * * *", min, h), nil
	case "watch":
		return "", nil
	default:
		return "", fmt.Errorf("scan: unknown mode %q", s.Mode)
	}
}

func parseHHMM(v string) (hour, min int, err error) {
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("scan: invalid time %q", v)
	}
	hour, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || hour < 0 || hour > 23 || min < 0 || min > 59 {
		return 0, 0, fmt.Errorf("scan: invalid time %q", v)
	}
	return hour, min, nil
}

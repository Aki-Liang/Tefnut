package scan

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"Tefnut/internal/store"
)

// Scanner is the subset of library.Scanner the manager drives.
type Scanner interface {
	Scan(ctx context.Context) error
}

type Manager struct {
	scanner  Scanner
	settings *store.SettingsRepo
	paths    *store.LibraryPathRepo

	mu       sync.Mutex
	cron     *cron.Cron
	stopMode func() // tears down the current mode (cron stop / watcher close)
	debounce time.Duration
}

func New(sc Scanner, settings *store.SettingsRepo, paths *store.LibraryPathRepo) *Manager {
	return &Manager{scanner: sc, settings: settings, paths: paths, debounce: 3 * time.Second}
}

// Start runs one blocking scan, then starts the active mode.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.scanner.Scan(ctx); err != nil {
		log.Printf("scan: initial scan: %v", err)
	}
	return m.applyMode(ctx)
}

// Reconfigure tears down the current mode, starts the mode from current
// settings, then triggers an async rescan.
func (m *Manager) Reconfigure(ctx context.Context) error {
	if err := m.applyMode(ctx); err != nil {
		return err
	}
	go func() {
		if err := m.scanner.Scan(ctx); err != nil {
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

func (m *Manager) applyMode(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teardownLocked()

	settings, err := m.settings.GetScan(ctx)
	if err != nil {
		return fmt.Errorf("scan: read settings: %w", err)
	}

	switch settings.Mode {
	case "watch":
		return m.startWatchLocked(ctx)
	case "interval", "daily":
		spec, err := cronSpec(settings)
		if err != nil {
			return err
		}
		c := cron.New()
		if _, err := c.AddFunc(spec, func() {
			if err := m.scanner.Scan(ctx); err != nil {
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

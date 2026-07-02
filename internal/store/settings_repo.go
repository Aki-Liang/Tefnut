package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

type ScanSettings struct {
	Mode      string
	Interval  string
	DailyTime string
}

const (
	keyScanMode       = "scan_mode"
	keyScanInterval   = "scan_interval"
	keyScanDaily      = "scan_daily_time"
	keyCacheMaxBytes  = "cache_max_bytes"
	keyThumbPagesMaxBytes = "thumb_pages_max_bytes"

	defScanMode     = "interval"
	defScanInterval = "1h"
	defScanDaily    = "03:00"
)

type SettingsRepo struct {
	rdb *sql.DB
	wdb *sql.DB
}

func NewSettingsRepo(db *DB) *SettingsRepo { return &SettingsRepo{rdb: db.Read(), wdb: db.Write()} }

func (r *SettingsRepo) get(ctx context.Context, key, def string) (string, error) {
	var v string
	err := r.rdb.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	if err != nil {
		return "", fmt.Errorf("store: get setting %q: %w", key, err)
	}
	return v, nil
}

func (r *SettingsRepo) GetScan(ctx context.Context) (ScanSettings, error) {
	mode, err := r.get(ctx, keyScanMode, defScanMode)
	if err != nil {
		return ScanSettings{}, err
	}
	interval, err := r.get(ctx, keyScanInterval, defScanInterval)
	if err != nil {
		return ScanSettings{}, err
	}
	daily, err := r.get(ctx, keyScanDaily, defScanDaily)
	if err != nil {
		return ScanSettings{}, err
	}
	return ScanSettings{Mode: mode, Interval: interval, DailyTime: daily}, nil
}

func (r *SettingsRepo) SetScan(ctx context.Context, s ScanSettings) error {
	tx, err := r.wdb.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()
	pairs := [][2]string{
		{keyScanMode, s.Mode},
		{keyScanInterval, s.Interval},
		{keyScanDaily, s.DailyTime},
	}
	for _, p := range pairs {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, p[0], p[1])
		if err != nil {
			return fmt.Errorf("store: set setting %q: %w", p[0], err)
		}
	}
	return tx.Commit()
}

// lookup returns (value, found) without a default, distinguishing a missing
// row from any stored value.
func (r *SettingsRepo) lookup(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := r.rdb.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: get setting %q: %w", key, err)
	}
	return v, true, nil
}

func (r *SettingsRepo) getInt64(ctx context.Context, key string, def int64) (int64, error) {
	v, found, err := r.lookup(ctx, key)
	if err != nil {
		return 0, err
	}
	if !found {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		// A corrupt stored value must surface, not silently fall back: the
		// caller may be about to evict caches against this number.
		return 0, fmt.Errorf("store: setting %q has non-numeric value %q", key, v)
	}
	return n, nil
}

// GetBudgets returns the effective disk budgets: values saved from the
// settings UI win; keys never saved fall back to the given defaults
// (config file / env, resolved by the caller). <=0 means unlimited.
func (r *SettingsRepo) GetBudgets(ctx context.Context, defCache, defPageThumb int64) (int64, int64, error) {
	cache, err := r.getInt64(ctx, keyCacheMaxBytes, defCache)
	if err != nil {
		return 0, 0, err
	}
	pageThumb, err := r.getInt64(ctx, keyThumbPagesMaxBytes, defPageThumb)
	if err != nil {
		return 0, 0, err
	}
	return cache, pageThumb, nil
}

// SetBudgets persists both budgets (bytes) in one transaction.
func (r *SettingsRepo) SetBudgets(ctx context.Context, cacheMax, pageThumbMax int64) error {
	tx, err := r.wdb.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()
	pairs := [][2]string{
		{keyCacheMaxBytes, strconv.FormatInt(cacheMax, 10)},
		{keyThumbPagesMaxBytes, strconv.FormatInt(pageThumbMax, 10)},
	}
	for _, p := range pairs {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, p[0], p[1])
		if err != nil {
			return fmt.Errorf("store: set setting %q: %w", p[0], err)
		}
	}
	return tx.Commit()
}

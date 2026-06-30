package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type ScanSettings struct {
	Mode      string
	Interval  string
	DailyTime string
}

const (
	keyScanMode     = "scan_mode"
	keyScanInterval = "scan_interval"
	keyScanDaily    = "scan_daily_time"

	defScanMode     = "interval"
	defScanInterval = "2m"
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

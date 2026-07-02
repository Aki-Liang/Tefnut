package store

import (
	"context"
	"testing"
)

func TestGetScanDefaults(t *testing.T) {
	r := NewSettingsRepo(openTemp(t))
	s, err := r.GetScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != "interval" || s.Interval != "1h" || s.DailyTime != "03:00" {
		t.Fatalf("defaults wrong: %+v", s)
	}
}

func TestSetGetScan(t *testing.T) {
	ctx := context.Background()
	r := NewSettingsRepo(openTemp(t))
	want := ScanSettings{Mode: "daily", Interval: "1h", DailyTime: "04:30"}
	if err := r.SetScan(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetScan(ctx)
	if err != nil || got != want {
		t.Fatalf("got %+v err %v", got, err)
	}
	// partial override still returns stored values for set keys
	if err := r.SetScan(ctx, ScanSettings{Mode: "watch", Interval: "1h", DailyTime: "04:30"}); err != nil {
		t.Fatal(err)
	}
	got, _ = r.GetScan(ctx)
	if got.Mode != "watch" {
		t.Fatalf("mode = %s", got.Mode)
	}
}

func TestGetBudgetsFallsBackToDefaults(t *testing.T) {
	r := NewSettingsRepo(openTemp(t))
	cache, thumb, err := r.GetBudgets(context.Background(), 111, 222)
	if err != nil {
		t.Fatalf("GetBudgets: %v", err)
	}
	if cache != 111 || thumb != 222 {
		t.Errorf("got %d/%d, want defaults 111/222", cache, thumb)
	}
}

func TestSetBudgetsRoundTrip(t *testing.T) {
	r := NewSettingsRepo(openTemp(t))
	if err := r.SetBudgets(context.Background(), 1<<30, 64<<20); err != nil {
		t.Fatalf("SetBudgets: %v", err)
	}
	cache, thumb, err := r.GetBudgets(context.Background(), 111, 222)
	if err != nil {
		t.Fatalf("GetBudgets: %v", err)
	}
	if cache != 1<<30 || thumb != 64<<20 {
		t.Errorf("got %d/%d, want saved values (DB must beat defaults)", cache, thumb)
	}
}

func TestGetBudgetsRejectsCorruptValue(t *testing.T) {
	db := openTemp(t)
	r := NewSettingsRepo(db)
	if _, err := db.Write().Exec(
		`INSERT INTO settings (key, value) VALUES ('cache_max_bytes', 'garbage')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.GetBudgets(context.Background(), 1, 2); err == nil {
		t.Fatal("expected error for non-numeric stored value, got nil (must not silently fall back)")
	}
}

func TestGetBudgetsMixedStoredAndDefault(t *testing.T) {
	db := openTemp(t)
	r := NewSettingsRepo(db)
	if _, err := db.Write().Exec(
		`INSERT INTO settings (key, value) VALUES ('cache_max_bytes', '4096')`); err != nil {
		t.Fatal(err)
	}
	cache, thumb, err := r.GetBudgets(context.Background(), 111, 222)
	if err != nil {
		t.Fatalf("GetBudgets: %v", err)
	}
	if cache != 4096 || thumb != 222 {
		t.Errorf("got %d/%d, want stored 4096 + default 222", cache, thumb)
	}
}

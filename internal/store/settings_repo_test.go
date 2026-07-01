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

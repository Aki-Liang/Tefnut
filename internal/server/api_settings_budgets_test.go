package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"Tefnut/internal/store"
)

func TestGetSettingsReturnsDefaultBudgets(t *testing.T) {
	_, e, _ := newTestServer(t) // defaults: 1<<30 / 64<<20
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"cacheMaxBytes":1073741824`) ||
		!strings.Contains(body, `"thumbPagesMaxBytes":67108864`) {
		t.Fatalf("defaults missing in body: %s", body)
	}
}

func TestPutSettingsSavesBudgetsAndReconfigures(t *testing.T) {
	s, e, db := newTestServer(t)
	stub := s.reconf.(*stubReconf)
	req := httptest.NewRequest(http.MethodPut, "/api/settings",
		strings.NewReader(`{"cacheMaxBytes":2048,"thumbPagesMaxBytes":1024}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	cache, thumb, err := store.NewSettingsRepo(db).GetBudgets(context.Background(), 0, 0)
	if err != nil || cache != 2048 || thumb != 1024 {
		t.Fatalf("persisted = %d/%d err=%v", cache, thumb, err)
	}
	if stub.calls != 1 {
		t.Fatalf("Reconfigure calls = %d, want 1", stub.calls)
	}
}

func TestPutSettingsRejectsNegativeBudget(t *testing.T) {
	_, e, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/settings",
		strings.NewReader(`{"cacheMaxBytes":-1}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// 旧客户端只发 scan 字段 → 预算不被触碰。
func TestPutScanOnlyBodyLeavesBudgetsUnset(t *testing.T) {
	_, e, db := newTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/settings",
		strings.NewReader(`{"scanMode":"watch"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	cache, thumb, err := store.NewSettingsRepo(db).GetBudgets(context.Background(), 7, 8)
	if err != nil || cache != 7 || thumb != 8 {
		t.Fatalf("budgets must stay unset (fall back to 7/8), got %d/%d err=%v", cache, thumb, err)
	}
}

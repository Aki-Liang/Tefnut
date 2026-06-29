package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/store"
)

type settingsDTO struct {
	LibraryPaths  []*store.LibraryPath `json:"libraryPaths"`
	ScanMode      string               `json:"scanMode"`
	ScanInterval  string               `json:"scanInterval"`
	ScanDailyTime string               `json:"scanDailyTime"`
}

func (s *Server) apiGetSettings(c echo.Context) error {
	ctx := c.Request().Context()
	scan, err := s.settings.GetScan(ctx)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	paths, err := s.paths.List(ctx)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, settingsDTO{
		LibraryPaths: paths, ScanMode: scan.Mode,
		ScanInterval: scan.Interval, ScanDailyTime: scan.DailyTime,
	})
}

func validScanSettings(mode, interval, daily string) error {
	switch mode {
	case "interval":
		if d, err := time.ParseDuration(interval); err != nil || d <= 0 {
			return errors.New("scanInterval must be a positive duration like 30m or 2h")
		}
	case "daily":
		if !validHHMM(daily) {
			return errors.New("scanDailyTime must be HH:MM (00:00-23:59)")
		}
	case "watch":
		// no extra params
	default:
		return errors.New("scanMode must be interval, daily, or watch")
	}
	return nil
}

func validHHMM(v string) bool {
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return false
	}
	h, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	return e1 == nil && e2 == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59
}

func (s *Server) apiUpdateSettings(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		ScanMode      string `json:"scanMode"`
		ScanInterval  string `json:"scanInterval"`
		ScanDailyTime string `json:"scanDailyTime"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	if err := validScanSettings(body.ScanMode, body.ScanInterval, body.ScanDailyTime); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	if err := s.settings.SetScan(ctx, store.ScanSettings{
		Mode: body.ScanMode, Interval: body.ScanInterval, DailyTime: body.ScanDailyTime,
	}); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if err := s.reconf.Reconfigure(ctx); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}

func (s *Server) apiAddPath(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	p := strings.TrimSpace(body.Path)
	if p == "" {
		return fail(c, http.StatusBadRequest, errors.New("path is required"))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return fail(c, http.StatusBadRequest, errors.New("path is not a readable directory"))
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = filepath.Base(abs)
	}
	lp, err := s.paths.Add(ctx, name, abs)
	if errors.Is(err, store.ErrDuplicate) {
		return fail(c, http.StatusConflict, errors.New("this path is already a library"))
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if err := s.reconf.Reconfigure(ctx); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, lp)
}

func (s *Server) apiDeletePath(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, errors.New("invalid id"))
	}
	if err := s.paths.Delete(ctx, id); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if err := s.reconf.Reconfigure(ctx); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}

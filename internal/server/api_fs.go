package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

type dirEntryDTO struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type fsRootsDTO struct {
	Roots []dirEntryDTO `json:"roots"`
}

type fsDirsDTO struct {
	Path   string        `json:"path"`
	Parent string        `json:"parent"`
	Dirs   []dirEntryDTO `json:"dirs"`
}

// apiFsDirs lists server-side directories for the library path picker.
// Without ?path it returns the configured allowed roots; with ?path it lists
// that directory's immediate subdirectories. Every path is validated against
// allowedRoots with the same fail-closed guard as apiAddPath, and only
// directory names are exposed — never files or metadata.
func (s *Server) apiFsDirs(c echo.Context) error {
	q := strings.TrimSpace(c.QueryParam("path"))
	if q == "" {
		return ok(c, fsRootsDTO{Roots: s.existingRoots()})
	}
	if !filepath.IsAbs(q) {
		return fail(c, http.StatusBadRequest, errors.New("path 必须是绝对路径"))
	}
	abs := filepath.Clean(q)
	if within, err := PathWithinRoots(abs, s.allowedRoots); err != nil || !within {
		return fail(c, http.StatusBadRequest, errors.New("目录必须在允许的库根之内"))
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fail(c, http.StatusBadRequest, errors.New("目录不可读"))
	}
	dirs := make([]dirEntryDTO, 0, len(entries))
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		p := filepath.Join(abs, ent.Name())
		if !entryIsDir(ent, p) {
			continue
		}
		dirs = append(dirs, dirEntryDTO{Name: ent.Name(), Path: p})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	return ok(c, fsDirsDTO{Path: abs, Parent: s.parentWithinRoots(abs), Dirs: dirs})
}

// existingRoots returns the configured allowedRoots that exist as directories,
// in config order, for the picker's initial view.
func (s *Server) existingRoots() []dirEntryDTO {
	roots := make([]dirEntryDTO, 0, len(s.allowedRoots))
	for _, r := range s.allowedRoots {
		info, err := os.Stat(r)
		if err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, dirEntryDTO{Name: filepath.Base(r), Path: r})
	}
	return roots
}

// parentWithinRoots returns abs's parent directory, or "" when the parent
// leaves every allowed root, so the UI falls back to the roots view instead
// of climbing out of the jail.
func (s *Server) parentWithinRoots(abs string) string {
	parent := filepath.Dir(abs)
	if parent == abs {
		return ""
	}
	if within, err := PathWithinRoots(parent, s.allowedRoots); err != nil || !within {
		return ""
	}
	return parent
}

// entryIsDir reports whether ent names a directory, following symlinks so a
// symlinked library folder still shows up in the picker. Entering it remains
// guarded by PathWithinRoots on the follow-up request.
func entryIsDir(ent os.DirEntry, path string) bool {
	if ent.IsDir() {
		return true
	}
	if ent.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

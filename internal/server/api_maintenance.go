package server

import (
	"log"
	"net/http"
	"path/filepath"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/cache"
)

// apiScanStatus reports whether a library scan is in flight, so the browse
// page's scan button can reflect (and poll) the real state.
func (s *Server) apiScanStatus(c echo.Context) error {
	return ok(c, map[string]any{"scanning": s.reconf.Scanning()})
}

// apiCacheClear empties the rebuildable disk caches — extracted archive pages
// and per-page thumbnails — and reports the bytes freed. Cover thumbnails are
// kept: they are metadata whose rebuild would require a full library rescan.
// Open readers self-heal on the next page request (openPage drops and
// re-extracts when its cache dir vanishes).
func (s *Server) apiCacheClear(c echo.Context) error {
	var freed int64
	for _, root := range []string{
		filepath.Join(s.dataDir, "cache"),
		filepath.Join(s.dataDir, "thumbs", "pages"),
	} {
		n, err := cache.Clear(root)
		freed += n
		if err != nil {
			log.Printf("server: clear cache %s: %v", root, err)
			return fail(c, http.StatusInternalServerError, err)
		}
	}
	// Cached archive readers still point at the extracted files we just
	// deleted; reset them so the next page request re-extracts cleanly instead
	// of failing once before the per-request self-heal kicks in.
	s.readers.Close()
	return ok(c, map[string]any{"freedBytes": freed})
}

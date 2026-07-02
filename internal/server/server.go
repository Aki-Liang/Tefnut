package server

import (
	"context"
	"path/filepath"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/archive"
	"Tefnut/internal/server/web"
	"Tefnut/internal/store"
)

const archiveCacheSize = 8
const decodeConcurrency = 4

// Reconfigurer is satisfied by *scan.Manager.
type Reconfigurer interface {
	Reconfigure(ctx context.Context) error
	ScanNow() bool
}

type Server struct {
	nodes           *store.NodeRepo
	tags            *store.TagRepo
	progress        *store.ProgressRepo
	settings        *store.SettingsRepo
	paths           *store.LibraryPathRepo
	reconf          Reconfigurer
	dataDir         string
	thumbWidth      int
	pageThumbWidth  int
	thumbs          *thumbCache
	readers         *archive.ReaderCache
	decodeSem       chan struct{}
	allowedRoots    []string
	defCacheMax     int64 // effective-budget fallback (config/env) for settings API
	defPageThumbMax int64
}

const thumbCacheMaxEntries = 512

func NewServer(nodes *store.NodeRepo, tags *store.TagRepo, progress *store.ProgressRepo,
	settings *store.SettingsRepo, paths *store.LibraryPathRepo, reconf Reconfigurer,
	dataDir string, thumbWidth int, pageThumbWidth int, allowedRoots []string,
	defCacheMax, defPageThumbMax int64) *Server {
	// lru.New only errors on size<=0; thumbCacheMaxEntries is a positive
	// constant, so the ignored error is safe.
	tc, _ := newThumbCache(thumbCacheMaxEntries, filepath.Join(dataDir, "thumbs"))
	return &Server{nodes: nodes, tags: tags, progress: progress, settings: settings,
		paths: paths, reconf: reconf, dataDir: dataDir, thumbWidth: thumbWidth,
		pageThumbWidth: pageThumbWidth,
		thumbs:         tc, readers: archive.NewReaderCache(archiveCacheSize),
		decodeSem:       make(chan struct{}, decodeConcurrency),
		allowedRoots:    allowedRoots,
		defCacheMax:     defCacheMax,
		defPageThumbMax: defPageThumbMax}
}

// Register wires routes and static assets onto e.
func (s *Server) Register(e *echo.Echo) {
	e.GET("/healthz", func(c echo.Context) error { return c.String(200, "ok") })
	e.StaticFS("/static", web.Static)

	// Rendered pages (implemented in pages.go).
	e.GET("/", s.pageBrowse)
	e.GET("/folder/:id", s.pageBrowse)
	e.GET("/read/:id", s.pageReader)
	e.GET("/comic/:id", s.pageComicDetail)
	e.GET("/tags", s.pageTags)
	e.GET("/settings", s.pageSettings)

	// JSON / binary API.
	api := e.Group("/api")
	api.GET("/nodes", s.apiNodes)
	api.GET("/comics/:id", s.apiComicDetail)
	api.GET("/comics/:id/cover", s.apiCover)
	api.GET("/comics/:id/pages/:n", s.apiPage)
	api.GET("/comics/:id/pages/:n/thumb", s.apiPageThumb)
	api.PATCH("/comics/:id", s.apiUpdateMeta)
	api.POST("/comics/:id/tags", s.apiAddTag)
	api.DELETE("/comics/:id/tags/:tagId", s.apiRemoveTag)
	api.PUT("/comics/:id/progress", s.apiSetProgress)
	api.GET("/tags", s.apiListTags)
	api.POST("/tags", s.apiCreateTag)
	api.PATCH("/tags/:id", s.apiRenameTag)
	api.DELETE("/tags/:id", s.apiDeleteTag)

	api.GET("/settings", s.apiGetSettings)
	api.PUT("/settings", s.apiUpdateSettings)
	api.POST("/settings/paths", s.apiAddPath)
	api.DELETE("/settings/paths/:id", s.apiDeletePath)

	api.POST("/scan", s.apiScanNow)

}

package server

import (
	"context"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/server/web"
	"Tefnut/internal/store"
)

// Reconfigurer is satisfied by *scan.Manager via its Reconfigure method.
type Reconfigurer interface {
	Reconfigure(ctx context.Context) error
}

type Server struct {
	nodes      *store.NodeRepo
	tags       *store.TagRepo
	progress   *store.ProgressRepo
	settings   *store.SettingsRepo
	paths      *store.LibraryPathRepo
	reconf     Reconfigurer
	dataDir    string
	thumbWidth int
	thumbs     *thumbCache
}

func NewServer(nodes *store.NodeRepo, tags *store.TagRepo, progress *store.ProgressRepo,
	settings *store.SettingsRepo, paths *store.LibraryPathRepo, reconf Reconfigurer,
	dataDir string, thumbWidth int) *Server {
	return &Server{nodes: nodes, tags: tags, progress: progress, settings: settings,
		paths: paths, reconf: reconf, dataDir: dataDir, thumbWidth: thumbWidth,
		thumbs: newThumbCache(256)}
}

// Register wires routes and static assets onto e.
func (s *Server) Register(e *echo.Echo) {
	e.GET("/healthz", func(c echo.Context) error { return c.String(200, "ok") })
	e.StaticFS("/static", web.Static)

	// Rendered pages (implemented in pages.go).
	e.GET("/", s.pageBrowse)
	e.GET("/folder/:id", s.pageBrowse)
	e.GET("/read/:id", s.pageReader)
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

}

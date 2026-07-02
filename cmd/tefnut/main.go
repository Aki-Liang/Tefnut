package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"Tefnut/internal/config"
	"Tefnut/internal/library"
	"Tefnut/internal/scan"
	"Tefnut/internal/server"
	"Tefnut/internal/store"
)

func main() {
	cfgPath := flag.String("config", "./config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}

	db, err := store.Open(filepath.Join(cfg.DataDir, "tefnut.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	nodes := store.NewNodeRepo(db)
	tags := store.NewTagRepo(db)
	progress := store.NewProgressRepo(db)

	settingsRepo := store.NewSettingsRepo(db)
	pathRepo := store.NewLibraryPathRepo(db)

	// First-run seed: import the yaml rootPath as the first library.
	if libs, err := pathRepo.List(context.Background()); err == nil && len(libs) == 0 && cfg.Library.RootPath != "" {
		if _, err := pathRepo.Add(context.Background(), filepath.Base(cfg.Library.RootPath), cfg.Library.RootPath); err != nil {
			log.Printf("seed library path: %v", err)
		}
	}

	scanner := library.NewScanner(nodes, pathRepo, cfg.DataDir, cfg.Thumbnail.Width)

	manager := scan.New(scanner, settingsRepo, pathRepo, cfg.DataDir, scan.Budgets{
		ExtractCacheBytes: int64(cfg.Cache.MaxBytes),
		PageThumbBytes:    int64(cfg.Thumbnail.PagesMaxBytes),
	})
	if err := manager.Start(context.Background()); err != nil {
		log.Printf("scan manager start: %v", err)
	}
	defer manager.Stop()

	srv := server.NewServer(nodes, tags, progress, settingsRepo, pathRepo, manager,
		cfg.DataDir, cfg.Thumbnail.Width, cfg.Thumbnail.PageWidth, cfg.Library.AllowedRoots,
		int64(cfg.Cache.MaxBytes), int64(cfg.Thumbnail.PagesMaxBytes))

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	// Gzip HTML/JSON/JS/CSS but skip already-compressed image bodies on the
	// hottest endpoints (page, page-thumb, cover) — no savings, wasted CPU, and
	// it would buffer streamed pages.
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Skipper: func(c echo.Context) bool {
			p := c.Path() // registered route template
			return strings.Contains(p, "/pages/") || strings.HasSuffix(p, "/cover")
		},
	}))
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		ContentTypeNosniff: "nosniff",
	}))
	srv.Register(e)

	log.Printf("tefnut listening on %s, library=%s", cfg.Server.Addr, cfg.Library.RootPath)
	if err := e.Start(cfg.Server.Addr); err != nil {
		log.Fatal(err)
	}
}

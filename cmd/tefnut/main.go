package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/robfig/cron/v3"

	"Tefnut/internal/config"
	"Tefnut/internal/library"
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

	scanner := library.NewScanner(nodes, cfg.Library.RootPath, cfg.DataDir, cfg.Thumbnail.Width)

	// Startup scan (blocking once, so the library is populated before serving).
	if err := scanner.Scan(context.Background()); err != nil {
		log.Printf("initial scan: %v", err)
	}

	// Periodic scan.
	interval, _ := cfg.ScanInterval()
	c := cron.New()
	if _, err := c.AddFunc("@every "+interval.String(), func() {
		if err := scanner.Scan(context.Background()); err != nil {
			log.Printf("scan: %v", err)
		}
	}); err != nil {
		log.Fatalf("cron schedule: %v", err)
	}
	c.Start()
	defer c.Stop()

	srv := server.NewServer(nodes, tags, progress, cfg.DataDir, cfg.Thumbnail.Width)

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	srv.Register(e)

	log.Printf("tefnut listening on %s, library=%s", cfg.Server.Addr, cfg.Library.RootPath)
	if err := e.Start(cfg.Server.Addr); err != nil {
		log.Fatal(err)
	}
}

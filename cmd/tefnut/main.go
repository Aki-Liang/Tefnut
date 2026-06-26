package main

import (
	"flag"
	"log"

	"Tefnut/internal/config"
)

func main() {
	cfgPath := flag.String("config", "./config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("tefnut: loaded config, library=%s dataDir=%s addr=%s",
		cfg.Library.RootPath, cfg.DataDir, cfg.Server.Addr)
}

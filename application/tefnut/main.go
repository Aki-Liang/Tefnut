package main

import (
	"Tefnut/application"
	"Tefnut/configs"
	service_impl "Tefnut/internal/domain/service/impl"
	"Tefnut/internal/handler"
	"Tefnut/internal/handler/cron_impl"
	"Tefnut/internal/handler/handler_impl"
	"Tefnut/internal/infrastructure/repository_impl"
	_ "github.com/go-sql-driver/mysql"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/robfig/cron/v3"
	"xorm.io/xorm"
)

func main() {
	app := application.GetApp()
	err := configs.NewConfig().Load("./config.yaml", app.Config)
	if err != nil {
		panic(err)
	}

	engine, err := xorm.NewEngine("mysql", app.Config.DbConfig.Conn)
	if err != nil {
		panic(err)
	}

	fsRepo := repository_impl.NewFileSystemRepository(engine)
	fs := service_impl.NewFileService().SetConfig(app.Config.FsConfig).SetFileRepository(fsRepo)

	/*
		cron
	*/
	fileScanCron := cron_impl.NewFileScanCron().
		SetFSService(fs)
	c := cron.New()
	c.AddFunc("*/2 * * * *", fileScanCron.Scan) //for test now
	c.Start()

	/*
		http
	*/
	tefnutHandlerImpl := handler_impl.NewTefnutHandlerImpl().
		SetFSService(fs)
	tefnutHandler := handler.NewTefnutHandler().SetImpl(tefnutHandlerImpl)
	e := echo.New()
	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	// Routes
	g := e.Group("/tefnut/api/v1")
	gLib := g.Group("/library")
	gLib.GET("/list", tefnutHandler.LibList)
	e.Logger.Fatal(e.Start(":8086"))
}

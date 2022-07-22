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
	engine.ShowSQL(true)

	//repository
	fsRepo := repository_impl.NewLibraryRepositoryImpl(engine)

	//service
	libService := service_impl.NewLibraryServiceImpl().SetConfig(app.Config.FsConfig).SetLibraryRepository(fsRepo)

	//cron
	fileScanCron := cron_impl.NewFileScanCron().SetLibraryService(libService)
	c := cron.New()
	c.AddFunc("*/2 * * * *", fileScanCron.Scan) //for test now
	c.Start()

	//http handler
	tefnutHandlerImpl := handler_impl.NewTefnutHandlerImpl().SetLibraryService(libService)
	tefnutHandler := handler.NewTefnutHandler().SetImpl(tefnutHandlerImpl)

	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	root := e.Group("/tefnut")
	root.Static("/resource", app.Config.FsConfig.TempPath)

	g := root.Group("/api/v1")
	// library
	gLib := g.Group("/library")
	gLib.Any("/list", tefnutHandler.LibList)
	gLib.GET("/content/:id", tefnutHandler.LibContentGet)

	e.Logger.Fatal(e.Start(":8086"))
}

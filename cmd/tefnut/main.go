package main


import (
    "Tefnut/configs"
    service_impl "Tefnut/internal/domain/service/impl"
    "Tefnut/internal/infrastructure/repository_impl"
    "context"
    _ "github.com/go-sql-driver/mysql"
    "xorm.io/xorm"
)

type ApplicationConfig struct {
    FsConfig *configs.FilesystemConfig `yaml:"fileConfig"`
    DbConfig *configs.DatabaseConfig `yaml:"database"`
}

func main() {
    conf := &ApplicationConfig{}
    err := configs.NewConfig().Load("./config.yaml", conf)
    if err != nil {
        panic(err)
    }

    engine, err := xorm.NewEngine("mysql", conf.DbConfig.Conn)
    if err != nil {
        panic(err)
    }

    fsRepo := repository_impl.NewFileSystemRepository(engine)
    fs := service_impl.NewFileService().SetConfig(conf.FsConfig).SetFileRepository(fsRepo)
    fs.ScanRoot(context.Background())
}

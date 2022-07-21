package application

import "Tefnut/configs"

var App = &Application{
	Config: &ApplicationConfig{},
}

type ApplicationConfig struct {
	FsConfig *configs.FilesystemConfig `yaml:"fileConfig"`
	DbConfig *configs.DatabaseConfig   `yaml:"database"`
}

type Application struct {
	Config *ApplicationConfig
}

func GetApp() *Application {
	return App
}

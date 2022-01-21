package configs

type FilesystemConfig struct {
    RootPath string `yaml:"rootPath"`
    TempPath string `yaml:"tempPath"`
}

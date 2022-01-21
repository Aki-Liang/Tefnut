package configs

import (
    "gopkg.in/yaml.v2"
    "os"
)

type Config struct {

}

func NewConfig() *Config {
    return &Config{}
}

func (c *Config)Load(path string, i interface{}) error {
    confData, err := os.ReadFile(path)
    if err != nil {
        return err
    }
    err = yaml.Unmarshal(confData, i)
    if err != nil {
        return err
    }
    return nil
}

package configs


type DatabaseConfig struct {
    Name        string
    Conn        string
    MaxOpenConn int `yaml:"max_open_conn"`
    MaxIdleConn int `yaml:"max_idle_conn"`
}

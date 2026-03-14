package db

type Database struct {
	Name       string   `yaml:"name"`       // 数据库名
	Params     []string `yaml:"params"`     // 数据库参数
	AutoCreate bool     `yaml:"autoCreate"` // 是否自动创建数据库
	InMemory   bool     `yaml:"inMemory"`   // sqlite 使用
	// 以下为sqlite使用目录
	DbDir    string `yaml:"dbDir"`
	LogLevel string `yaml:"logLevel"`
}

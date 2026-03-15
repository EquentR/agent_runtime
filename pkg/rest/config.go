package rest

type Config struct {
	Host        string   `yaml:"host"`
	Port        int      `yaml:"port"`
	ApiBasePath string   `yaml:"apiBasePath"`
	StaticPaths []Static `yaml:"staticPaths"`
}

type Static struct {
	Path string `yaml:"path"`
	Dir  string `yaml:"dir"`
}

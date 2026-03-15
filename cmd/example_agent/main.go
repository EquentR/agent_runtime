package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/EquentR/agent_runtime/app/commands"
	"github.com/EquentR/agent_runtime/app/config"
	"gopkg.in/yaml.v3"
)

var (
	Version   = "0.0.1-dev"
	GitCommit = "none"
)

var (
	configFile = flag.String("config", "conf/app.yaml", "config file")
)

func init() {
	// 版本信息
	fmt.Printf("Version: %s\n", Version)
	fmt.Printf("Git Commit: %s\n", GitCommit)
}

func main() {
	flag.Parse()
	cfg, err := loadConfig(*configFile)
	if err != nil {
		panic(err)
	}
	commands.Serve(cfg, Version, GitCommit)
}

// 加载配置
func loadConfig(path string) (*config.Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadConfigFromBytes(raw)
}

// 配置中的环境变量替换功能
func loadConfigFromBytes(raw []byte) (*config.Config, error) {
	expanded := os.ExpandEnv(string(raw))
	cfg := &config.Config{}
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

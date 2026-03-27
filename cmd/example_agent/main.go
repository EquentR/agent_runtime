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
	Version   = "0.0.9-dev"
	GitCommit = "none"
)

var (
	configFile = flag.String("config", "conf/app.yaml", "config file")
)

// init 打印当前构建版本信息，便于启动时快速确认二进制来源。
func init() {
	// 版本信息
	fmt.Printf("Version: %s\n", Version)
	fmt.Printf("Git Commit: %s\n", GitCommit)
}

// main 是示例应用入口。
//
// @title Agent Runtime API
// @version 0.0.1
// @description Agent Runtime 示例应用 API 文档。
// @BasePath /api/v1
func main() {
	flag.Parse()
	cfg, err := loadConfig(*configFile)
	if err != nil {
		panic(err)
	}
	commands.Serve(cfg, Version, GitCommit)
}

// loadConfig 从配置文件读取并解析应用配置。
func loadConfig(path string) (*config.Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadConfigFromBytes(raw)
}

// loadConfigFromBytes 在反序列化前先展开环境变量。
func loadConfigFromBytes(raw []byte) (*config.Config, error) {
	expanded := os.ExpandEnv(string(raw))
	cfg := &config.Config{}
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

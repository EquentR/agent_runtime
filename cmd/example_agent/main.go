package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/commands"
	"github.com/EquentR/agent_runtime/app/config"
	"gopkg.in/yaml.v3"
)

var (
	Version   = "0.1.1-dev"
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
	go openConfiguredBrowserWhenReady(cfg, waitForServerReady, openBrowser)
	commands.Serve(cfg, Version, GitCommit)
}

func openConfiguredBrowserWhenReady(cfg *config.Config, waiter func(string) bool, opener func(string) error) {
	url := buildBrowserLaunchURL(cfg)
	if url == "" {
		return
	}
	if waiter != nil && !waiter(url) {
		return
	}
	openConfiguredBrowserBestEffort(cfg, opener)
}

func openConfiguredBrowserBestEffort(cfg *config.Config, opener func(string) error) {
	if opener == nil {
		return
	}
	url := buildBrowserLaunchURL(cfg)
	if url == "" {
		return
	}
	_ = opener(url)
}

func buildBrowserLaunchURL(cfg *config.Config) string {
	if cfg == nil || cfg.Server.Port <= 0 {
		return ""
	}
	host := normalizeBrowserHost(cfg.Server.Host)
	path := "/"
	if len(cfg.Server.StaticPaths) > 0 {
		path = normalizeBrowserPath(cfg.Server.StaticPaths[0].Path)
	}
	return fmt.Sprintf("http://%s:%d%s", host, cfg.Server.Port, path)
}

func normalizeBrowserHost(host string) string {
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "::":
		return "localhost"
	default:
		return host
	}
}

func normalizeBrowserPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func waitForServerReady(url string) bool {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
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

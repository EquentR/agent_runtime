package log

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNewLoggerCreatesMissingLogParentDirectory(t *testing.T) {
	if os.Getenv("ICE_ART_LOGGER_HELPER") == "1" {
		logger := NewLogger(&Config{Level: "info", File: os.Getenv("ICE_ART_LOGGER_PATH")})
		logger.Info("startup smoke")
		_ = logger.Sync()
		return
	}
	logPath := filepath.Join(t.TempDir(), "nested", "logs", "app.log")
	command := exec.Command(os.Args[0], "-test.run=^TestNewLoggerCreatesMissingLogParentDirectory$")
	command.Env = append(os.Environ(), "ICE_ART_LOGGER_HELPER=1", "ICE_ART_LOGGER_PATH="+logPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("logger helper failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file was not created: %v", err)
	}
}

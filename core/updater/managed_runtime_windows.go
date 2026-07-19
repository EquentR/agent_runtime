//go:build windows

package updater

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
)

func NewPlatformRuntime(mode, serviceName, healthURL string, client *http.Client) HelperRuntime {
	direct := NewDirectRuntime(healthURL, client)
	if mode == "windows-service" && serviceName != "" {
		return &windowsServiceRuntime{direct: direct, serviceName: serviceName}
	}
	return direct
}

type windowsServiceRuntime struct {
	direct      *DirectRuntime
	serviceName string
}

func (r *windowsServiceRuntime) WaitForExit(ctx context.Context, pid int, executable string) error {
	return r.direct.WaitForExit(ctx, pid, executable)
}

func (r *windowsServiceRuntime) Start(ctx context.Context, _ string, _ []string, _ string, _ []string) (int, error) {
	if output, err := exec.CommandContext(ctx, "sc.exe", "start", r.serviceName).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("start Windows service %s: %w: %s", r.serviceName, err, strings.TrimSpace(string(output)))
	}
	return 1, nil
}

func (r *windowsServiceRuntime) Stop(ctx context.Context, _ int, _ string) error {
	if output, err := exec.CommandContext(ctx, "sc.exe", "stop", r.serviceName).CombinedOutput(); err != nil {
		return fmt.Errorf("stop Windows service %s: %w: %s", r.serviceName, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (r *windowsServiceRuntime) Health(ctx context.Context, pid int, version, token string) error {
	return r.direct.Health(ctx, pid, version, token)
}

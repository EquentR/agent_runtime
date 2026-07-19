//go:build !windows

package updater

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
)

func NewPlatformRuntime(mode, serviceName, healthURL string, client *http.Client) HelperRuntime {
	direct := NewDirectRuntime(healthURL, client)
	if mode == "systemd" && serviceName != "" {
		return &systemdRuntime{direct: direct, serviceName: serviceName}
	}
	return direct
}

type systemdRuntime struct {
	direct      *DirectRuntime
	serviceName string
}

func (r *systemdRuntime) WaitForExit(ctx context.Context, pid int, executable string) error {
	return r.direct.WaitForExit(ctx, pid, executable)
}

func (r *systemdRuntime) Start(ctx context.Context, _ string, _ []string, _ string, _ []string) (int, error) {
	if err := exec.CommandContext(ctx, "systemctl", "start", r.serviceName).Run(); err != nil {
		return 0, fmt.Errorf("start systemd service %s: %w", r.serviceName, err)
	}
	return 1, nil
}

func (r *systemdRuntime) Stop(ctx context.Context, _ int, _ string) error {
	if err := exec.CommandContext(ctx, "systemctl", "stop", r.serviceName).Run(); err != nil {
		return fmt.Errorf("stop systemd service %s: %w", r.serviceName, err)
	}
	return nil
}

func (r *systemdRuntime) Health(ctx context.Context, pid int, version, token string) error {
	return r.direct.Health(ctx, pid, version, token)
}

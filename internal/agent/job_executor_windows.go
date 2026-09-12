//go:build windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/gokayybaz/bazusop/internal/jobs"
)

func executePlatformJob(ctx context.Context, job jobs.Job) (string, error) {
	switch job.Action {
	case jobs.ActionServiceRestart:
		return restartWindowsService(ctx, job.Target)
	case jobs.ActionHostReboot:
		output, err := exec.CommandContext(ctx, "shutdown.exe", "/r", "/t", "60", "/d", "p:4:1", "/c", "bazUSOP approved reboot").CombinedOutput()
		if err != nil {
			return string(output), fmt.Errorf("schedule host reboot: %w", err)
		}
		return "host reboot scheduled", nil
	default:
		return "", errors.New("unsupported platform job")
	}
}

func restartWindowsService(ctx context.Context, name string) (string, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("connect to Service Manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(name)
	if err != nil {
		return "", fmt.Errorf("open service %s: %w", name, err)
	}
	defer service.Close()
	status, err := service.Query()
	if err != nil {
		return "", fmt.Errorf("query service %s: %w", name, err)
	}
	if status.State != svc.Stopped {
		if _, err := service.Control(svc.Stop); err != nil {
			return "", fmt.Errorf("stop service %s: %w", name, err)
		}
		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for status.State != svc.Stopped {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-deadline.C:
				return "", fmt.Errorf("stop service %s: timeout", name)
			case <-ticker.C:
				status, err = service.Query()
				if err != nil {
					return "", fmt.Errorf("query service %s: %w", name, err)
				}
			}
		}
	}
	if err := service.Start(); err != nil {
		return "", fmt.Errorf("start service %s: %w", name, err)
	}
	return name + " restarted", nil
}

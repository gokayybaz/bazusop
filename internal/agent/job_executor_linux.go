//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/gokayybaz/bazusop/internal/jobs"
)

func executePlatformJob(parent context.Context, job jobs.Job) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	var command *exec.Cmd
	switch job.Action {
	case jobs.ActionServiceRestart:
		command = exec.CommandContext(ctx, "systemctl", "restart", "--", job.Target)
	case jobs.ActionHostReboot:
		command = exec.CommandContext(ctx, "shutdown", "-r", "+1", "bazUSOP approved reboot")
	default:
		return "", errors.New("unsupported platform job")
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("execute %s: %w", job.Action, err)
	}
	if len(output) > 0 {
		return string(output), nil
	}
	if job.Action == jobs.ActionServiceRestart {
		return job.Target + " restarted", nil
	}
	return "host reboot scheduled", nil
}

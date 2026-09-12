//go:build !windows

package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/gokayybaz/bazusop/internal/agent"
)

func runPlatform(runner agent.Runner) error {
	shutdown, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return runner.Run(shutdown)
}

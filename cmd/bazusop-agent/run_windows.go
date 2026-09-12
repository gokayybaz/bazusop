//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/gokayybaz/bazusop/internal/agent"
	"golang.org/x/sys/windows/svc"
)

const windowsServiceName = "bazusop-agent"

func runPlatform(runner agent.Runner) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("detect Windows service session: %w", err)
	}
	if isService {
		return svc.Run(windowsServiceName, serviceHandler{runner: runner})
	}
	shutdown, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return runner.Run(shutdown)
}

type serviceHandler struct {
	runner agent.Runner
}

func (handler serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- handler.runner.Run(ctx)
	}()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case err := <-done:
			if err != nil {
				return true, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				status <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-done; err != nil {
					return true, 1
				}
				return false, 0
			}
		}
	}
}

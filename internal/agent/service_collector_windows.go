//go:build windows

package agent

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/gokayybaz/bazusop/internal/serviceinventory"
)

func collectPlatformServices(context.Context) ([]serviceinventory.Fact, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("connect to Windows Service Manager: %w", err)
	}
	defer manager.Disconnect()
	names, err := manager.ListServices()
	if err != nil {
		return nil, fmt.Errorf("list Windows services: %w", err)
	}
	if len(names) > 5000 {
		return nil, fmt.Errorf("service snapshot exceeds 5000 entries")
	}
	services := make([]serviceinventory.Fact, 0, len(names))
	for _, name := range names {
		fact := serviceinventory.Fact{Name: name, State: "unknown", StartupType: "unknown"}
		service, openErr := manager.OpenService(name)
		if openErr != nil {
			services = append(services, fact)
			continue
		}
		status, statusErr := service.Query()
		if statusErr == nil {
			fact.State = windowsServiceState(status)
		}
		configuration, configErr := service.Config()
		if configErr == nil {
			fact.DisplayName = configuration.DisplayName
			fact.StartupType = windowsStartupType(configuration.StartType)
		}
		service.Close()
		services = append(services, fact)
	}
	return services, nil
}

func windowsServiceState(status svc.Status) string {
	if status.State == svc.Running {
		return "running"
	}
	if status.State == svc.Stopped {
		if status.Win32ExitCode != 0 || status.ServiceSpecificExitCode != 0 {
			return "failed"
		}
		return "stopped"
	}
	return "unknown"
}

func windowsStartupType(startType uint32) string {
	switch startType {
	case mgr.StartAutomatic:
		return "automatic"
	case mgr.StartManual:
		return "manual"
	case mgr.StartDisabled:
		return "disabled"
	default:
		return "unknown"
	}
}

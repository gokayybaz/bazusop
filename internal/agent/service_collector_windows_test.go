//go:build windows

package agent

import (
	"testing"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func TestWindowsServiceNormalization(t *testing.T) {
	if state := windowsServiceState(svc.Status{State: svc.Running}); state != "running" {
		t.Fatalf("unexpected running state: %s", state)
	}
	if state := windowsServiceState(svc.Status{State: svc.Stopped, Win32ExitCode: 1}); state != "failed" {
		t.Fatalf("unexpected failed state: %s", state)
	}
	if startup := windowsStartupType(mgr.StartAutomatic); startup != "automatic" {
		t.Fatalf("unexpected automatic startup: %s", startup)
	}
	if startup := windowsStartupType(mgr.StartDisabled); startup != "disabled" {
		t.Fatalf("unexpected disabled startup: %s", startup)
	}
}

package inventory_test

import (
	"context"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
)

func TestAgentFactsAreNormalizedAndUpserted(t *testing.T) {
	t.Parallel()

	store := inventory.NewMemoryStore()
	now := time.Date(2026, time.September, 10, 20, 0, 0, 0, time.UTC)
	service := inventory.NewService(store, inventory.WithClock(func() time.Time { return now }))

	err := service.Report(context.Background(), "agent-01", inventory.Facts{
		Hostname:      "EDGE-01.EXAMPLE.COM",
		OSFamily:      "LINUX",
		OSName:        "Ubuntu",
		OSVersion:     "24.04",
		Architecture:  "x86_64",
		KernelVersion: "6.8.0",
		CPUCores:      8,
		MemoryBytes:   16 * 1024 * 1024 * 1024,
		IPAddresses:   []string{"10.0.0.8", "10.0.0.8", "not-an-ip"},
		AgentVersion:  "0.2.0",
	})
	if err != nil {
		t.Fatalf("report host facts: %v", err)
	}

	now = now.Add(time.Minute)
	err = service.Report(context.Background(), "agent-01", inventory.Facts{
		Hostname:      "edge-01.example.com",
		OSFamily:      "linux",
		OSName:        "Ubuntu",
		OSVersion:     "24.04.1",
		Architecture:  "amd64",
		KernelVersion: "6.8.1",
		CPUCores:      8,
		MemoryBytes:   16 * 1024 * 1024 * 1024,
		IPAddresses:   []string{"10.0.0.8", "2001:db8::8"},
		AgentVersion:  "0.2.1",
	})
	if err != nil {
		t.Fatalf("update host facts: %v", err)
	}

	hosts, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list hosts: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected one upserted host, got %d", len(hosts))
	}
	host := hosts[0]
	if host.Hostname != "edge-01.example.com" {
		t.Errorf("expected normalized hostname, got %q", host.Hostname)
	}
	if host.Architecture != "amd64" {
		t.Errorf("expected normalized architecture, got %q", host.Architecture)
	}
	if host.OSVersion != "24.04.1" || host.AgentVersion != "0.2.1" {
		t.Errorf("expected updated facts, got %#v", host)
	}
	if len(host.IPAddresses) != 2 || host.IPAddresses[1] != "2001:db8::8" {
		t.Errorf("expected normalized addresses, got %#v", host.IPAddresses)
	}
	if host.Status != inventory.StatusConnected {
		t.Errorf("expected connected status, got %q", host.Status)
	}
}

func TestInvalidHostFactsAreRejected(t *testing.T) {
	t.Parallel()

	service := inventory.NewService(inventory.NewMemoryStore())
	err := service.Report(context.Background(), "agent-01", inventory.Facts{
		Hostname:     "edge-01",
		OSFamily:     "plan9",
		Architecture: "amd64",
		CPUCores:     8,
		MemoryBytes:  1024,
	})
	if err == nil {
		t.Fatal("expected unsupported operating system to be rejected")
	}
}

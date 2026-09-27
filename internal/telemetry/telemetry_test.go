package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"

	"github.com/gokayybaz/bazusop/internal/telemetry"
)

func TestSamplesAreValidatedAndReturnedChronologically(t *testing.T) {
	t.Parallel()

	store := telemetry.NewMemoryStore()
	service := telemetry.NewService(store)
	start := time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC)

	for index, cpu := range []float64{42.5, 51.2, 47.8} {
		err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, telemetry.Sample{
			RecordedAt:     start.Add(time.Duration(index) * time.Minute),
			CPUPercent:     cpu,
			MemoryPercent:  63.4,
			DiskPercent:    71.1,
			NetworkRXBytes: uint64(1000 + index*100),
			NetworkTXBytes: uint64(500 + index*50),
		})
		if err != nil {
			t.Fatalf("report sample %d: %v", index, err)
		}
	}

	samples, err := service.History(context.Background(), tenancy.DefaultScope(), "agent-01", start.Add(30*time.Second), start.Add(3*time.Minute), 2)
	if err != nil {
		t.Fatalf("query history: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("expected two bounded samples, got %d", len(samples))
	}
	if samples[0].CPUPercent != 51.2 || samples[1].CPUPercent != 47.8 {
		t.Fatalf("expected chronological samples in range, got %#v", samples)
	}
}

func TestInvalidSampleIsRejected(t *testing.T) {
	t.Parallel()

	service := telemetry.NewService(telemetry.NewMemoryStore())
	err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, telemetry.Sample{
		RecordedAt:    time.Now(),
		CPUPercent:    101,
		MemoryPercent: 50,
		DiskPercent:   50,
	})
	if err == nil {
		t.Fatal("expected out-of-range CPU value to be rejected")
	}
}

func TestFleetAverageOnlyCountsAgentsWithARecentSample(t *testing.T) {
	t.Parallel()

	store := telemetry.NewMemoryStore()
	service := telemetry.NewService(store)
	scope := tenancy.DefaultScope()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)

	if err := service.Report(context.Background(), tenancy.Agent{ID: "agent-fresh", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, telemetry.Sample{RecordedAt: now, CPUPercent: 60, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatal(err)
	}
	if err := service.Report(context.Background(), tenancy.Agent{ID: "agent-stale", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, telemetry.Sample{RecordedAt: now.Add(-10 * time.Minute), CPUPercent: 100, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatal(err)
	}
	if err := service.Report(context.Background(), tenancy.Agent{ID: "agent-other-site", OrganizationID: scope.OrganizationID, SiteID: "some-other-site"}, telemetry.Sample{RecordedAt: now, CPUPercent: 100, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatal(err)
	}

	average, err := service.FleetAverage(context.Background(), scope, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("fleet average: %v", err)
	}
	if average.DeviceCount != 1 || average.AverageCPUPercent != 60 {
		t.Fatalf("expected only the fresh, same-site agent counted (60%%, 1 device), got %#v", average)
	}
}

func TestFleetAverageReturnsZeroDevicesWhenFleetIsEmpty(t *testing.T) {
	t.Parallel()

	service := telemetry.NewService(telemetry.NewMemoryStore())
	average, err := service.FleetAverage(context.Background(), tenancy.DefaultScope(), time.Now().Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("fleet average: %v", err)
	}
	if average.DeviceCount != 0 || average.AverageCPUPercent != 0 {
		t.Fatalf("expected a zero-value FleetAverage for an empty fleet, got %#v", average)
	}
}

func TestFleetAverageAveragesAcrossMultipleRecentAgents(t *testing.T) {
	t.Parallel()

	service := telemetry.NewService(telemetry.NewMemoryStore())
	scope := tenancy.DefaultScope()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)

	for _, entry := range []struct {
		agent string
		cpu   float64
	}{{"agent-1", 40}, {"agent-2", 60}} {
		if err := service.Report(context.Background(), tenancy.Agent{ID: entry.agent, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, telemetry.Sample{RecordedAt: now, CPUPercent: entry.cpu, MemoryPercent: 50, DiskPercent: 50}); err != nil {
			t.Fatal(err)
		}
	}

	average, err := service.FleetAverage(context.Background(), scope, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("fleet average: %v", err)
	}
	if average.DeviceCount != 2 || average.AverageCPUPercent != 50 {
		t.Fatalf("expected (40+60)/2 = 50%% across 2 devices, got %#v", average)
	}
}

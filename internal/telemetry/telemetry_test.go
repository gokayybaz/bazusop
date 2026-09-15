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

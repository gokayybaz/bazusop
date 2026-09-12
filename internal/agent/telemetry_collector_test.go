package agent

import (
	"testing"
	"time"
)

func TestTelemetryCollectorCalculatesUtilizationAndCounterDeltas(t *testing.T) {
	snapshots := []systemSnapshot{
		{CPUTotal: 100, CPUIdle: 70, MemoryTotal: 1000, MemoryAvailable: 400, DiskTotal: 2000, DiskFree: 500, NetworkRXBytes: 100, NetworkTXBytes: 50},
		{CPUTotal: 120, CPUIdle: 75, MemoryTotal: 1000, MemoryAvailable: 250, DiskTotal: 2000, DiskFree: 400, NetworkRXBytes: 140, NetworkTXBytes: 65},
	}
	index := 0
	recordedAt := time.Date(2026, time.September, 12, 13, 0, 0, 0, time.FixedZone("TRT", 3*60*60))
	collector := &TelemetryCollector{
		snapshot: func() (systemSnapshot, error) {
			value := snapshots[index]
			index++
			return value, nil
		},
		now: func() time.Time { return recordedAt },
	}

	first, err := collector.Collect()
	if err != nil {
		t.Fatalf("collect first telemetry: %v", err)
	}
	if first.CPUPercent != 30 || first.MemoryPercent != 60 || first.DiskPercent != 75 {
		t.Fatalf("unexpected first utilization: %#v", first)
	}
	second, err := collector.Collect()
	if err != nil {
		t.Fatalf("collect second telemetry: %v", err)
	}
	if second.CPUPercent != 75 || second.MemoryPercent != 75 || second.DiskPercent != 80 {
		t.Fatalf("unexpected delta utilization: %#v", second)
	}
	if second.NetworkRXBytes != 140 || second.NetworkTXBytes != 65 || second.RecordedAt.Location() != time.UTC {
		t.Fatalf("unexpected counters or timestamp: %#v", second)
	}
}

func TestTelemetryCollectorRejectsInvalidSystemSnapshot(t *testing.T) {
	collector := &TelemetryCollector{
		snapshot: func() (systemSnapshot, error) {
			return systemSnapshot{CPUTotal: 1, CPUIdle: 2}, nil
		},
		now: time.Now,
	}
	if _, err := collector.Collect(); err == nil {
		t.Fatal("expected invalid system snapshot to fail")
	}
}

func TestTelemetryCollectorReadsCurrentHost(t *testing.T) {
	sample, err := NewTelemetryCollector().Collect()
	if err != nil {
		t.Fatalf("collect host telemetry: %v", err)
	}
	if sample.RecordedAt.IsZero() || sample.CPUPercent < 0 || sample.CPUPercent > 100 || sample.MemoryPercent < 0 || sample.MemoryPercent > 100 || sample.DiskPercent < 0 || sample.DiskPercent > 100 {
		t.Fatalf("invalid host telemetry: %#v", sample)
	}
}

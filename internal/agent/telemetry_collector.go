package agent

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/gokayybaz/bazusop/internal/telemetry"
)

type systemSnapshot struct {
	CPUTotal        float64
	CPUIdle         float64
	MemoryTotal     uint64
	MemoryAvailable uint64
	DiskTotal       uint64
	DiskFree        uint64
	NetworkRXBytes  uint64
	NetworkTXBytes  uint64
}

type TelemetryCollector struct {
	mu       sync.Mutex
	snapshot func() (systemSnapshot, error)
	now      func() time.Time
	previous *systemSnapshot
}

func NewTelemetryCollector() *TelemetryCollector {
	return &TelemetryCollector{snapshot: collectSystemSnapshot, now: time.Now}
}

func (collector *TelemetryCollector) Collect() (telemetry.Sample, error) {
	if collector == nil || collector.snapshot == nil || collector.now == nil {
		return telemetry.Sample{}, errors.New("telemetry collector is incomplete")
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()

	current, err := collector.snapshot()
	if err != nil {
		return telemetry.Sample{}, err
	}
	if err := current.validate(); err != nil {
		return telemetry.Sample{}, err
	}
	cpuTotal, cpuIdle := current.CPUTotal, current.CPUIdle
	if collector.previous != nil && current.CPUTotal > collector.previous.CPUTotal && current.CPUIdle >= collector.previous.CPUIdle {
		cpuTotal -= collector.previous.CPUTotal
		cpuIdle -= collector.previous.CPUIdle
	}
	collector.previous = &current
	return telemetry.Sample{
		RecordedAt:     collector.now().UTC(),
		CPUPercent:     percentUsed(cpuTotal, cpuIdle),
		MemoryPercent:  percentUsed(float64(current.MemoryTotal), float64(current.MemoryAvailable)),
		DiskPercent:    percentUsed(float64(current.DiskTotal), float64(current.DiskFree)),
		NetworkRXBytes: current.NetworkRXBytes,
		NetworkTXBytes: current.NetworkTXBytes,
	}, nil
}

func (snapshot systemSnapshot) validate() error {
	if snapshot.CPUTotal <= 0 || math.IsNaN(snapshot.CPUTotal) || math.IsInf(snapshot.CPUTotal, 0) || snapshot.CPUIdle < 0 || snapshot.CPUIdle > snapshot.CPUTotal || snapshot.MemoryTotal == 0 || snapshot.MemoryAvailable > snapshot.MemoryTotal || snapshot.DiskTotal == 0 || snapshot.DiskFree > snapshot.DiskTotal || snapshot.NetworkRXBytes > math.MaxInt64 || snapshot.NetworkTXBytes > math.MaxInt64 {
		return errors.New("invalid system telemetry snapshot")
	}
	return nil
}

func percentUsed(total, available float64) float64 {
	value := 100 * (total - available) / total
	return math.Max(0, math.Min(100, value))
}

func collectSystemSnapshot() (systemSnapshot, error) {
	cpuTimes, err := cpu.Times(false)
	if err != nil {
		return systemSnapshot{}, fmt.Errorf("read CPU counters: %w", err)
	}
	if len(cpuTimes) == 0 {
		return systemSnapshot{}, errors.New("read CPU counters: no aggregate sample")
	}
	memory, err := mem.VirtualMemory()
	if err != nil {
		return systemSnapshot{}, fmt.Errorf("read memory counters: %w", err)
	}
	volume, err := disk.Usage(rootDiskPath())
	if err != nil {
		return systemSnapshot{}, fmt.Errorf("read root disk counters: %w", err)
	}
	receiveBytes, transmitBytes, err := networkCounters()
	if err != nil {
		return systemSnapshot{}, fmt.Errorf("read network counters: %w", err)
	}
	times := cpuTimes[0]
	return systemSnapshot{
		CPUTotal: times.Total(), CPUIdle: times.Idle + times.Iowait,
		MemoryTotal: memory.Total, MemoryAvailable: memory.Available,
		DiskTotal: volume.Total, DiskFree: volume.Free,
		NetworkRXBytes: receiveBytes, NetworkTXBytes: transmitBytes,
	}, nil
}

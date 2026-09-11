package telemetry

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrInvalidSample = errors.New("invalid telemetry sample")

type Sample struct {
	AgentID        string    `json:"agent_id,omitempty"`
	RecordedAt     time.Time `json:"recorded_at"`
	CPUPercent     float64   `json:"cpu_percent"`
	MemoryPercent  float64   `json:"memory_percent"`
	DiskPercent    float64   `json:"disk_percent"`
	NetworkRXBytes uint64    `json:"network_rx_bytes"`
	NetworkTXBytes uint64    `json:"network_tx_bytes"`
}

type Store interface {
	Append(context.Context, Sample) error
	History(context.Context, string, time.Time, time.Time, int) ([]Sample, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (service *Service) Report(ctx context.Context, agentID string, sample Sample) error {
	sample.AgentID = strings.TrimSpace(agentID)
	sample.RecordedAt = sample.RecordedAt.UTC().Truncate(time.Microsecond)
	if sample.AgentID == "" || sample.RecordedAt.IsZero() || !validPercent(sample.CPUPercent) || !validPercent(sample.MemoryPercent) || !validPercent(sample.DiskPercent) || sample.NetworkRXBytes > math.MaxInt64 || sample.NetworkTXBytes > math.MaxInt64 {
		return ErrInvalidSample
	}
	return service.store.Append(ctx, sample)
}

func (service *Service) History(ctx context.Context, agentID string, from, to time.Time, limit int) ([]Sample, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || from.IsZero() || to.IsZero() || !from.Before(to) || limit < 1 || limit > 2000 {
		return nil, ErrInvalidSample
	}
	return service.store.History(ctx, agentID, from.UTC(), to.UTC(), limit)
}

func validPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

type MemoryStore struct {
	mu      sync.RWMutex
	samples map[string][]Sample
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{samples: make(map[string][]Sample)}
}

func (store *MemoryStore) Append(_ context.Context, sample Sample) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	values := store.samples[sample.AgentID]
	replaced := false
	for index := range values {
		if values[index].RecordedAt.Equal(sample.RecordedAt) {
			values[index] = sample
			replaced = true
			break
		}
	}
	if !replaced {
		values = append(values, sample)
	}
	sort.Slice(values, func(left, right int) bool {
		return values[left].RecordedAt.Before(values[right].RecordedAt)
	})
	store.samples[sample.AgentID] = values
	return nil
}

func (store *MemoryStore) History(_ context.Context, agentID string, from, to time.Time, limit int) ([]Sample, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	values := store.samples[agentID]
	result := make([]Sample, 0, len(values))
	for _, sample := range values {
		if sample.RecordedAt.Before(from) || sample.RecordedAt.After(to) {
			continue
		}
		result = append(result, sample)
	}
	if len(result) > limit {
		result = result[len(result)-limit:]
	}
	return append([]Sample(nil), result...), nil
}

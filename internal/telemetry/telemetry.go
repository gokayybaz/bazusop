package telemetry

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

var ErrInvalidSample = errors.New("invalid telemetry sample")

type Sample struct {
	OrganizationID string    `json:"organization_id"`
	SiteID         string    `json:"site_id"`
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
	History(context.Context, tenancy.Scope, string, time.Time, time.Time, int) ([]Sample, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (service *Service) Report(ctx context.Context, agent tenancy.Agent, sample Sample) error {
	if err := agent.Validate(); err != nil {
		return err
	}
	sample.AgentID = strings.TrimSpace(agent.ID)
	sample.OrganizationID, sample.SiteID = agent.OrganizationID, agent.SiteID
	sample.RecordedAt = sample.RecordedAt.UTC().Truncate(time.Microsecond)
	if sample.AgentID == "" || sample.RecordedAt.IsZero() || !validPercent(sample.CPUPercent) || !validPercent(sample.MemoryPercent) || !validPercent(sample.DiskPercent) || sample.NetworkRXBytes > math.MaxInt64 || sample.NetworkTXBytes > math.MaxInt64 {
		return ErrInvalidSample
	}
	return service.store.Append(ctx, sample)
}

func (service *Service) History(ctx context.Context, scope tenancy.Scope, agentID string, from, to time.Time, limit int) ([]Sample, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || from.IsZero() || to.IsZero() || !from.Before(to) || limit < 1 || limit > 2000 {
		return nil, ErrInvalidSample
	}
	return service.store.History(ctx, scope, agentID, from.UTC(), to.UTC(), limit)
}

func validPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

type MemoryStore struct {
	mu      sync.RWMutex
	samples map[tenancy.Agent][]Sample
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{samples: make(map[tenancy.Agent][]Sample)}
}

func (store *MemoryStore) Append(_ context.Context, sample Sample) error {
	key := tenancy.Agent{ID: sample.AgentID, OrganizationID: sample.OrganizationID, SiteID: sample.SiteID}
	if err := key.Validate(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	values := store.samples[key]
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
	store.samples[key] = values
	return nil
}

func (store *MemoryStore) History(_ context.Context, scope tenancy.Scope, agentID string, from, to time.Time, limit int) ([]Sample, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()

	values := store.samples[tenancy.Agent{ID: agentID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}]
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

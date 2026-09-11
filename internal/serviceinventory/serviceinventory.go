package serviceinventory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrInvalidSnapshot = errors.New("invalid service inventory snapshot")

type State string

const (
	StateRunning State = "running"
	StateStopped State = "stopped"
	StateFailed  State = "failed"
	StateUnknown State = "unknown"
)

type StartupType string

const (
	StartupAutomatic StartupType = "automatic"
	StartupManual    StartupType = "manual"
	StartupDisabled  StartupType = "disabled"
	StartupUnknown   StartupType = "unknown"
)

type Fact struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	State       string `json:"state"`
	StartupType string `json:"startup_type"`
}

type Snapshot struct {
	ObservedAt time.Time `json:"observed_at"`
	Services   []Fact    `json:"services"`
}

type Service struct {
	AgentID     string      `json:"agent_id"`
	Name        string      `json:"name"`
	DisplayName string      `json:"display_name"`
	State       State       `json:"state"`
	StartupType StartupType `json:"startup_type"`
	ObservedAt  time.Time   `json:"observed_at"`
}

type Filter struct {
	State State
	Query string
}

type Store interface {
	ReplaceServices(context.Context, string, []Service) error
	ListServices(context.Context, string, Filter) ([]Service, error)
}

type Manager struct {
	store Store
}

func NewService(store Store) *Manager {
	return &Manager{store: store}
}

func (manager *Manager) Report(ctx context.Context, agentID string, snapshot Snapshot) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || snapshot.ObservedAt.IsZero() || len(snapshot.Services) > 5000 {
		return ErrInvalidSnapshot
	}
	observedAt := snapshot.ObservedAt.UTC()
	services := make([]Service, 0, len(snapshot.Services))
	seen := make(map[string]struct{}, len(snapshot.Services))
	for _, fact := range snapshot.Services {
		name := strings.ToLower(strings.TrimSpace(fact.Name))
		state, validState := normalizeState(fact.State)
		startupType, validStartup := normalizeStartupType(fact.StartupType)
		if name == "" || len(name) > 256 || !validState || !validStartup {
			return ErrInvalidSnapshot
		}
		if _, exists := seen[name]; exists {
			return ErrInvalidSnapshot
		}
		seen[name] = struct{}{}
		services = append(services, Service{
			AgentID:     agentID,
			Name:        name,
			DisplayName: bounded(strings.TrimSpace(fact.DisplayName), 256),
			State:       state,
			StartupType: startupType,
			ObservedAt:  observedAt,
		})
	}
	return manager.store.ReplaceServices(ctx, agentID, services)
}

func (manager *Manager) List(ctx context.Context, agentID string, filter Filter) ([]Service, error) {
	agentID = strings.TrimSpace(agentID)
	filter.Query = strings.TrimSpace(filter.Query)
	if agentID == "" || len(filter.Query) > 256 || (filter.State != "" && !validNormalizedState(filter.State)) {
		return nil, ErrInvalidSnapshot
	}
	return manager.store.ListServices(ctx, agentID, filter)
}

func normalizeState(value string) (State, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "running", "active", "started":
		return StateRunning, true
	case "stopped", "inactive":
		return StateStopped, true
	case "failed":
		return StateFailed, true
	case "", "unknown":
		return StateUnknown, true
	default:
		return "", false
	}
}

func normalizeStartupType(value string) (StartupType, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "automatic", "auto", "enabled":
		return StartupAutomatic, true
	case "manual", "static":
		return StartupManual, true
	case "disabled", "masked":
		return StartupDisabled, true
	case "", "unknown":
		return StartupUnknown, true
	default:
		return "", false
	}
}

func validNormalizedState(state State) bool {
	return state == StateRunning || state == StateStopped || state == StateFailed || state == StateUnknown
}

func bounded(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

type MemoryStore struct {
	mu       sync.RWMutex
	services map[string][]Service
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{services: make(map[string][]Service)}
}

func (store *MemoryStore) ReplaceServices(_ context.Context, agentID string, services []Service) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.services[agentID] = append([]Service(nil), services...)
	return nil
}

func (store *MemoryStore) ListServices(_ context.Context, agentID string, filter Filter) ([]Service, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	query := strings.ToLower(filter.Query)
	result := make([]Service, 0)
	for _, service := range store.services[agentID] {
		if filter.State != "" && service.State != filter.State {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(service.Name), query) && !strings.Contains(strings.ToLower(service.DisplayName), query) {
			continue
		}
		result = append(result, service)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

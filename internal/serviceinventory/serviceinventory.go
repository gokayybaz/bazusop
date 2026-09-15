package serviceinventory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
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
	OrganizationID string      `json:"organization_id"`
	SiteID         string      `json:"site_id"`
	AgentID        string      `json:"agent_id"`
	Name           string      `json:"name"`
	DisplayName    string      `json:"display_name"`
	State          State       `json:"state"`
	StartupType    StartupType `json:"startup_type"`
	ObservedAt     time.Time   `json:"observed_at"`
}

type Filter struct {
	State State
	Query string
}

type Store interface {
	ReplaceServices(context.Context, tenancy.Scope, string, []Service) error
	ListServices(context.Context, tenancy.Scope, string, Filter) ([]Service, error)
}

type Manager struct {
	store Store
}

func NewService(store Store) *Manager {
	return &Manager{store: store}
}

func (manager *Manager) Report(ctx context.Context, agent tenancy.Agent, snapshot Snapshot) error {
	if err := agent.Validate(); err != nil {
		return err
	}
	agentID := strings.TrimSpace(agent.ID)
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
			OrganizationID: agent.OrganizationID,
			SiteID:         agent.SiteID,
			AgentID:        agentID,
			Name:           name,
			DisplayName:    bounded(strings.TrimSpace(fact.DisplayName), 256),
			State:          state,
			StartupType:    startupType,
			ObservedAt:     observedAt,
		})
	}
	return manager.store.ReplaceServices(ctx, agent.Scope(), agentID, services)
}

func (manager *Manager) List(ctx context.Context, scope tenancy.Scope, agentID string, filter Filter) ([]Service, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	agentID = strings.TrimSpace(agentID)
	filter.Query = strings.TrimSpace(filter.Query)
	if agentID == "" || len(filter.Query) > 256 || (filter.State != "" && !validNormalizedState(filter.State)) {
		return nil, ErrInvalidSnapshot
	}
	return manager.store.ListServices(ctx, scope, agentID, filter)
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
	services map[tenancy.Agent][]Service
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{services: make(map[tenancy.Agent][]Service)}
}

func (store *MemoryStore) ReplaceServices(_ context.Context, scope tenancy.Scope, agentID string, services []Service) error {
	key := tenancy.Agent{ID: agentID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}
	if err := key.Validate(); err != nil {
		return err
	}
	for _, service := range services {
		if service.AgentID != agentID || service.OrganizationID != scope.OrganizationID || service.SiteID != scope.SiteID {
			return tenancy.ErrInvalidScope
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.services[key] = append([]Service(nil), services...)
	return nil
}

func (store *MemoryStore) ListServices(_ context.Context, scope tenancy.Scope, agentID string, filter Filter) ([]Service, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	query := strings.ToLower(filter.Query)
	result := make([]Service, 0)
	for _, service := range store.services[tenancy.Agent{ID: agentID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}] {
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

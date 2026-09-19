package alerting

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

var (
	ErrInvalidAlert = errors.New("invalid alert")
	ErrNotFound     = errors.New("alert not found")
	ErrConflict     = errors.New("alert conflict")
)

type Kind string
type Metric string
type Severity string
type IncidentStatus string
type EventType string

const (
	KindMetric         Kind           = "metric"
	KindReachability   Kind           = "reachability"
	MetricCPU          Metric         = "cpu"
	MetricMemory       Metric         = "memory"
	MetricDisk         Metric         = "disk"
	SeverityWarning    Severity       = "warning"
	SeverityCritical   Severity       = "critical"
	StatusOpen         IncidentStatus = "open"
	StatusAcknowledged IncidentStatus = "acknowledged"
	StatusResolved     IncidentStatus = "resolved"
	EventOpened        EventType      = "opened"
	EventAcknowledged  EventType      = "acknowledged"
	EventResolved      EventType      = "resolved"
)

type RuleRequest struct {
	Name              string   `json:"name"`
	Kind              Kind     `json:"kind"`
	Metric            Metric   `json:"metric"`
	Threshold         float64  `json:"threshold"`
	StaleAfterSeconds int      `json:"stale_after_seconds"`
	Severity          Severity `json:"severity"`
	Enabled           bool     `json:"enabled"`
}

type Rule struct {
	OrganizationID string `json:"organization_id"`
	SiteID         string `json:"site_id"`
	ID             string `json:"id"`
	RuleRequest
	CreatedAt time.Time `json:"created_at"`
}

type MaintenanceRequest struct {
	Name      string    `json:"name"`
	AgentID   string    `json:"agent_id"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	CreatedBy string    `json:"created_by"`
}

type MaintenanceWindow struct {
	OrganizationID string `json:"organization_id"`
	SiteID         string `json:"site_id"`
	ID             string `json:"id"`
	MaintenanceRequest
	CreatedAt time.Time `json:"created_at"`
}

type Incident struct {
	OrganizationID string         `json:"organization_id"`
	SiteID         string         `json:"site_id"`
	ID             string         `json:"id"`
	RuleID         string         `json:"rule_id"`
	RuleName       string         `json:"rule_name"`
	AgentID        string         `json:"agent_id"`
	Severity       Severity       `json:"severity"`
	Status         IncidentStatus `json:"status"`
	Message        string         `json:"message"`
	LatestValue    float64        `json:"latest_value"`
	OpenedAt       time.Time      `json:"opened_at"`
	AcknowledgedAt *time.Time     `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string         `json:"acknowledged_by,omitempty"`
	ResolvedAt     *time.Time     `json:"resolved_at,omitempty"`
}

type Event struct {
	OrganizationID string    `json:"organization_id"`
	SiteID         string    `json:"site_id"`
	ID             string    `json:"id"`
	IncidentID     string    `json:"incident_id"`
	Type           EventType `json:"type"`
	Actor          string    `json:"actor"`
	Message        string    `json:"message"`
	OccurredAt     time.Time `json:"occurred_at"`
}

type Telemetry struct{ CPUPercent, MemoryPercent, DiskPercent float64 }

type Store interface {
	CreateAlertRule(context.Context, tenancy.Scope, Rule) error
	ListAlertRules(context.Context, tenancy.Scope) ([]Rule, error)
	CreateMaintenanceWindow(context.Context, tenancy.Scope, MaintenanceWindow) error
	ListMaintenanceWindows(context.Context, tenancy.Scope) ([]MaintenanceWindow, error)
	EnsureIncident(context.Context, tenancy.Scope, Incident, Event) (Incident, bool, error)
	ResolveIncident(context.Context, tenancy.Scope, string, string, float64, string, Event) (*Incident, error)
	AcknowledgeIncident(context.Context, tenancy.Scope, string, string, time.Time, Event) (Incident, error)
	ListAlertIncidents(context.Context, tenancy.Scope, int) ([]Incident, error)
	ListAlertEvents(context.Context, tenancy.Scope, string) ([]Event, error)
}

type HostScopeChecker func(context.Context, tenancy.Scope, string) (bool, error)

type Service struct {
	store      Store
	now        func() time.Time
	hostExists HostScopeChecker
}
type Option func(*Service)

func WithClock(clock func() time.Time) Option { return func(service *Service) { service.now = clock } }
func WithHostScopeChecker(checker HostScopeChecker) Option {
	return func(service *Service) { service.hostExists = checker }
}
func NewService(store Store, options ...Option) (*Service, error) {
	service := &Service{store: store, now: time.Now}
	for _, option := range options {
		option(service)
	}
	if service.hostExists == nil {
		return nil, errors.New("alert host scope checker is required")
	}
	return service, nil
}

func (service *Service) CreateRule(ctx context.Context, scope tenancy.Scope, request RuleRequest) (Rule, error) {
	if err := scope.Validate(); err != nil {
		return Rule{}, err
	}
	request.Name = strings.TrimSpace(request.Name)
	if !validRule(request) {
		return Rule{}, ErrInvalidAlert
	}
	id, err := newID()
	if err != nil {
		return Rule{}, err
	}
	rule := Rule{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: id, RuleRequest: request, CreatedAt: service.now().UTC().Truncate(time.Microsecond)}
	if err := service.store.CreateAlertRule(ctx, scope, rule); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

func (service *Service) ListRules(ctx context.Context, scope tenancy.Scope) ([]Rule, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return service.store.ListAlertRules(ctx, scope)
}

func (service *Service) CreateMaintenance(ctx context.Context, scope tenancy.Scope, request MaintenanceRequest) (MaintenanceWindow, error) {
	if err := scope.Validate(); err != nil {
		return MaintenanceWindow{}, err
	}
	request.Name, request.AgentID, request.CreatedBy = strings.TrimSpace(request.Name), strings.TrimSpace(request.AgentID), strings.TrimSpace(request.CreatedBy)
	request.StartsAt, request.EndsAt = request.StartsAt.UTC().Truncate(time.Microsecond), request.EndsAt.UTC().Truncate(time.Microsecond)
	if request.Name == "" || request.CreatedBy == "" || request.StartsAt.IsZero() || !request.StartsAt.Before(request.EndsAt) || len(request.Name) > 128 || len(request.AgentID) > 128 {
		return MaintenanceWindow{}, ErrInvalidAlert
	}
	if request.AgentID != "" {
		hostExists, err := service.hostExists(ctx, scope, request.AgentID)
		if err != nil {
			return MaintenanceWindow{}, err
		}
		if !hostExists {
			return MaintenanceWindow{}, ErrNotFound
		}
	}
	id, err := newID()
	if err != nil {
		return MaintenanceWindow{}, err
	}
	window := MaintenanceWindow{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: id, MaintenanceRequest: request, CreatedAt: service.now().UTC().Truncate(time.Microsecond)}
	if err := service.store.CreateMaintenanceWindow(ctx, scope, window); err != nil {
		return MaintenanceWindow{}, err
	}
	return window, nil
}

func (service *Service) ListMaintenance(ctx context.Context, scope tenancy.Scope) ([]MaintenanceWindow, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return service.store.ListMaintenanceWindows(ctx, scope)
}

func (service *Service) EvaluateTelemetry(ctx context.Context, scope tenancy.Scope, agentID string, telemetry Telemetry) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	values := map[Metric]float64{MetricCPU: telemetry.CPUPercent, MetricMemory: telemetry.MemoryPercent, MetricDisk: telemetry.DiskPercent}
	rules, err := service.store.ListAlertRules(ctx, scope)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if !rule.Enabled || rule.Kind != KindMetric {
			continue
		}
		value := values[rule.Metric]
		if err := service.evaluate(ctx, scope, rule, strings.TrimSpace(agentID), value > rule.Threshold, value, fmt.Sprintf("%s %.1f%%; eşik %.1f%%", metricLabel(rule.Metric), value, rule.Threshold)); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) EvaluateReachability(ctx context.Context, scope tenancy.Scope, agentID string, lastSeen time.Time) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	rules, err := service.store.ListAlertRules(ctx, scope)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if !rule.Enabled || rule.Kind != KindReachability {
			continue
		}
		staleSeconds := service.now().UTC().Sub(lastSeen.UTC()).Seconds()
		if staleSeconds < 0 {
			staleSeconds = 0
		}
		if err := service.evaluate(ctx, scope, rule, strings.TrimSpace(agentID), staleSeconds > float64(rule.StaleAfterSeconds), staleSeconds, fmt.Sprintf("Agent %.0f saniyedir rapor vermiyor", staleSeconds)); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) evaluate(ctx context.Context, scope tenancy.Scope, rule Rule, agentID string, breached bool, value float64, message string) error {
	if agentID == "" {
		return ErrInvalidAlert
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	if breached {
		windows, err := service.store.ListMaintenanceWindows(ctx, scope)
		if err != nil {
			return err
		}
		for _, window := range windows {
			if (window.AgentID == "" || window.AgentID == agentID) && !now.Before(window.StartsAt) && now.Before(window.EndsAt) {
				return nil
			}
		}
		id, err := newID()
		if err != nil {
			return err
		}
		incident := Incident{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: id, RuleID: rule.ID, RuleName: rule.Name, AgentID: agentID, Severity: rule.Severity, Status: StatusOpen, Message: message, LatestValue: value, OpenedAt: now}
		event := Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: mustID(), IncidentID: id, Type: EventOpened, Actor: "hub", Message: message, OccurredAt: now}
		_, _, err = service.store.EnsureIncident(ctx, scope, incident, event)
		return err
	}
	_, err := service.store.ResolveIncident(ctx, scope, rule.ID, agentID, value, "Koşul normale döndü", Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: mustID(), Type: EventResolved, Actor: "hub", Message: "Koşul normale döndü", OccurredAt: now})
	return err
}

func (service *Service) Acknowledge(ctx context.Context, scope tenancy.Scope, incidentID, actor string) (Incident, error) {
	if err := scope.Validate(); err != nil {
		return Incident{}, err
	}
	incidentID, actor = strings.TrimSpace(incidentID), strings.TrimSpace(actor)
	if incidentID == "" || actor == "" || len(actor) > 128 {
		return Incident{}, ErrInvalidAlert
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	return service.store.AcknowledgeIncident(ctx, scope, incidentID, actor, now, Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: mustID(), IncidentID: incidentID, Type: EventAcknowledged, Actor: actor, Message: "Olay operatör tarafından onaylandı", OccurredAt: now})
}

func (service *Service) ListIncidents(ctx context.Context, scope tenancy.Scope, limit int) ([]Incident, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		return nil, ErrInvalidAlert
	}
	return service.store.ListAlertIncidents(ctx, scope, limit)
}
func (service *Service) ListEvents(ctx context.Context, scope tenancy.Scope, incidentID string) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(incidentID) == "" {
		return nil, ErrInvalidAlert
	}
	return service.store.ListAlertEvents(ctx, scope, incidentID)
}

func validRule(request RuleRequest) bool {
	if request.Name == "" || len(request.Name) > 128 || (request.Severity != SeverityWarning && request.Severity != SeverityCritical) {
		return false
	}
	if request.Kind == KindMetric {
		return (request.Metric == MetricCPU || request.Metric == MetricMemory || request.Metric == MetricDisk) && request.Threshold > 0 && request.Threshold <= 100 && request.StaleAfterSeconds == 0
	}
	return request.Kind == KindReachability && request.Metric == "" && request.Threshold == 0 && request.StaleAfterSeconds >= 60 && request.StaleAfterSeconds <= 86400
}
func metricLabel(metric Metric) string {
	return map[Metric]string{MetricCPU: "CPU", MetricMemory: "Bellek", MetricDisk: "Disk"}[metric]
}
func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
func mustID() string { id, _ := newID(); return id }

type MemoryStore struct {
	mu        sync.Mutex
	rules     map[string]Rule
	windows   map[string]MaintenanceWindow
	incidents map[string]Incident
	events    map[string][]Event
	hosts     map[string]map[string]struct{}
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rules: map[string]Rule{}, windows: map[string]MaintenanceWindow{}, incidents: map[string]Incident{}, events: map[string][]Event{}, hosts: map[string]map[string]struct{}{}}
}
func scopeKey(scope tenancy.Scope) string { return scope.OrganizationID + "\x00" + scope.SiteID }

// RegisterHost seeds the in-memory host directory used to enforce scoped maintenance targets.
func (store *MemoryStore) RegisterHost(scope tenancy.Scope, agentID string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := scopeKey(scope)
	if store.hosts[key] == nil {
		store.hosts[key] = map[string]struct{}{}
	}
	store.hosts[key][agentID] = struct{}{}
}

func (store *MemoryStore) HasHost(_ context.Context, scope tenancy.Scope, agentID string) (bool, error) {
	if err := scope.Validate(); err != nil {
		return false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	_, ok := store.hosts[scopeKey(scope)][agentID]
	return ok, nil
}

func (store *MemoryStore) CreateAlertRule(_ context.Context, scope tenancy.Scope, rule Rule) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if rule.OrganizationID != scope.OrganizationID || rule.SiteID != scope.SiteID {
		return tenancy.ErrInvalidScope
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.rules[rule.ID] = rule
	return nil
}
func (store *MemoryStore) ListAlertRules(_ context.Context, scope tenancy.Scope) ([]Rule, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]Rule, 0, len(store.rules))
	for _, v := range store.rules {
		if v.OrganizationID != scope.OrganizationID || v.SiteID != scope.SiteID {
			continue
		}
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}
func (store *MemoryStore) CreateMaintenanceWindow(_ context.Context, scope tenancy.Scope, window MaintenanceWindow) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if window.OrganizationID != scope.OrganizationID || window.SiteID != scope.SiteID {
		return tenancy.ErrInvalidScope
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.windows[window.ID] = window
	return nil
}
func (store *MemoryStore) ListMaintenanceWindows(_ context.Context, scope tenancy.Scope) ([]MaintenanceWindow, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]MaintenanceWindow, 0, len(store.windows))
	for _, v := range store.windows {
		if v.OrganizationID != scope.OrganizationID || v.SiteID != scope.SiteID {
			continue
		}
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartsAt.Before(result[j].StartsAt) })
	return result, nil
}
func (store *MemoryStore) EnsureIncident(_ context.Context, scope tenancy.Scope, incident Incident, event Event) (Incident, bool, error) {
	if err := scope.Validate(); err != nil {
		return Incident{}, false, err
	}
	if incident.OrganizationID != scope.OrganizationID || incident.SiteID != scope.SiteID || event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
		return Incident{}, false, tenancy.ErrInvalidScope
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, v := range store.incidents {
		if v.OrganizationID == scope.OrganizationID && v.SiteID == scope.SiteID && v.RuleID == incident.RuleID && v.AgentID == incident.AgentID && v.Status != StatusResolved {
			return v, false, nil
		}
	}
	store.incidents[incident.ID] = incident
	store.events[incident.ID] = []Event{event}
	return incident, true, nil
}
func (store *MemoryStore) ResolveIncident(_ context.Context, scope tenancy.Scope, ruleID, agentID string, value float64, message string, event Event) (*Incident, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
		return nil, tenancy.ErrInvalidScope
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for id, v := range store.incidents {
		if v.OrganizationID == scope.OrganizationID && v.SiteID == scope.SiteID && v.RuleID == ruleID && v.AgentID == agentID && v.Status != StatusResolved {
			now := event.OccurredAt
			v.Status = StatusResolved
			v.ResolvedAt = &now
			v.LatestValue = value
			v.Message = message
			event.IncidentID = v.ID
			store.incidents[id] = v
			store.events[id] = append(store.events[id], event)
			return &v, nil
		}
	}
	return nil, nil
}
func (store *MemoryStore) AcknowledgeIncident(_ context.Context, scope tenancy.Scope, id, actor string, at time.Time, event Event) (Incident, error) {
	if err := scope.Validate(); err != nil {
		return Incident{}, err
	}
	if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
		return Incident{}, tenancy.ErrInvalidScope
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	v, ok := store.incidents[id]
	if !ok || v.OrganizationID != scope.OrganizationID || v.SiteID != scope.SiteID {
		return Incident{}, ErrNotFound
	}
	if v.Status != StatusOpen {
		return Incident{}, ErrConflict
	}
	v.Status = StatusAcknowledged
	v.AcknowledgedAt = &at
	v.AcknowledgedBy = actor
	event.IncidentID = v.ID
	store.incidents[id] = v
	store.events[id] = append(store.events[id], event)
	return v, nil
}
func (store *MemoryStore) ListAlertIncidents(_ context.Context, scope tenancy.Scope, limit int) ([]Incident, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]Incident, 0, len(store.incidents))
	for _, v := range store.incidents {
		if v.OrganizationID != scope.OrganizationID || v.SiteID != scope.SiteID {
			continue
		}
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].OpenedAt.After(result[j].OpenedAt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func (store *MemoryStore) ListAlertEvents(_ context.Context, scope tenancy.Scope, id string) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	incident, ok := store.incidents[id]
	if !ok || incident.OrganizationID != scope.OrganizationID || incident.SiteID != scope.SiteID {
		return nil, ErrNotFound
	}
	return append([]Event(nil), store.events[id]...), nil
}

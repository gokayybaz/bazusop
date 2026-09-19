package alerting

import (
	"context"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func newTestService(t *testing.T, store Store, clock func() time.Time, agents ...tenancy.Agent) *Service {
	t.Helper()
	registry := inventory.NewService(inventory.NewMemoryStore())
	for _, agent := range agents {
		if err := registry.Report(t.Context(), agent, inventory.Facts{Hostname: "host-" + agent.ID + "-" + agent.SiteID, OSFamily: "linux", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024}); err != nil {
			t.Fatalf("register test host: %v", err)
		}
	}
	options := []Option{WithHostScopeChecker(registry.HasHost)}
	if clock != nil {
		options = append(options, WithClock(clock))
	}
	service, err := NewService(store, options...)
	if err != nil {
		t.Fatalf("new alert service: %v", err)
	}
	return service
}

func TestNewServiceRequiresHostScopeChecker(t *testing.T) {
	t.Parallel()
	if _, err := NewService(NewMemoryStore()); err == nil {
		t.Fatal("expected host scope checker to be required")
	}
}

func TestMemoryStoreRejectsForgedEventScopeAndUsesAuthoritativeIncidentID(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Microsecond)
	scope := tenancy.DefaultScope()
	incident := Incident{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "incident-1", RuleID: "rule-1", RuleName: "CPU", AgentID: "agent-1", Severity: SeverityCritical, Status: StatusOpen, Message: "high", OpenedAt: now}
	store := NewMemoryStore()
	if _, _, err := store.EnsureIncident(context.Background(), scope, incident, Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, IncidentID: "forged", ID: "event-1", Type: EventOpened, OccurredAt: now}); err != nil {
		t.Fatalf("ensure incident: %v", err)
	}
	if _, err := store.AcknowledgeIncident(context.Background(), scope, incident.ID, "ops", now, Event{OrganizationID: "other-org", SiteID: scope.SiteID, IncidentID: "forged", ID: "event-2", Type: EventAcknowledged, OccurredAt: now}); err != tenancy.ErrInvalidScope {
		t.Fatalf("expected forged event scope rejection, got %v", err)
	}
	if events, err := store.ListAlertEvents(context.Background(), scope, incident.ID); err != nil || len(events) != 1 || events[0].IncidentID != incident.ID {
		t.Fatalf("opening event did not use authoritative incident id: %#v, %v", events, err)
	}
	if _, err := store.AcknowledgeIncident(context.Background(), scope, incident.ID, "ops", now, Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, IncidentID: "forged", ID: "event-3", Type: EventAcknowledged, OccurredAt: now}); err != nil {
		t.Fatalf("acknowledge with forged incident id: %v", err)
	}
	events, err := store.ListAlertEvents(context.Background(), scope, incident.ID)
	if err != nil || len(events) != 2 || events[1].IncidentID != incident.ID {
		t.Fatalf("expected authoritative incident id, got %#v, %v", events, err)
	}
	incident2 := incident
	incident2.ID = "incident-2"
	incident2.RuleID = "rule-2"
	incident2.Status = StatusOpen
	if _, _, err := store.EnsureIncident(context.Background(), scope, incident2, Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "event-4", Type: EventOpened, OccurredAt: now}); err != nil {
		t.Fatalf("ensure second incident: %v", err)
	}
	if _, err := store.ResolveIncident(context.Background(), scope, incident2.RuleID, incident2.AgentID, 10, "recovered", Event{OrganizationID: "other-org", SiteID: scope.SiteID, IncidentID: "forged", ID: "event-5", Type: EventResolved, OccurredAt: now}); err != tenancy.ErrInvalidScope {
		t.Fatalf("expected forged resolution scope rejection, got %v", err)
	}
	if events, err := store.ListAlertEvents(context.Background(), scope, incident2.ID); err != nil || len(events) != 1 {
		t.Fatalf("forged resolution mutated history: %#v, %v", events, err)
	}
	if _, err := store.ResolveIncident(context.Background(), scope, incident2.RuleID, incident2.AgentID, 10, "recovered", Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, IncidentID: "forged", ID: "event-6", Type: EventResolved, OccurredAt: now}); err != nil {
		t.Fatalf("resolve with forged incident id: %v", err)
	}
	if events, err := store.ListAlertEvents(context.Background(), scope, incident2.ID); err != nil || len(events) != 2 || events[1].IncidentID != incident2.ID {
		t.Fatalf("expected authoritative resolution incident id: %#v, %v", events, err)
	}
}

func TestMetricIncidentLifecycle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	scope := tenancy.DefaultScope()
	service := newTestService(t, NewMemoryStore(), func() time.Time { return now }, tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})
	_, err := service.CreateRule(context.Background(), scope, RuleRequest{Name: "Yüksek CPU", Kind: KindMetric, Metric: MetricCPU, Threshold: 90, Severity: SeverityCritical, Enabled: true})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	if err := service.EvaluateTelemetry(context.Background(), scope, "agent-01", Telemetry{CPUPercent: 96, MemoryPercent: 40, DiskPercent: 30}); err != nil {
		t.Fatalf("evaluate breach: %v", err)
	}
	incidents, err := service.ListIncidents(context.Background(), scope, 50)
	if err != nil || len(incidents) != 1 || incidents[0].Status != StatusOpen {
		t.Fatalf("expected open incident, got %#v, %v", incidents, err)
	}

	now = now.Add(time.Minute)
	acknowledged, err := service.Acknowledge(context.Background(), scope, incidents[0].ID, "gokay")
	if err != nil || acknowledged.Status != StatusAcknowledged {
		t.Fatalf("acknowledge: %#v, %v", acknowledged, err)
	}

	now = now.Add(time.Minute)
	if err := service.EvaluateTelemetry(context.Background(), scope, "agent-01", Telemetry{CPUPercent: 45, MemoryPercent: 40, DiskPercent: 30}); err != nil {
		t.Fatalf("evaluate recovery: %v", err)
	}
	incidents, _ = service.ListIncidents(context.Background(), scope, 50)
	if incidents[0].Status != StatusResolved {
		t.Fatalf("expected resolved incident, got %#v", incidents[0])
	}
	events, err := service.ListEvents(context.Background(), scope, incidents[0].ID)
	if err != nil || len(events) != 3 || events[0].Type != EventOpened || events[1].Type != EventAcknowledged || events[2].Type != EventResolved {
		t.Fatalf("unexpected lifecycle: %#v, %v", events, err)
	}
}

func TestMaintenanceWindowSuppressesReachabilityIncident(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	scope := tenancy.DefaultScope()
	store := NewMemoryStore()
	store.RegisterHost(scope, "agent-01")
	service := newTestService(t, store, func() time.Time { return now }, tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})
	_, err := service.CreateRule(context.Background(), scope, RuleRequest{Name: "Agent erişilemiyor", Kind: KindReachability, StaleAfterSeconds: 120, Severity: SeverityWarning, Enabled: true})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	_, err = service.CreateMaintenance(context.Background(), scope, MaintenanceRequest{Name: "Kernel bakımı", AgentID: "agent-01", StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Hour), CreatedBy: "gokay"})
	if err != nil {
		t.Fatalf("create maintenance: %v", err)
	}

	if err := service.EvaluateReachability(context.Background(), scope, "agent-01", now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("evaluate maintenance: %v", err)
	}
	incidents, _ := service.ListIncidents(context.Background(), scope, 50)
	if len(incidents) != 0 {
		t.Fatalf("expected suppression, got %#v", incidents)
	}

	now = now.Add(2 * time.Hour)
	if err := service.EvaluateReachability(context.Background(), scope, "agent-01", now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("evaluate stale host: %v", err)
	}
	incidents, _ = service.ListIncidents(context.Background(), scope, 50)
	if len(incidents) != 1 || incidents[0].Status != StatusOpen {
		t.Fatalf("expected reachability incident, got %#v", incidents)
	}

	now = now.Add(time.Minute)
	if err := service.EvaluateReachability(context.Background(), scope, "agent-01", now); err != nil {
		t.Fatalf("evaluate recovery: %v", err)
	}
	incidents, _ = service.ListIncidents(context.Background(), scope, 50)
	if incidents[0].Status != StatusResolved {
		t.Fatalf("expected automatic resolution, got %#v", incidents[0])
	}
}

func TestInvalidRulesAndMaintenanceAreRejected(t *testing.T) {
	t.Parallel()
	service := newTestService(t, NewMemoryStore(), nil)
	scope := tenancy.DefaultScope()
	invalidRules := []RuleRequest{
		{Name: "shell", Kind: "command", Enabled: true},
		{Name: "cpu", Kind: KindMetric, Metric: MetricCPU, Threshold: 101, Severity: SeverityWarning, Enabled: true},
		{Name: "reach", Kind: KindReachability, StaleAfterSeconds: 10, Severity: SeverityWarning, Enabled: true},
	}
	for _, request := range invalidRules {
		if _, err := service.CreateRule(context.Background(), scope, request); err != ErrInvalidAlert {
			t.Fatalf("expected invalid rule %#v, got %v", request, err)
		}
	}
	now := time.Now().UTC()
	if _, err := service.CreateMaintenance(context.Background(), scope, MaintenanceRequest{Name: "bad", StartsAt: now, EndsAt: now.Add(-time.Minute), CreatedBy: "ops"}); err != ErrInvalidAlert {
		t.Fatalf("expected invalid maintenance, got %v", err)
	}
}

func TestMaintenanceTargetMustBelongToScope(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	store := NewMemoryStore()
	scope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	otherScope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-b"}
	store.RegisterHost(scope, "agent-01")
	service := newTestService(t, store, nil, tenancy.Agent{ID: "agent-01", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID})
	request := MaintenanceRequest{Name: "patch", AgentID: "agent-01", StartsAt: now, EndsAt: now.Add(time.Hour), CreatedBy: "ops"}
	if _, err := service.CreateMaintenance(context.Background(), otherScope, request); err != ErrNotFound {
		t.Fatalf("expected mismatched maintenance target to be not found, got %v", err)
	}
}

func TestAlertStateIsolatedBySite(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	oldScope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-old"}
	newScope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-new"}
	store.RegisterHost(oldScope, "shared-agent")
	store.RegisterHost(newScope, "shared-agent")
	service := newTestService(t, store, func() time.Time { return now },
		tenancy.Agent{ID: "shared-agent", OrganizationID: oldScope.OrganizationID, SiteID: oldScope.SiteID},
		tenancy.Agent{ID: "shared-agent", OrganizationID: newScope.OrganizationID, SiteID: newScope.SiteID})

	oldRule, err := service.CreateRule(context.Background(), oldScope, RuleRequest{Name: "Old CPU", Kind: KindMetric, Metric: MetricCPU, Threshold: 90, Severity: SeverityCritical, Enabled: true})
	if err != nil {
		t.Fatalf("create old rule: %v", err)
	}
	newRule, err := service.CreateRule(context.Background(), newScope, RuleRequest{Name: "New CPU", Kind: KindMetric, Metric: MetricCPU, Threshold: 90, Severity: SeverityCritical, Enabled: true})
	if err != nil {
		t.Fatalf("create new rule: %v", err)
	}
	if err := service.EvaluateTelemetry(context.Background(), oldScope, "shared-agent", Telemetry{CPUPercent: 99}); err != nil {
		t.Fatalf("evaluate old scope: %v", err)
	}
	oldIncidents, err := service.ListIncidents(context.Background(), oldScope, 50)
	if err != nil || len(oldIncidents) != 1 {
		t.Fatalf("expected old-site incident, got %#v, %v", oldIncidents, err)
	}
	if _, err := service.Acknowledge(context.Background(), oldScope, oldIncidents[0].ID, "ops"); err != nil {
		t.Fatalf("acknowledge old incident: %v", err)
	}
	if _, err := service.CreateMaintenance(context.Background(), oldScope, MaintenanceRequest{Name: "Old maintenance", AgentID: "shared-agent", StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Hour), CreatedBy: "ops"}); err != nil {
		t.Fatalf("create old maintenance: %v", err)
	}
	if err := service.EvaluateTelemetry(context.Background(), newScope, "shared-agent", Telemetry{CPUPercent: 99}); err != nil {
		t.Fatalf("evaluate new scope: %v", err)
	}
	incidents, err := service.ListIncidents(context.Background(), newScope, 50)
	if err != nil || len(incidents) != 1 || incidents[0].RuleID != newRule.ID {
		t.Fatalf("expected only new-site incident, got %#v, %v", incidents, err)
	}
	if _, err := service.Acknowledge(context.Background(), newScope, oldIncidents[0].ID, "ops"); err != ErrNotFound {
		t.Fatalf("expected cross-site lookup to be not found, got %v", err)
	}
	if _, err := service.Acknowledge(context.Background(), newScope, incidents[0].ID, "ops"); err != nil {
		t.Fatalf("acknowledge new incident: %v", err)
	}
	if oldRule.ID == newRule.ID {
		t.Fatal("expected distinct scoped rules")
	}
}

func TestMemoryStoreAllEventsIsScopedAndCarriesAgentID(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	scope := tenancy.DefaultScope()
	incident := Incident{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "incident-01", RuleID: "rule-01", RuleName: "Yüksek CPU", AgentID: "agent-01", Severity: SeverityCritical, Status: StatusOpen, Message: "CPU %96", LatestValue: 96, OpenedAt: time.Now().UTC()}
	event := Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "event-01", IncidentID: incident.ID, Type: EventOpened, Actor: "hub", Message: incident.Message, OccurredAt: incident.OpenedAt}
	if _, _, err := store.EnsureIncident(context.Background(), scope, incident, event); err != nil {
		t.Fatal(err)
	}
	otherScope := tenancy.Scope{OrganizationID: "org-other", SiteID: "site-other"}
	otherIncident := Incident{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "incident-02", RuleID: "rule-02", RuleName: "Disk kritik", AgentID: "agent-02", Severity: SeverityCritical, Status: StatusOpen, Message: "Disk %98", LatestValue: 98, OpenedAt: time.Now().UTC()}
	otherEvent := Event{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "event-02", IncidentID: otherIncident.ID, Type: EventOpened, Actor: "hub", Message: otherIncident.Message, OccurredAt: otherIncident.OpenedAt}
	if _, _, err := store.EnsureIncident(context.Background(), otherScope, otherIncident, otherEvent); err != nil {
		t.Fatal(err)
	}

	records, err := store.AllEvents(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].AgentID != "agent-01" || records[0].Event.IncidentID != incident.ID {
		t.Fatalf("expected only the in-scope event with its agent ID, got %#v", records)
	}
}

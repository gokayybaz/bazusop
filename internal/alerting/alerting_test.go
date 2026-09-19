package alerting

import (
	"context"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestMetricIncidentLifecycle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	scope := tenancy.DefaultScope()
	service := NewService(NewMemoryStore(), WithClock(func() time.Time { return now }))
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
	service := NewService(store, WithClock(func() time.Time { return now }))
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
	service := NewService(NewMemoryStore())
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
	service := NewService(store)
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
	service := NewService(store, WithClock(func() time.Time { return now }))

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

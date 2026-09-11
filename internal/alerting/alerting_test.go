package alerting

import (
	"context"
	"testing"
	"time"
)

func TestMetricIncidentLifecycle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	service := NewService(NewMemoryStore(), WithClock(func() time.Time { return now }))
	_, err := service.CreateRule(context.Background(), RuleRequest{Name: "Yüksek CPU", Kind: KindMetric, Metric: MetricCPU, Threshold: 90, Severity: SeverityCritical, Enabled: true})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	if err := service.EvaluateTelemetry(context.Background(), "agent-01", Telemetry{CPUPercent: 96, MemoryPercent: 40, DiskPercent: 30}); err != nil {
		t.Fatalf("evaluate breach: %v", err)
	}
	incidents, err := service.ListIncidents(context.Background(), 50)
	if err != nil || len(incidents) != 1 || incidents[0].Status != StatusOpen {
		t.Fatalf("expected open incident, got %#v, %v", incidents, err)
	}

	now = now.Add(time.Minute)
	acknowledged, err := service.Acknowledge(context.Background(), incidents[0].ID, "gokay")
	if err != nil || acknowledged.Status != StatusAcknowledged {
		t.Fatalf("acknowledge: %#v, %v", acknowledged, err)
	}

	now = now.Add(time.Minute)
	if err := service.EvaluateTelemetry(context.Background(), "agent-01", Telemetry{CPUPercent: 45, MemoryPercent: 40, DiskPercent: 30}); err != nil {
		t.Fatalf("evaluate recovery: %v", err)
	}
	incidents, _ = service.ListIncidents(context.Background(), 50)
	if incidents[0].Status != StatusResolved {
		t.Fatalf("expected resolved incident, got %#v", incidents[0])
	}
	events, err := service.ListEvents(context.Background(), incidents[0].ID)
	if err != nil || len(events) != 3 || events[0].Type != EventOpened || events[1].Type != EventAcknowledged || events[2].Type != EventResolved {
		t.Fatalf("unexpected lifecycle: %#v, %v", events, err)
	}
}

func TestMaintenanceWindowSuppressesReachabilityIncident(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	service := NewService(NewMemoryStore(), WithClock(func() time.Time { return now }))
	_, err := service.CreateRule(context.Background(), RuleRequest{Name: "Agent erişilemiyor", Kind: KindReachability, StaleAfterSeconds: 120, Severity: SeverityWarning, Enabled: true})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	_, err = service.CreateMaintenance(context.Background(), MaintenanceRequest{Name: "Kernel bakımı", AgentID: "agent-01", StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Hour), CreatedBy: "gokay"})
	if err != nil {
		t.Fatalf("create maintenance: %v", err)
	}

	if err := service.EvaluateReachability(context.Background(), "agent-01", now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("evaluate maintenance: %v", err)
	}
	incidents, _ := service.ListIncidents(context.Background(), 50)
	if len(incidents) != 0 {
		t.Fatalf("expected suppression, got %#v", incidents)
	}

	now = now.Add(2 * time.Hour)
	if err := service.EvaluateReachability(context.Background(), "agent-01", now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("evaluate stale host: %v", err)
	}
	incidents, _ = service.ListIncidents(context.Background(), 50)
	if len(incidents) != 1 || incidents[0].Status != StatusOpen {
		t.Fatalf("expected reachability incident, got %#v", incidents)
	}

	now = now.Add(time.Minute)
	if err := service.EvaluateReachability(context.Background(), "agent-01", now); err != nil {
		t.Fatalf("evaluate recovery: %v", err)
	}
	incidents, _ = service.ListIncidents(context.Background(), 50)
	if incidents[0].Status != StatusResolved {
		t.Fatalf("expected automatic resolution, got %#v", incidents[0])
	}
}

func TestInvalidRulesAndMaintenanceAreRejected(t *testing.T) {
	t.Parallel()
	service := NewService(NewMemoryStore())
	invalidRules := []RuleRequest{
		{Name: "shell", Kind: "command", Enabled: true},
		{Name: "cpu", Kind: KindMetric, Metric: MetricCPU, Threshold: 101, Severity: SeverityWarning, Enabled: true},
		{Name: "reach", Kind: KindReachability, StaleAfterSeconds: 10, Severity: SeverityWarning, Enabled: true},
	}
	for _, request := range invalidRules {
		if _, err := service.CreateRule(context.Background(), request); err != ErrInvalidAlert {
			t.Fatalf("expected invalid rule %#v, got %v", request, err)
		}
	}
	now := time.Now().UTC()
	if _, err := service.CreateMaintenance(context.Background(), MaintenanceRequest{Name: "bad", StartsAt: now, EndsAt: now.Add(-time.Minute), CreatedBy: "ops"}); err != ErrInvalidAlert {
		t.Fatalf("expected invalid maintenance, got %v", err)
	}
}

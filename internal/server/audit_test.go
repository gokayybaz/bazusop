package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/audit"
	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestAuditEventsMergeJobAndAlertHistoryOrderedByRecency(t *testing.T) {
	t.Parallel()
	scope := tenancy.DefaultScope()
	agent := tenancy.Agent{ID: "edge-01", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}

	registry := inventory.NewService(inventory.NewMemoryStore())
	if err := registry.Report(t.Context(), agent, inventory.Facts{Hostname: "edge-01", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}); err != nil {
		t.Fatal(err)
	}

	jobStore := jobs.NewMemoryStore()
	jobService, err := jobs.NewService(jobStore, jobs.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobService.Create(t.Context(), scope, agent.ID, jobs.CreateRequest{Action: jobs.ActionServiceRestart, Target: "nginx.service", ApprovedBy: "gokay", Reason: "config rollout"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	alertStore := alerting.NewMemoryStore()
	alertService, err := alerting.NewService(alertStore, alerting.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := alertService.CreateRule(t.Context(), scope, alerting.RuleRequest{Name: "Yüksek CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := alertService.EvaluateTelemetry(t.Context(), scope, agent.ID, alerting.Telemetry{CPUPercent: 97}); err != nil {
		t.Fatal(err)
	}
	if incidents, err := alertService.ListIncidents(t.Context(), scope, 10); err != nil || len(incidents) != 1 {
		t.Fatalf("expected one incident, got %#v %v", incidents, err)
	}

	auditService := audit.NewService(audit.NewMemoryStore(jobStore, alertStore))
	handler := server.NewHandler(server.WithAudit(auditService), server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/audit/events", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Events []audit.Event `json:"events"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode audit events: %v", err)
	}
	if len(payload.Events) != 2 {
		t.Fatalf("expected 2 merged events, got %#v", payload.Events)
	}
	if payload.Events[0].Source != audit.SourceAlert || payload.Events[0].AgentID != agent.ID {
		t.Fatalf("expected the more recent alert event first, got %#v", payload.Events[0])
	}
	if payload.Events[1].Source != audit.SourceJob || payload.Events[1].ReferenceID != job.ID || payload.Events[1].AgentID != agent.ID {
		t.Fatalf("expected the older job event second, got %#v", payload.Events[1])
	}

	limited := httptest.NewRecorder()
	handler.ServeHTTP(limited, httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?limit=1", nil))
	var limitedPayload struct {
		Events []audit.Event `json:"events"`
	}
	if err := json.NewDecoder(limited.Body).Decode(&limitedPayload); err != nil || len(limitedPayload.Events) != 1 {
		t.Fatalf("expected exactly one event with limit=1, got %#v %v", limitedPayload, err)
	}

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?limit=0", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range limit, got %d", invalid.Code)
	}
}

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresAuditEventsMergeJobsAndAlertsScopedBySite(t *testing.T) {
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	orgID := "audit-org-" + suffix
	scope := tenancy.Scope{OrganizationID: orgID, SiteID: "audit-site-a-" + suffix}
	otherScope := tenancy.Scope{OrganizationID: orgID, SiteID: "audit-site-b-" + suffix}
	agent := tenancy.Agent{ID: "audit-agent-" + suffix, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}

	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Audit test')`, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, table := range []string{"job_events", "jobs", "alert_events", "alert_incidents", "alert_rules", "hosts", "agents", "sites", "organizations"} {
			column := "organization_id"
			if table == "organizations" {
				column = "id"
			}
			if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM "+table+" WHERE "+column+"=$1", orgID); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	}()
	for _, site := range []tenancy.Scope{scope, otherScope} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1, $2, $1, $1)`, site.SiteID, site.OrganizationID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO agents (id, organization_id, site_id) VALUES ($1, $2, $3)`, agent.ID, agent.OrganizationID, agent.SiteID); err != nil {
		t.Fatal(err)
	}

	hosts := inventory.NewService(store)
	facts := inventory.Facts{Hostname: "edge", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}
	if err := hosts.Report(ctx, agent, facts); err != nil {
		t.Fatal(err)
	}
	jobService, err := jobs.NewService(store, jobs.WithHostScopeChecker(hosts.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobService.Create(ctx, scope, agent.ID, jobs.CreateRequest{Action: jobs.ActionHostReboot, ApprovedBy: "ops", Reason: "maintenance"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	// The incident must sort after the job event regardless of wall-clock skew between the
	// two Create calls, so anchor it a full minute past the job's own (real) clock reading.
	now := job.RequestedAt.Add(time.Minute)
	rule := alerting.Rule{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "audit-rule-" + suffix, RuleRequest: alerting.RuleRequest{Name: "Yüksek CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}, CreatedAt: now}
	if err := store.CreateAlertRule(ctx, scope, rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	incident := alerting.Incident{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "audit-incident-" + suffix, RuleID: rule.ID, RuleName: rule.Name, AgentID: agent.ID, Severity: alerting.SeverityCritical, Status: alerting.StatusOpen, Message: "CPU %96", LatestValue: 96, OpenedAt: now}
	incidentEvent := alerting.Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "audit-event-" + suffix, IncidentID: "forged", Type: alerting.EventOpened, Actor: "hub", Message: "CPU %96", OccurredAt: now}
	if _, created, err := store.EnsureIncident(ctx, scope, incident, incidentEvent); err != nil || !created {
		t.Fatalf("create incident: created=%v err=%v", created, err)
	}

	otherRule := alerting.Rule{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "audit-other-rule-" + suffix, RuleRequest: alerting.RuleRequest{Name: "Disk kritik", Kind: alerting.KindMetric, Metric: alerting.MetricDisk, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}, CreatedAt: now}
	if err := store.CreateAlertRule(ctx, otherScope, otherRule); err != nil {
		t.Fatalf("create other-site rule: %v", err)
	}
	otherIncident := alerting.Incident{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "audit-other-incident-" + suffix, RuleID: otherRule.ID, RuleName: otherRule.Name, AgentID: "other-agent", Severity: alerting.SeverityCritical, Status: alerting.StatusOpen, Message: "Disk %98", LatestValue: 98, OpenedAt: now}
	otherIncidentEvent := alerting.Event{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "audit-other-event-" + suffix, IncidentID: "forged", Type: alerting.EventOpened, Actor: "hub", Message: "Disk %98", OccurredAt: now}
	if _, created, err := store.EnsureIncident(ctx, otherScope, otherIncident, otherIncidentEvent); err != nil || !created {
		t.Fatalf("create other-site incident: created=%v err=%v", created, err)
	}

	events, err := store.ListAuditEvents(ctx, scope, 50)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 in-scope events, got %d: %#v", len(events), events)
	}
	if events[0].Source != "alert" || events[0].ReferenceID != incident.ID || events[0].AgentID != agent.ID {
		t.Fatalf("expected the more recent alert event first, got %#v", events[0])
	}
	if events[1].Source != "job" || events[1].ReferenceID != job.ID || events[1].AgentID != agent.ID {
		t.Fatalf("expected the older job event second, got %#v", events[1])
	}
	for _, event := range events {
		if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
			t.Fatalf("cross-site audit event leaked: %#v", event)
		}
	}

	if limited, err := store.ListAuditEvents(ctx, scope, 1); err != nil || len(limited) != 1 || limited[0].Source != "alert" {
		t.Fatalf("expected limit to keep only the most recent event, got %#v, %v", limited, err)
	}
}

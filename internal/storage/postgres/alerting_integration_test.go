package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresAlertingIsScopedAndActiveIncidentIndexIsReplaySafe(t *testing.T) {
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
	orgID := "alert-org-" + suffix
	oldScope := tenancy.Scope{OrganizationID: orgID, SiteID: "alert-site-old-" + suffix}
	newScope := tenancy.Scope{OrganizationID: orgID, SiteID: "alert-site-new-" + suffix}
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id,name) VALUES ($1,$2)`, orgID, "Alert test"); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []tenancy.Scope{oldScope, newScope} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id,organization_id,name,slug) VALUES ($1,$2,$1,$1)`, scope.SiteID, orgID); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, table := range []string{"alert_events", "alert_incidents", "maintenance_windows", "alert_rules", "agents", "hosts", "sites", "organizations"} {
			column := "organization_id"
			if table == "organizations" {
				column = "id"
			}
			if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM "+table+" WHERE "+column+"=$1", orgID); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	}()

	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	oldRule := alerting.Rule{OrganizationID: oldScope.OrganizationID, SiteID: oldScope.SiteID, ID: "alert-rule-old-" + suffix, RuleRequest: alerting.RuleRequest{Name: "Old CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}, CreatedAt: now}
	newRule := alerting.Rule{OrganizationID: newScope.OrganizationID, SiteID: newScope.SiteID, ID: "alert-rule-new-" + suffix, RuleRequest: alerting.RuleRequest{Name: "New CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}, CreatedAt: now.Add(time.Second)}
	if err := store.CreateAlertRule(ctx, oldScope, oldRule); err != nil {
		t.Fatalf("create old rule: %v", err)
	}
	if err := store.CreateAlertRule(ctx, newScope, newRule); err != nil {
		t.Fatalf("create new rule: %v", err)
	}
	for _, scope := range []tenancy.Scope{oldScope, newScope} {
		windows := []alerting.MaintenanceWindow{{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "alert-window-" + strings.TrimPrefix(scope.SiteID, "alert-site-") + "-" + suffix, MaintenanceRequest: alerting.MaintenanceRequest{Name: "global window", StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Hour), CreatedBy: "ops"}, CreatedAt: now}}
		if err := store.CreateMaintenanceWindow(ctx, scope, windows[0]); err != nil {
			t.Fatalf("create %s window: %v", scope.SiteID, err)
		}
		listed, err := store.ListMaintenanceWindows(ctx, scope)
		if err != nil || len(listed) != 1 || listed[0].SiteID != scope.SiteID {
			t.Fatalf("scoped windows for %s: %#v, %v", scope.SiteID, listed, err)
		}
	}
	if rules, err := store.ListAlertRules(ctx, oldScope); err != nil || len(rules) != 1 || rules[0].ID != oldRule.ID {
		t.Fatalf("old scoped rules: %#v, %v", rules, err)
	}
	if rules, err := store.ListAlertRules(ctx, newScope); err != nil || len(rules) != 1 || rules[0].ID != newRule.ID {
		t.Fatalf("new scoped rules: %#v, %v", rules, err)
	}

	makeIncident := func(scope tenancy.Scope, rule alerting.Rule, id string) (alerting.Incident, alerting.Event) {
		incident := alerting.Incident{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: id, RuleID: rule.ID, RuleName: rule.Name, AgentID: "shared-agent", Severity: rule.Severity, Status: alerting.StatusOpen, Message: "high", LatestValue: 99, OpenedAt: now}
		event := alerting.Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: id + "-opened", IncidentID: "forged", Type: alerting.EventOpened, Actor: "hub", Message: "high", OccurredAt: now}
		return incident, event
	}
	oldIncident, oldEvent := makeIncident(oldScope, oldRule, "alert-incident-old-"+suffix)
	if _, created, err := store.EnsureIncident(ctx, oldScope, oldIncident, oldEvent); err != nil || !created {
		t.Fatalf("create old incident: created=%v err=%v", created, err)
	}
	newIncident, newEvent := makeIncident(newScope, newRule, "alert-incident-new-"+suffix)
	if _, created, err := store.EnsureIncident(ctx, newScope, newIncident, newEvent); err != nil || !created {
		t.Fatalf("create new incident: created=%v err=%v", created, err)
	}
	if incidents, err := store.ListAlertIncidents(ctx, oldScope, 50); err != nil || len(incidents) != 1 || incidents[0].ID != oldIncident.ID {
		t.Fatalf("old scoped incidents: %#v, %v", incidents, err)
	}
	if incidents, err := store.ListAlertIncidents(ctx, newScope, 50); err != nil || len(incidents) != 1 || incidents[0].ID != newIncident.ID {
		t.Fatalf("new scoped incidents: %#v, %v", incidents, err)
	}
	if _, err := store.AcknowledgeIncident(ctx, newScope, oldIncident.ID, "ops", now, alerting.Event{OrganizationID: newScope.OrganizationID, SiteID: newScope.SiteID, ID: "cross-site-ack", IncidentID: "forged", Type: alerting.EventAcknowledged, OccurredAt: now}); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("cross-site acknowledgement: %v", err)
	}
	if _, err := store.AcknowledgeIncident(ctx, oldScope, oldIncident.ID, "ops", now, alerting.Event{OrganizationID: oldScope.OrganizationID, SiteID: oldScope.SiteID, ID: "old-ack", IncidentID: "forged", Type: alerting.EventAcknowledged, OccurredAt: now}); err != nil {
		t.Fatalf("acknowledge old incident: %v", err)
	}
	if events, err := store.ListAlertEvents(ctx, oldScope, oldIncident.ID); err != nil || len(events) != 2 || events[1].IncidentID != oldIncident.ID {
		t.Fatalf("old scoped events: %#v, %v", events, err)
	}
	if _, err := store.ListAlertEvents(ctx, newScope, oldIncident.ID); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("cross-site event read: %v", err)
	}

	concurrentRule := oldRule
	concurrentRule.ID = "alert-rule-concurrent-" + suffix
	if err := store.CreateAlertRule(ctx, oldScope, concurrentRule); err != nil {
		t.Fatalf("create concurrent rule: %v", err)
	}
	const attempts = 8
	var wait sync.WaitGroup
	results := make(chan bool, attempts)
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			incident, event := makeIncident(oldScope, concurrentRule, fmt.Sprintf("alert-concurrent-%s-%d", suffix, index))
			_, created, err := store.EnsureIncident(ctx, oldScope, incident, event)
			if err != nil {
				t.Errorf("concurrent ensure %d: %v", index, err)
				return
			}
			results <- created
		}(index)
	}
	wait.Wait()
	close(results)
	createdCount := 0
	for created := range results {
		if created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("expected one active incident under concurrency, got %d", createdCount)
	}
	if _, created, err := store.EnsureIncident(ctx, newScope, func() alerting.Incident {
		incident, _ := makeIncident(newScope, concurrentRule, "alert-separate-site-"+suffix)
		return incident
	}(), func() alerting.Event {
		_, event := makeIncident(newScope, concurrentRule, "alert-separate-site-"+suffix)
		return event
	}()); err != nil || !created {
		t.Fatalf("separate-site active incident: created=%v err=%v", created, err)
	}

	migration, err := migrationFiles.ReadFile("migrations/017_scoped_alerting_active_index.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("replay migration first time: %v", err)
	}
	if _, err := store.pool.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("replay migration second time: %v", err)
	}
	var indexDef string
	if err := store.pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname=current_schema() AND indexname='alert_incidents_active_scoped_rule_agent_idx'`).Scan(&indexDef); err != nil {
		t.Fatalf("read scoped active index: %v", err)
	}
	if !strings.Contains(indexDef, "organization_id") || !strings.Contains(indexDef, "site_id") || !strings.Contains(indexDef, "rule_id") || !strings.Contains(indexDef, "agent_id") {
		t.Fatalf("active index is not scoped: %s", indexDef)
	}
	var oldIndexCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname='alert_incidents_active_rule_agent_idx'`).Scan(&oldIndexCount); err != nil {
		t.Fatal(err)
	}
	if oldIndexCount != 0 {
		t.Fatalf("legacy global active index still exists: %d", oldIndexCount)
	}
}

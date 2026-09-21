package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresAuditTrailIsAppendOnly(t *testing.T) {
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	scope := tenancy.Scope{OrganizationID: "audittrail-org-" + suffix, SiteID: "audittrail-site-" + suffix}
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Audit trail test')`, scope.OrganizationID); err != nil {
		t.Fatal(err)
	}
	// audit_events rows are deliberately never cleaned up here: the table is
	// append-only by design (that's exactly what this test verifies below),
	// so the test row is left behind in the disposable test database. sites
	// and organizations have no FK from audit_events (same reason), so their
	// cleanup is unaffected by the leftover row.
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, table := range []string{"sites", "organizations"} {
			column := "organization_id"
			if table == "organizations" {
				column = "id"
			}
			if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM "+table+" WHERE "+column+"=$1", scope.OrganizationID); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	}()
	if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1, $2, $1, $1)`, scope.SiteID, scope.OrganizationID); err != nil {
		t.Fatal(err)
	}

	event := audittrail.Event{
		EventID: "audittrail-event-" + suffix, OccurredAt: time.Now().UTC(),
		CorrelationID: "corr-" + suffix, ActorType: audittrail.ActorAgent, ActorID: "agent-" + suffix,
		OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
		Action: "GET", ResourceType: "health", Outcome: audittrail.OutcomeSuccess,
		SourceIP: "127.0.0.1", UserAgent: "test-agent",
	}
	if err := store.Record(ctx, event); err != nil {
		t.Fatalf("record event: %v", err)
	}

	events, err := store.ListAuditTrail(ctx, scope, 10)
	if err != nil || len(events) != 1 || events[0].EventID != event.EventID || events[0].CorrelationID != event.CorrelationID || events[0].Outcome != audittrail.OutcomeSuccess {
		t.Fatalf("expected the recorded event back, got %#v, %v", events, err)
	}

	if _, err := store.pool.Exec(ctx, `UPDATE audit_events SET outcome='failure' WHERE event_id=$1`, event.EventID); err == nil {
		t.Fatal("expected UPDATE on audit_events to be rejected")
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM audit_events WHERE event_id=$1`, event.EventID); err == nil {
		t.Fatal("expected DELETE on audit_events to be rejected")
	}
}

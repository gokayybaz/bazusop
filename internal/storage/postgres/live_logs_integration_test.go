package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
	"github.com/jackc/pgx/v5"
)

func scopedStreamsFixture(t *testing.T) (*Store, []tenancy.Agent) {
	t.Helper()
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	store, err := Open(t.Context(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	agents := []tenancy.Agent{
		{ID: "stream-agent-" + suffix, OrganizationID: "stream-org-" + suffix, SiteID: "stream-old-" + suffix},
		{ID: "stream-agent-" + suffix, OrganizationID: "stream-org-" + suffix, SiteID: "stream-new-" + suffix},
		{ID: "stream-agent-" + suffix, OrganizationID: "stream-other-" + suffix, SiteID: "stream-other-site-" + suffix},
	}
	t.Cleanup(func() {
		for _, agent := range agents {
			for _, table := range []string{"log_entries", "services", "telemetry_samples", "hosts", "agents"} {
				if _, err := store.pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE organization_id=$1 AND site_id=$2", agent.OrganizationID, agent.SiteID); err != nil {
					t.Errorf("cleanup %s: %v", table, err)
				}
			}
			if _, err := store.pool.Exec(context.Background(), "DELETE FROM sites WHERE organization_id=$1 AND id=$2", agent.OrganizationID, agent.SiteID); err != nil {
				t.Error(err)
			}
		}
		for _, org := range []string{agents[0].OrganizationID, agents[2].OrganizationID} {
			if _, err := store.pool.Exec(context.Background(), "DELETE FROM organizations WHERE id=$1", org); err != nil {
				t.Error(err)
			}
		}
	})
	for _, org := range []string{agents[0].OrganizationID, agents[2].OrganizationID} {
		if _, err := store.pool.Exec(t.Context(), "INSERT INTO organizations (id, name) VALUES ($1, $1)", org); err != nil {
			t.Fatal(err)
		}
	}
	for _, agent := range agents {
		if _, err := store.pool.Exec(t.Context(), "INSERT INTO sites (id, organization_id, name, slug) VALUES ($1, $2, $1, $1)", agent.SiteID, agent.OrganizationID); err != nil {
			t.Fatal(err)
		}
	}
	agent := agents[0]
	if _, err := store.pool.Exec(t.Context(), "INSERT INTO agents (id, organization_id, site_id) VALUES ($1, $2, $3)", agent.ID, agent.OrganizationID, agent.SiteID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Upsert(t.Context(), inventory.Host{AgentID: agent.ID, OrganizationID: agent.OrganizationID, SiteID: agent.SiteID, Hostname: "stream-test", OSFamily: "linux", Architecture: "amd64", CPUCores: 1, MemoryBytes: 1024, IPAddresses: []string{}, FirstSeenAt: now, LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	return store, agents
}

func moveStreamAgent(t *testing.T, store *Store, from, to tenancy.Agent) {
	t.Helper()
	transaction, err := store.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(t.Context()) }()
	for _, target := range []struct{ table, id string }{{"agents", "id"}, {"hosts", "agent_id"}} {
		if _, err := transaction.Exec(t.Context(), "UPDATE "+target.table+" SET organization_id=$4, site_id=$5 WHERE "+target.id+"=$1 AND organization_id=$2 AND site_id=$3", from.ID, from.OrganizationID, from.SiteID, to.OrganizationID, to.SiteID); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStreamHistoryUsesStoredScopeAfterMove(t *testing.T) {
	store, agents := scopedStreamsFixture(t)
	metrics := telemetry.NewService(store)
	services := serviceinventory.NewService(store)
	logs := logstream.NewService(store)
	at := time.Date(2026, 9, 13, 10, 0, 0, 123000, time.UTC)
	for i, agent := range agents {
		if i > 0 {
			moveStreamAgent(t, store, agents[i-1], agent)
		}
		if err := metrics.Report(t.Context(), agent, telemetry.Sample{RecordedAt: at, CPUPercent: float64(i + 1), MemoryPercent: 20, DiskPercent: 30, NetworkRXBytes: 123, NetworkTXBytes: 456}); err != nil {
			t.Fatal(err)
		}
		if err := services.Report(t.Context(), agent, serviceinventory.Snapshot{ObservedAt: at, Services: []serviceinventory.Fact{{Name: "same.service", DisplayName: agent.SiteID, State: "running", StartupType: "automatic"}}}); err != nil {
			t.Fatal(err)
		}
		batch := logstream.Batch{Entries: []logstream.Entry{{ID: "0123456789abcdef0123456789abcdef", OccurredAt: at, Collector: "file", Source: "app.log", Severity: "warn", Message: agent.SiteID}}}
		if err := logs.Ingest(t.Context(), agent, batch); err != nil {
			t.Fatal(err)
		}
		if err := logs.Ingest(t.Context(), agent, batch); err != nil {
			t.Fatal(err)
		}
	}
	for i, agent := range agents {
		samples, err := metrics.History(t.Context(), agent.Scope(), agent.ID, at.Add(-time.Second), at.Add(time.Second), 10)
		if err != nil || len(samples) != 1 || samples[0].CPUPercent != float64(i+1) || samples[0].OrganizationID != agent.OrganizationID || samples[0].SiteID != agent.SiteID || samples[0].NetworkRXBytes != 123 || samples[0].NetworkTXBytes != 456 {
			t.Fatalf("telemetry crossed scope/scan: %#v, %v", samples, err)
		}
		items, err := services.List(t.Context(), agent.Scope(), agent.ID, serviceinventory.Filter{State: serviceinventory.StateRunning, Query: "same"})
		if err != nil || len(items) != 1 || items[0].DisplayName != agent.SiteID || items[0].OrganizationID != agent.OrganizationID || items[0].SiteID != agent.SiteID {
			t.Fatalf("services crossed scope/scan: %#v, %v", items, err)
		}
		entries, err := logs.Search(t.Context(), logstream.Query{Scope: agent.Scope(), AgentID: agent.ID, From: at.Add(-time.Second), To: at.Add(time.Second), Collector: "file", Severity: "warn", Source: "app", Text: agent.SiteID, Limit: 10})
		if err != nil || len(entries) != 1 || entries[0].Message != agent.SiteID || entries[0].OrganizationID != agent.OrganizationID || entries[0].SiteID != agent.SiteID {
			t.Fatalf("logs crossed scope/scan: %#v, %v", entries, err)
		}
	}
	// A valid-looking organization cannot read a different organization's site.
	wrongScope := tenancy.Scope{OrganizationID: agents[2].OrganizationID, SiteID: agents[0].SiteID}
	if samples, err := metrics.History(t.Context(), wrongScope, agents[0].ID, at.Add(-time.Second), at.Add(time.Second), 10); err != nil || len(samples) != 0 {
		t.Fatalf("telemetry ignored organization: %#v, %v", samples, err)
	}
	if items, err := services.List(t.Context(), wrongScope, agents[0].ID, serviceinventory.Filter{}); err != nil || len(items) != 0 {
		t.Fatalf("services ignored organization: %#v, %v", items, err)
	}
	if entries, err := logs.Search(t.Context(), logstream.Query{Scope: wrongScope, AgentID: agents[0].ID, From: at.Add(-time.Second), To: at.Add(time.Second), Limit: 10}); err != nil || len(entries) != 0 {
		t.Fatalf("logs ignored organization: %#v, %v", entries, err)
	}
	for _, agent := range agents[:2] {
		if err := services.Report(t.Context(), agent, serviceinventory.Snapshot{ObservedAt: at.Add(time.Minute)}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("stale scope acquired snapshot host lock: %v", err)
		}
	}
	if err := services.Report(t.Context(), agents[2], serviceinventory.Snapshot{ObservedAt: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	for i, agent := range agents {
		items, err := services.List(t.Context(), agent.Scope(), agent.ID, serviceinventory.Filter{})
		want := 1
		if i == 2 {
			want = 0
		}
		if err != nil || len(items) != want {
			t.Fatalf("empty snapshot crossed scope: %#v, %v", items, err)
		}
	}
	migration, err := migrationFiles.ReadFile("migrations/016_scoped_service_log_keys.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := store.pool.Exec(t.Context(), string(migration)); err != nil {
			t.Fatalf("replay migration: %v", err)
		}
	}
}

func TestPostgresDeliversLiveLogsAcrossStores(t *testing.T) {
	first, agents := scopedStreamsFixture(t)
	second, err := Open(t.Context(), os.Getenv("BAZUSOP_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	streaming := logstream.NewService(second)
	ingesting := logstream.NewService(first)
	streams := make([]<-chan logstream.Entry, len(agents))
	for i, agent := range agents {
		streams[i], err = streaming.Subscribe(t.Context(), agent.Scope(), agent.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	for i, agent := range agents {
		if i > 0 {
			moveStreamAgent(t, first, agents[i-1], agent)
		}
		if err := ingesting.Ingest(t.Context(), agent, logstream.Batch{Entries: []logstream.Entry{{ID: "0123456789abcdef0123456789abcdef", OccurredAt: at, Collector: "file", Source: "integration.log", Severity: "info", Message: agent.SiteID}}}); err != nil {
			t.Fatal(err)
		}
		select {
		case entry := <-streams[i]:
			if entry.AgentID != agent.ID || entry.OrganizationID != agent.OrganizationID || entry.SiteID != agent.SiteID || entry.Message != agent.SiteID {
				t.Fatalf("unexpected live entry: %#v", entry)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for matching PostgreSQL live log")
		}
		for j, stream := range streams {
			select {
			case entry := <-stream:
				t.Fatalf("subscriber %d received extra/cross-scope log: %#v", j, entry)
			default:
			}
		}
	}
	// A reused ID with a missing timestamp cannot replay a stored entry.
	payload := `[{"organization_id":"` + agents[0].OrganizationID + `","site_id":"` + agents[0].SiteID + `","id":"0123456789abcdef0123456789abcdef","occurred_at":"2026-09-13T10:00:01Z"}]`
	second.deliverLogNotification(t.Context(), payload)
	second.deliverLogNotification(t.Context(), `[{"organization_id":"`+agents[2].OrganizationID+`","site_id":"`+agents[0].SiteID+`","id":"0123456789abcdef0123456789abcdef","occurred_at":"2026-09-13T10:00:00Z"}]`)
	second.deliverLogNotification(t.Context(), `["0123456789abcdef0123456789abcdef"]`)
	for _, stream := range streams {
		select {
		case entry := <-stream:
			t.Fatalf("unscoped or missing identity replayed a log: %#v", entry)
		default:
		}
	}
}

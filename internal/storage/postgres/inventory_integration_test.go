package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresInventoryScopeAndAgentMovePreserveHistory(t *testing.T) {
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
	org := "inventory-org-" + suffix
	otherOrg := "inventory-other-org-" + suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Inventory test'), ($2, 'Other inventory test')`, org, otherOrg); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, table := range []string{"telemetry_samples", "hosts", "agents", "sites", "organizations"} {
			column := "organization_id"
			if table == "organizations" {
				column = "id"
			}
			if _, err := store.pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE "+column+" = ANY($1)", []string{org, otherOrg}); err != nil {
				t.Errorf("clean up %s: %v", table, err)
			}
		}
	}()
	agents := []tenancy.Agent{
		{ID: "inventory-agent-a-" + suffix, OrganizationID: org, SiteID: "inventory-site-a-" + suffix},
		{ID: "inventory-agent-b-" + suffix, OrganizationID: org, SiteID: "inventory-site-b-" + suffix},
		{ID: "inventory-agent-c-" + suffix, OrganizationID: otherOrg, SiteID: "inventory-site-c-" + suffix},
	}
	for _, agent := range agents {
		if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1, $2, $1, $1)`, agent.SiteID, agent.OrganizationID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO agents (id, organization_id, site_id) VALUES ($1, $2, $3)`, agent.ID, agent.OrganizationID, agent.SiteID); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service := inventory.NewService(store, inventory.WithClock(func() time.Time { return now }))
	facts := inventory.Facts{Hostname: "edge", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", KernelVersion: "6.8", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}
	for _, agent := range agents {
		if err := service.Report(ctx, agent, facts); err != nil {
			t.Fatal(err)
		}
	}
	for _, agent := range agents {
		hosts, err := service.List(ctx, agent.Scope())
		if err != nil || len(hosts) != 1 || hosts[0].AgentID != agent.ID || hosts[0].Scope() != agent.Scope() {
			t.Fatalf("cross-scope read: %#v, %v", hosts, err)
		}
		if hosts[0].MemoryBytes != 1024 || hosts[0].OSVersion != "24.04" || len(hosts[0].IPAddresses) != 1 {
			t.Fatalf("incorrect scan: %#v", hosts[0])
		}
	}
	for _, invalid := range []tenancy.Agent{
		{ID: agents[0].ID, OrganizationID: org, SiteID: agents[1].SiteID},
		{ID: agents[0].ID, OrganizationID: otherOrg, SiteID: agents[2].SiteID},
		{ID: "missing-" + suffix, OrganizationID: org, SiteID: agents[0].SiteID},
	} {
		if err := service.Report(ctx, invalid, facts); !errors.Is(err, tenancy.ErrNotFound) {
			t.Fatalf("wrong assignment accepted: %#v, %v", invalid, err)
		}
	}
	oldAgent := agents[0]
	// Reports cannot repair an inconsistent current host by silently moving it.
	if _, err := store.pool.Exec(ctx, `UPDATE hosts SET site_id=$4 WHERE agent_id=$1 AND organization_id=$2 AND site_id=$3`, oldAgent.ID, oldAgent.OrganizationID, oldAgent.SiteID, agents[1].SiteID); err != nil {
		t.Fatal(err)
	}
	if err := service.Report(ctx, oldAgent, facts); !errors.Is(err, tenancy.ErrNotFound) {
		t.Fatalf("mismatched current host assignment accepted: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE hosts SET site_id=$4 WHERE agent_id=$1 AND organization_id=$2 AND site_id=$3`, oldAgent.ID, oldAgent.OrganizationID, agents[1].SiteID, oldAgent.SiteID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO telemetry_samples (organization_id, site_id, agent_id, recorded_at, cpu_percent, memory_percent, disk_percent, network_rx_bytes, network_tx_bytes) VALUES ($1, $2, $3, $4, 1, 2, 3, 4, 5)`, oldAgent.OrganizationID, oldAgent.SiteID, oldAgent.ID, now); err != nil {
		t.Fatal(err)
	}
	// A future privileged move owns this transaction. Reports must never move
	// current hosts or rewrite historical ownership on their own.
	move, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = move.Rollback(ctx) }()
	newAgent := tenancy.Agent{ID: oldAgent.ID, OrganizationID: agents[2].OrganizationID, SiteID: agents[2].SiteID}
	if _, err := move.Exec(ctx, `UPDATE agents SET organization_id=$4, site_id=$5, moved_at=now() WHERE id=$1 AND organization_id=$2 AND site_id=$3`, oldAgent.ID, oldAgent.OrganizationID, oldAgent.SiteID, newAgent.OrganizationID, newAgent.SiteID); err != nil {
		t.Fatal(err)
	}
	if _, err := move.Exec(ctx, `UPDATE hosts SET organization_id=$4, site_id=$5 WHERE agent_id=$1 AND organization_id=$2 AND site_id=$3`, oldAgent.ID, oldAgent.OrganizationID, oldAgent.SiteID, newAgent.OrganizationID, newAgent.SiteID); err != nil {
		t.Fatal(err)
	}
	if err := move.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.Report(ctx, oldAgent, facts); !errors.Is(err, tenancy.ErrNotFound) {
		t.Fatalf("stale assignment restored host: %v", err)
	}
	now = now.Add(time.Minute)
	facts.Hostname = "updated-edge"
	if err := service.Report(ctx, newAgent, facts); err != nil {
		t.Fatal(err)
	}
	oldHosts, err := service.List(ctx, oldAgent.Scope())
	if err != nil || len(oldHosts) != 0 {
		t.Fatalf("old scope retained current host: %#v, %v", oldHosts, err)
	}
	newHosts, err := service.List(ctx, newAgent.Scope())
	if err != nil || len(newHosts) != 2 {
		t.Fatalf("new scope hosts: %#v, %v", newHosts, err)
	}
	movedHost := newHosts[1]
	if movedHost.AgentID != oldAgent.ID || movedHost.Scope() != newAgent.Scope() || movedHost.Hostname != "updated-edge" || !movedHost.LastSeenAt.Equal(now) || !movedHost.FirstSeenAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("moved host: %#v", movedHost)
	}
	var historyScope tenancy.Scope
	if err := store.pool.QueryRow(ctx, `SELECT organization_id, site_id FROM telemetry_samples WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3`, oldAgent.OrganizationID, oldAgent.SiteID, oldAgent.ID).Scan(&historyScope.OrganizationID, &historyScope.SiteID); err != nil || historyScope != oldAgent.Scope() {
		t.Fatalf("historical ownership changed: %#v, %v", historyScope, err)
	}
}

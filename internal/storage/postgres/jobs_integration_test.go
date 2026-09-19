package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresJobsUseStoredScopeAfterAgentMove(t *testing.T) {
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
	org := "jobs-org-" + suffix
	old := tenancy.Agent{ID: "jobs-agent-" + suffix, OrganizationID: org, SiteID: "jobs-old-" + suffix}
	moved := tenancy.Agent{ID: old.ID, OrganizationID: org, SiteID: "jobs-new-" + suffix}
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Jobs test')`, org); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, table := range []string{"jobs", "hosts", "agents", "sites", "organizations"} {
			column := "organization_id"
			if table == "organizations" {
				column = "id"
			}
			if _, err := store.pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE "+column+"=$1", org); err != nil {
				t.Errorf("clean up %s: %v", table, err)
			}
		}
	}()
	for _, agent := range []tenancy.Agent{old, moved} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1, $2, $1, $1)`, agent.SiteID, agent.OrganizationID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO agents (id, organization_id, site_id) VALUES ($1, $2, $3)`, old.ID, old.OrganizationID, old.SiteID); err != nil {
		t.Fatal(err)
	}
	hosts := inventory.NewService(store)
	facts := inventory.Facts{Hostname: "edge", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}
	if err := hosts.Report(ctx, old, facts); err != nil {
		t.Fatal(err)
	}
	service, err := jobs.NewService(store, jobs.WithHostScopeChecker(hosts.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	request := jobs.CreateRequest{Action: jobs.ActionHostReboot, ApprovedBy: "ops", Reason: "maintenance"}
	oldJob, err := service.Create(ctx, old.Scope(), old.ID, request)
	if err != nil {
		t.Fatalf("create old job: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE agents SET site_id=$3 WHERE id=$1 AND organization_id=$2`, old.ID, org, moved.SiteID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE hosts SET site_id=$3 WHERE agent_id=$1 AND organization_id=$2`, old.ID, org, moved.SiteID); err != nil {
		t.Fatal(err)
	}
	if listed, err := service.List(ctx, moved.Scope(), moved.ID, 10); err != nil || len(listed) != 0 {
		t.Fatalf("moved scope listed old job: %#v %v", listed, err)
	}
	if events, err := service.Events(ctx, moved.Scope(), moved.ID, oldJob.ID); !errors.Is(err, jobs.ErrJobNotFound) || events != nil {
		t.Fatalf("moved scope read old events: %#v %v", events, err)
	}
	if claimed, err := service.ClaimNext(ctx, moved); err != nil || claimed != nil {
		t.Fatalf("moved scope claimed old job: %#v %v", claimed, err)
	}
	if claimed, err := service.ClaimNext(ctx, old); err != nil || claimed == nil || claimed.ID != oldJob.ID {
		t.Fatalf("old scope claim: %#v %v", claimed, err)
	}
	if _, err := service.Report(ctx, old, oldJob.ID, jobs.EventRequest{Sequence: 2, Type: jobs.EventSucceeded, Message: "rebooted"}); err != nil {
		t.Fatalf("old scope report: %v", err)
	}
	newJob, err := service.Create(ctx, moved.Scope(), moved.ID, request)
	if err != nil {
		t.Fatalf("create moved job: %v", err)
	}
	if listed, err := service.List(ctx, moved.Scope(), moved.ID, 10); err != nil || len(listed) != 1 || listed[0].ID != newJob.ID || listed[0].SiteID != moved.SiteID {
		t.Fatalf("moved scope jobs: %#v %v", listed, err)
	}
	if _, err := service.Create(ctx, old.Scope(), old.ID, request); !errors.Is(err, jobs.ErrJobNotFound) {
		t.Fatalf("stale host scope created job: %v", err)
	}
}

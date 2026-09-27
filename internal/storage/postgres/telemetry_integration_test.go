package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresFleetAverageCountsOnlyRecentSamples(t *testing.T) {
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
	orgID := "fleet-avg-org-" + suffix
	siteID := "fleet-avg-site-" + suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Fleet average test')`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1,$2,'Fleet avg site','fleet-avg-site')`, siteID, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		store.pool.Exec(cleanupCtx, "DELETE FROM telemetry_samples WHERE organization_id=$1", orgID)
		store.pool.Exec(cleanupCtx, "DELETE FROM hosts WHERE organization_id=$1", orgID)
		store.pool.Exec(cleanupCtx, "DELETE FROM agents WHERE organization_id=$1", orgID)
		store.pool.Exec(cleanupCtx, "DELETE FROM sites WHERE id=$1", siteID)
		store.pool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id=$1", orgID)
	}()

	scope := tenancy.Scope{OrganizationID: orgID, SiteID: siteID}
	now := time.Now().UTC()

	// Insert agents first (required for hosts foreign key constraint)
	if _, err := store.pool.Exec(ctx, `INSERT INTO agents (id, organization_id, site_id, enrolled_at) VALUES ($1, $2, $3, $4)`, "fresh-agent", orgID, siteID, now); err != nil {
		t.Fatalf("insert fresh agent: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO agents (id, organization_id, site_id, enrolled_at) VALUES ($1, $2, $3, $4)`, "stale-agent", orgID, siteID, now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("insert stale agent: %v", err)
	}

	// Insert hosts next (required for telemetry_samples foreign key constraint)
	if _, err := store.pool.Exec(ctx, `INSERT INTO hosts (agent_id, hostname, os_family, os_name, os_version, architecture, kernel_version, cpu_cores, memory_bytes, ip_addresses, agent_version, organization_id, site_id, first_seen_at, last_seen_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`, "fresh-agent", "test-host-1", "linux", "Ubuntu", "22.04", "amd64", "5.15", 4, 8589934592, []string{"10.0.0.1"}, "1.0.0", orgID, siteID, now, now); err != nil {
		t.Fatalf("insert fresh host: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO hosts (agent_id, hostname, os_family, os_name, os_version, architecture, kernel_version, cpu_cores, memory_bytes, ip_addresses, agent_version, organization_id, site_id, first_seen_at, last_seen_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`, "stale-agent", "test-host-2", "linux", "Ubuntu", "22.04", "amd64", "5.15", 4, 8589934592, []string{"10.0.0.2"}, "1.0.0", orgID, siteID, now.Add(-10*time.Minute), now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("insert stale host: %v", err)
	}

	if err := store.Append(ctx, telemetry.Sample{OrganizationID: orgID, SiteID: siteID, AgentID: "fresh-agent", RecordedAt: now, CPUPercent: 60, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatalf("append fresh sample: %v", err)
	}
	if err := store.Append(ctx, telemetry.Sample{OrganizationID: orgID, SiteID: siteID, AgentID: "stale-agent", RecordedAt: now.Add(-10 * time.Minute), CPUPercent: 100, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatalf("append stale sample: %v", err)
	}

	average, err := store.FleetAverage(ctx, scope, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("fleet average: %v", err)
	}
	if average.DeviceCount != 1 || average.AverageCPUPercent != 60 {
		t.Fatalf("expected only the fresh agent counted (60%%, 1 device), got %#v", average)
	}
}

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
	"github.com/jackc/pgx/v5"
)

func TestInventoryUpsertLocksCurrentAssignment(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	host := inventory.Host{AgentID: "agent-a", OrganizationID: "org-a", SiteID: "site-a", Hostname: "edge", OSFamily: "linux", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, FirstSeenAt: now, LastSeenAt: now}
	failure := errors.New("database failure")
	for _, scenario := range []string{"success", "stale assignment", "lock failure", "write failure", "commit failure"} {
		t.Run(scenario, func(t *testing.T) {
			lock := enrollmentSQLStep{sql: `SELECT id FROM agents WHERE id = $1 AND organization_id = $2 AND site_id = $3 FOR UPDATE`, args: []any{"agent-a", "org-a", "site-a"}, values: []any{"agent-a"}}
			wantErr := error(nil)
			if scenario == "stale assignment" {
				lock.err = pgx.ErrNoRows
				wantErr = tenancy.ErrNotFound
			}
			if scenario == "lock failure" {
				lock.err = failure
				wantErr = failure
			}
			steps := []enrollmentSQLStep{{sql: "BEGIN"}, lock}
			if wantErr == nil {
				write := enrollmentSQLStep{sql: `INSERT INTO hosts (
					agent_id, organization_id, site_id, hostname, os_family, os_name, os_version, architecture,
					kernel_version, cpu_cores, memory_bytes, ip_addresses, agent_version, first_seen_at, last_seen_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
				ON CONFLICT (agent_id) DO UPDATE SET
					organization_id = EXCLUDED.organization_id, site_id = EXCLUDED.site_id,
					hostname = EXCLUDED.hostname, os_family = EXCLUDED.os_family,
					os_name = EXCLUDED.os_name, os_version = EXCLUDED.os_version,
					architecture = EXCLUDED.architecture, kernel_version = EXCLUDED.kernel_version,
					cpu_cores = EXCLUDED.cpu_cores, memory_bytes = EXCLUDED.memory_bytes,
					ip_addresses = EXCLUDED.ip_addresses, agent_version = EXCLUDED.agent_version,
					last_seen_at = EXCLUDED.last_seen_at
				WHERE hosts.agent_id = $1 AND hosts.organization_id = $2 AND hosts.site_id = $3`,
					args: []any{"agent-a", "org-a", "site-a", "edge", "linux", "", "", "amd64", "", 2, int64(1024), []string(nil), "", now, now}}
				if scenario == "write failure" {
					write.err = failure
					wantErr = failure
				}
				steps = append(steps, write)
			}
			if wantErr == nil {
				commit := enrollmentSQLStep{sql: "COMMIT"}
				if scenario == "commit failure" {
					commit.err = failure
					wantErr = failure
				}
				steps = append(steps, commit)
			}
			steps = append(steps, enrollmentSQLStep{sql: "ROLLBACK"})
			if err := upsertHost(t.Context(), newEnrollmentSQLDB(t, steps...), host); !errors.Is(err, wantErr) {
				t.Fatalf("err=%v, want %v", err, wantErr)
			}
		})
	}
}

func TestInventoryStoreRejectsInvalidScopeBeforeDatabaseAccess(t *testing.T) {
	store := &Store{}
	if err := store.Upsert(t.Context(), inventory.Host{AgentID: "agent-a"}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := store.List(t.Context(), tenancy.Scope{}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("list: %v", err)
	}
}

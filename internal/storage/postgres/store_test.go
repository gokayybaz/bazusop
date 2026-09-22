package postgres

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/logstream"
)

func TestStorageMigrationsAreEmbeddedInOrder(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if len(entries) != 23 {
		t.Fatalf("expected twenty-three storage migrations, got %d", len(entries))
	}
	if entries[0].Name() != "001_hosts.sql" || entries[22].Name() != "023_activity_events.sql" {
		t.Fatalf("unexpected migration range: %s through %s", entries[0].Name(), entries[len(entries)-1].Name())
	}
}

func TestOrganizationSiteMigrationBackfillsEveryScopedTable(t *testing.T) {
	t.Parallel()
	migration, err := migrationFiles.ReadFile("migrations/015_organization_sites.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(migration)
	for _, required := range []string{
		"org_default", "site_default", "CREATE TABLE IF NOT EXISTS organizations",
		"CREATE TABLE IF NOT EXISTS sites", "CREATE TABLE IF NOT EXISTS agents",
		"ALTER TABLE hosts", "ALTER TABLE enrollment_tokens",
		"ALTER TABLE telemetry_samples", "ALTER TABLE services", "ALTER TABLE log_entries",
		"ALTER TABLE jobs", "ALTER TABLE job_events", "ALTER TABLE alert_rules",
		"ALTER TABLE maintenance_windows", "ALTER TABLE alert_incidents",
		"ALTER TABLE alert_events", "ALTER TABLE cloud_accounts", "ALTER TABLE cloud_instances",
		"SET NOT NULL", "FOREIGN KEY (organization_id, site_id)",
		"ADD COLUMN IF NOT EXISTS", "consumed_by_agent_id", "REFERENCES agents(id)",
		"PRIMARY KEY (organization_id, site_id, agent_id, recorded_at)",
		"UNIQUE (organization_id, site_id, provider, external_id)",
		"site scope backfill incomplete",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("site migration must contain %q", required)
		}
	}
}

func TestEnrollmentMigrationDefinesSingletonCAAndOneTimeTokens(t *testing.T) {
	t.Parallel()
	migration, err := migrationFiles.ReadFile("migrations/014_enrollment_state.sql")
	if err != nil {
		t.Fatalf("read enrollment migration: %v", err)
	}
	contents := string(migration)
	for _, required := range []string{"enrollment_authority", "enrollment_tokens", "token_hash", "consumed_at", "revoked_at", "CHECK (singleton = TRUE)"} {
		if !strings.Contains(contents, required) {
			t.Errorf("enrollment migration must contain %q", required)
		}
	}
}

func TestLogNotificationsStayBelowPostgresPayloadLimit(t *testing.T) {
	t.Parallel()
	entries := make([]logstream.Entry, 1000)
	for index := range entries {
		entries[index] = logstream.Entry{OrganizationID: strings.Repeat("o", 128), SiteID: strings.Repeat("s", 128), ID: strings.Repeat("a", 32), OccurredAt: time.Date(2026, 9, 13, 10, 0, 0, index*1000, time.UTC), Message: strings.Repeat("x", 64*1024)}
	}

	payloads, err := encodeLogNotificationBatches(entries)
	if err != nil {
		t.Fatalf("encode notifications: %v", err)
	}
	decoded := 0
	for _, payload := range payloads {
		if len(payload) >= postgresNotifyPayloadLimit {
			t.Fatalf("notification payload is %d bytes", len(payload))
		}
		var ids []struct {
			OrganizationID string    `json:"organization_id"`
			SiteID         string    `json:"site_id"`
			ID             string    `json:"id"`
			OccurredAt     time.Time `json:"occurred_at"`
		}
		if err := json.Unmarshal([]byte(payload), &ids); err != nil {
			t.Fatalf("decode notification: %v", err)
		}
		for _, identity := range ids {
			if identity.OrganizationID != entries[decoded].OrganizationID || identity.SiteID != entries[decoded].SiteID || identity.ID != entries[decoded].ID || !identity.OccurredAt.Equal(entries[decoded].OccurredAt) {
				t.Fatalf("notification lost scoped identity: %#v", identity)
			}
			decoded++
		}
		if strings.Contains(payload, strings.Repeat("x", 32)) {
			t.Fatal("notification must not contain log message content")
		}
	}
	if decoded != len(entries) {
		t.Fatalf("decoded %d ids, want %d", decoded, len(entries))
	}
}

func TestRetentionPolicyConfiguration(t *testing.T) {
	t.Parallel()
	store := &Store{telemetryRetentionDays: defaultTelemetryRetentionDays, logRetentionDays: defaultLogRetentionDays}
	WithRetention(90, 21)(store)

	policies := store.retentionPolicies()
	if len(policies) != 2 {
		t.Fatalf("expected two retention policies, got %d", len(policies))
	}
	if policies[0].table != "telemetry_samples" || policies[0].days != 90 {
		t.Fatalf("unexpected telemetry policy: %#v", policies[0])
	}
	if policies[1].table != "log_entries" || policies[1].days != 21 {
		t.Fatalf("unexpected log policy: %#v", policies[1])
	}
}

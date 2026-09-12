package postgres

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/gokayybaz/bazusop/internal/logstream"
)

func TestStorageMigrationsAreEmbeddedInOrder(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if len(entries) != 14 {
		t.Fatalf("expected fourteen storage migrations, got %d", len(entries))
	}
	if entries[0].Name() != "001_hosts.sql" || entries[13].Name() != "014_enrollment_state.sql" {
		t.Fatalf("unexpected migration range: %s through %s", entries[0].Name(), entries[13].Name())
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
		entries[index] = logstream.Entry{ID: strings.Repeat("a", 28) + string(rune('A'+index%26)), Message: strings.Repeat("x", 64*1024)}
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
		var ids []string
		if err := json.Unmarshal([]byte(payload), &ids); err != nil {
			t.Fatalf("decode notification: %v", err)
		}
		decoded += len(ids)
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

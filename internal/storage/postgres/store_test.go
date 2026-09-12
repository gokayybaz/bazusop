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
	if len(entries) != 13 {
		t.Fatalf("expected thirteen storage migrations, got %d", len(entries))
	}
	if entries[0].Name() != "001_hosts.sql" || entries[12].Name() != "013_cloud_inventory.sql" {
		t.Fatalf("unexpected migration range: %s through %s", entries[0].Name(), entries[3].Name())
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

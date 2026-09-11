package postgres

import (
	"io/fs"
	"testing"
)

func TestInventoryMigrationsAreEmbeddedInOrder(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected two inventory migrations, got %d", len(entries))
	}
	if entries[0].Name() != "001_hosts.sql" || entries[1].Name() != "002_hosts_last_seen.sql" {
		t.Fatalf("unexpected migration order: %s, %s", entries[0].Name(), entries[1].Name())
	}
}

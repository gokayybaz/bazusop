package postgres

import (
	"io/fs"
	"testing"
)

func TestStorageMigrationsAreEmbeddedInOrder(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("expected six storage migrations, got %d", len(entries))
	}
	if entries[0].Name() != "001_hosts.sql" || entries[5].Name() != "006_services_lookup.sql" {
		t.Fatalf("unexpected migration range: %s through %s", entries[0].Name(), entries[3].Name())
	}
}

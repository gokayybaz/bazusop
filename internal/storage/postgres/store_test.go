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
	if len(entries) != 13 {
		t.Fatalf("expected thirteen storage migrations, got %d", len(entries))
	}
	if entries[0].Name() != "001_hosts.sql" || entries[12].Name() != "013_cloud_inventory.sql" {
		t.Fatalf("unexpected migration range: %s through %s", entries[0].Name(), entries[3].Name())
	}
}

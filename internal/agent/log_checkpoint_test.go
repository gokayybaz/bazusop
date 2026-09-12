package agent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFileLogCheckpointStoreUsesProtectedAtomicState(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	store := NewFileLogCheckpointStore(directory)
	cursor := time.Date(2026, 9, 12, 12, 0, 0, 123, time.UTC)
	if err := store.Save(cursor); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || !loaded.Equal(cursor) {
		t.Fatalf("load checkpoint: %s %v", loaded, err)
	}
	if mode := fileMode(t, filepath.Join(directory, logCheckpointFile)); mode.Perm() != 0o600 {
		t.Fatalf("checkpoint must be 0600, got %o", mode.Perm())
	}
}

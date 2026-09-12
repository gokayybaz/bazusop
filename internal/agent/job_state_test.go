package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gokayybaz/bazusop/internal/jobs"
)

func TestFileJobStateStorePersistsProtectedExecutionState(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	store := NewFileJobStateStore(directory)
	state := jobExecutionState{
		Job: jobs.Job{ID: "0123456789abcdef0123456789abcdef", AgentID: "agent-01"}, Phase: jobPhaseCompleted,
		Events: []jobs.EventRequest{{Sequence: 2, Type: jobs.EventSucceeded, Message: "done"}},
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}
	loaded, err := store.Load()
	if err != nil || loaded.Job.ID != state.Job.ID || len(loaded.Events) != 1 {
		t.Fatalf("load state: %#v %v", loaded, err)
	}
	if mode := fileMode(t, filepath.Join(directory, jobStateFile)); mode.Perm() != 0o600 {
		t.Fatalf("job state must be 0600, got %o", mode.Perm())
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("delete state: %v", err)
	}
	if _, err := store.Load(); !os.IsNotExist(err) {
		t.Fatalf("expected deleted state, got %v", err)
	}
}

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gokayybaz/bazusop/internal/jobs"
)

const jobStateFile = "job-state.json"

const (
	jobPhaseExecuting = "executing"
	jobPhaseCompleted = "completed"
)

type jobExecutionState struct {
	Job    jobs.Job            `json:"job"`
	Phase  string              `json:"phase"`
	Events []jobs.EventRequest `json:"events,omitempty"`
}

type JobStateStore interface {
	Load() (*jobExecutionState, error)
	Save(jobExecutionState) error
	Delete() error
}

type FileJobStateStore struct{ directory string }

func NewFileJobStateStore(directory string) *FileJobStateStore {
	return &FileJobStateStore{directory: directory}
}

func (store *FileJobStateStore) Load() (*jobExecutionState, error) {
	contents, err := os.ReadFile(filepath.Join(store.directory, jobStateFile))
	if err != nil {
		return nil, err
	}
	var state jobExecutionState
	if err := json.Unmarshal(contents, &state); err != nil {
		return nil, fmt.Errorf("decode job execution state: %w", err)
	}
	if state.Job.ID == "" || (state.Phase != jobPhaseExecuting && state.Phase != jobPhaseCompleted) {
		return nil, errors.New("stored job execution state is invalid")
	}
	return &state, nil
}

func (store *FileJobStateStore) Save(state jobExecutionState) error {
	if state.Job.ID == "" || (state.Phase != jobPhaseExecuting && state.Phase != jobPhaseCompleted) {
		return errors.New("job execution state is invalid")
	}
	if err := os.MkdirAll(store.directory, 0o700); err != nil {
		return fmt.Errorf("create agent state directory: %w", err)
	}
	if err := protectStateDirectory(store.directory); err != nil {
		return fmt.Errorf("protect agent state directory: %w", err)
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode job execution state: %w", err)
	}
	return atomicWrite(filepath.Join(store.directory, jobStateFile), append(payload, '\n'), 0o600)
}

func (store *FileJobStateStore) Delete() error {
	err := os.Remove(filepath.Join(store.directory, jobStateFile))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove completed job execution state: %w", err)
	}
	return nil
}

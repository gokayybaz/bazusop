package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const logCheckpointFile = "log-checkpoint.json"

type LogCheckpointStore interface {
	Load() (time.Time, error)
	Save(time.Time) error
}

type FileLogCheckpointStore struct{ directory string }

func NewFileLogCheckpointStore(directory string) *FileLogCheckpointStore {
	return &FileLogCheckpointStore{directory: directory}
}

func (store *FileLogCheckpointStore) Load() (time.Time, error) {
	contents, err := os.ReadFile(filepath.Join(store.directory, logCheckpointFile))
	if err != nil {
		return time.Time{}, err
	}
	var checkpoint struct {
		Cursor time.Time `json:"cursor"`
	}
	if err := json.Unmarshal(contents, &checkpoint); err != nil {
		return time.Time{}, fmt.Errorf("decode log checkpoint: %w", err)
	}
	if checkpoint.Cursor.IsZero() {
		return time.Time{}, errors.New("stored log checkpoint is invalid")
	}
	return checkpoint.Cursor.UTC(), nil
}

func (store *FileLogCheckpointStore) Save(cursor time.Time) error {
	if cursor.IsZero() {
		return errors.New("log checkpoint cursor is invalid")
	}
	if err := os.MkdirAll(store.directory, 0o700); err != nil {
		return fmt.Errorf("create agent state directory: %w", err)
	}
	if err := protectStateDirectory(store.directory); err != nil {
		return fmt.Errorf("protect agent state directory: %w", err)
	}
	payload, err := json.MarshalIndent(struct {
		Cursor time.Time `json:"cursor"`
	}{cursor.UTC()}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode log checkpoint: %w", err)
	}
	return atomicWrite(filepath.Join(store.directory, logCheckpointFile), append(payload, '\n'), 0o600)
}

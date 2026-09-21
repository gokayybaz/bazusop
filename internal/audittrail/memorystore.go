package audittrail

import (
	"context"
	"sync"
)

// MemoryStore is for local development and tests only; it holds no
// durability guarantee and is never used against a real deployment (the
// hub only constructs it when DATABASE_URL is unset, matching every other
// domain's memory-mode fallback).
type MemoryStore struct {
	mu     sync.Mutex
	events []Event
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (store *MemoryStore) Record(_ context.Context, event Event) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.events = append(store.events, event)
	return nil
}

// Events returns a copy of every recorded event, oldest first. Test-only.
func (store *MemoryStore) Events() []Event {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]Event(nil), store.events...)
}

package audittrail

import (
	"context"
	"sort"
	"sync"

	"github.com/gokayybaz/bazusop/internal/tenancy"
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

func (store *MemoryStore) ListAuditTrail(_ context.Context, scope tenancy.Scope, filter Filter, limit int) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	matched := make([]Event, 0, len(store.events))
	for _, event := range store.events {
		if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
			continue
		}
		if filter.ActorType != "" && event.ActorType != filter.ActorType {
			continue
		}
		if filter.ResourceType != "" && event.ResourceType != filter.ResourceType {
			continue
		}
		if filter.Outcome != "" && event.Outcome != filter.Outcome {
			continue
		}
		if !filter.Since.IsZero() && event.OccurredAt.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && event.OccurredAt.After(filter.Until) {
			continue
		}
		matched = append(matched, event)
	}
	sort.Slice(matched, func(left, right int) bool { return matched[left].OccurredAt.After(matched[right].OccurredAt) })
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

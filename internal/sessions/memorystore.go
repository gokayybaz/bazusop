package sessions

import (
	"context"
	"sync"
	"time"
)

type sessionRecord struct {
	session   Session
	tokenHash string
}

// MemoryStore is for local development and tests only, matching every
// other domain's memory-mode fallback.
type MemoryStore struct {
	mu          sync.Mutex
	byTokenHash map[string]sessionRecord
	byID        map[string]string // session id -> token hash
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byTokenHash: make(map[string]sessionRecord),
		byID:        make(map[string]string),
	}
}

func (store *MemoryStore) CreateSession(_ context.Context, session Session, tokenHash string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.byTokenHash[tokenHash] = sessionRecord{session: session, tokenHash: tokenHash}
	store.byID[session.ID] = tokenHash
	return nil
}

func (store *MemoryStore) SessionByTokenHash(_ context.Context, tokenHash string) (Session, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.byTokenHash[tokenHash]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return record.session, nil
}

func (store *MemoryStore) Touch(_ context.Context, sessionID string, lastSeenAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	tokenHash, ok := store.byID[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	record := store.byTokenHash[tokenHash]
	record.session.LastSeenAt = lastSeenAt
	store.byTokenHash[tokenHash] = record
	return nil
}

func (store *MemoryStore) RevokeSession(_ context.Context, sessionID string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	tokenHash, ok := store.byID[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	record := store.byTokenHash[tokenHash]
	revokedAt := at
	record.session.RevokedAt = &revokedAt
	store.byTokenHash[tokenHash] = record
	return nil
}

func (store *MemoryStore) RevokeAllForUser(_ context.Context, userID string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	revokedAt := at
	for tokenHash, record := range store.byTokenHash {
		if record.session.UserID == userID && record.session.RevokedAt == nil {
			record.session.RevokedAt = &revokedAt
			store.byTokenHash[tokenHash] = record
		}
	}
	return nil
}

func (store *MemoryStore) RevokeAllForOrganization(_ context.Context, organizationID string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	revokedAt := at
	for tokenHash, record := range store.byTokenHash {
		if record.session.OrganizationID == organizationID && record.session.RevokedAt == nil {
			record.session.RevokedAt = &revokedAt
			store.byTokenHash[tokenHash] = record
		}
	}
	return nil
}

package serviceaccounts

import (
	"context"
	"sync"
	"time"
)

type tokenRecord struct {
	token      Token
	secretHash string
}

// MemoryStore is for local development and tests only, matching every
// other domain's memory-mode fallback.
type MemoryStore struct {
	mu       sync.Mutex
	accounts map[string]ServiceAccount
	tokens   map[string]tokenRecord // token id -> record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{accounts: make(map[string]ServiceAccount), tokens: make(map[string]tokenRecord)}
}

func (store *MemoryStore) CreateAccount(_ context.Context, account ServiceAccount) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.accounts[account.ID] = account
	return nil
}

func (store *MemoryStore) AccountByID(_ context.Context, id string) (ServiceAccount, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	account, ok := store.accounts[id]
	if !ok {
		return ServiceAccount{}, ErrAccountNotFound
	}
	return account, nil
}

func (store *MemoryStore) DisableAccount(_ context.Context, id string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	account, ok := store.accounts[id]
	if !ok {
		return ErrAccountNotFound
	}
	disabledAt := at
	account.DisabledAt = &disabledAt
	store.accounts[id] = account
	return nil
}

func (store *MemoryStore) AccountsForSite(_ context.Context, siteID string) ([]ServiceAccount, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var accounts []ServiceAccount
	for _, account := range store.accounts {
		if account.SiteID == siteID {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (store *MemoryStore) CreateToken(_ context.Context, token Token, secretHash string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.tokens[token.ID] = tokenRecord{token: token, secretHash: secretHash}
	return nil
}

func (store *MemoryStore) TokenByID(_ context.Context, id string) (Token, string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.tokens[id]
	if !ok {
		return Token{}, "", ErrTokenNotFound
	}
	return record.token, record.secretHash, nil
}

func (store *MemoryStore) RevokeToken(_ context.Context, id string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.tokens[id]
	if !ok {
		return ErrTokenNotFound
	}
	revokedAt := at
	record.token.RevokedAt = &revokedAt
	store.tokens[id] = record
	return nil
}

func (store *MemoryStore) RevokeActiveTokensForAccount(_ context.Context, accountID string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	revokedAt := at
	for id, record := range store.tokens {
		if record.token.ServiceAccountID == accountID && record.token.RevokedAt == nil {
			record.token.RevokedAt = &revokedAt
			store.tokens[id] = record
		}
	}
	return nil
}

func (store *MemoryStore) TouchToken(_ context.Context, tokenID string, at time.Time, sourceIP string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.tokens[tokenID]
	if !ok {
		return ErrTokenNotFound
	}
	lastUsedAt := at
	record.token.LastUsedAt = &lastUsedAt
	record.token.LastUsedIP = sourceIP
	store.tokens[tokenID] = record
	return nil
}

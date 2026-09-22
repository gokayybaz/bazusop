package identity

import (
	"context"
	"sync"
	"time"
)

type inviteRecord struct {
	invite    Invite
	tokenHash string
}

// MemoryStore is for local development and tests only, matching every
// other domain's memory-mode fallback.
type MemoryStore struct {
	mu             sync.Mutex
	bootstrapped   bool
	usersByID      map[string]User
	usersByEmail   map[string]string            // "org_id\x00email" -> user id
	usersByOIDC    map[string]string            // "org_id\x00issuer\x00subject" -> user id
	invites        map[string]inviteRecord      // token hash -> record
	recoveryHashes map[string]map[string]bool   // user id -> code hash -> consumed
	oidcConfigs    map[string]OIDCConfiguration // org id -> config
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		usersByID:      make(map[string]User),
		usersByEmail:   make(map[string]string),
		usersByOIDC:    make(map[string]string),
		invites:        make(map[string]inviteRecord),
		recoveryHashes: make(map[string]map[string]bool),
		oidcConfigs:    make(map[string]OIDCConfiguration),
	}
}

func emailKey(organizationID, email string) string { return organizationID + "\x00" + email }

func oidcKey(organizationID, issuer, subject string) string {
	return organizationID + "\x00" + issuer + "\x00" + subject
}

func (store *MemoryStore) IsBootstrapped(_ context.Context) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.bootstrapped, nil
}

func (store *MemoryStore) CompleteBootstrap(_ context.Context, user User) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.bootstrapped {
		return ErrAlreadyBootstrapped
	}
	store.bootstrapped = true
	store.usersByID[user.ID] = user
	store.usersByEmail[emailKey(user.OrganizationID, user.Email)] = user.ID
	return nil
}

func (store *MemoryStore) CreateUser(_ context.Context, user User) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.usersByID[user.ID] = user
	store.usersByEmail[emailKey(user.OrganizationID, user.Email)] = user.ID
	if user.OIDCSubject != "" {
		store.usersByOIDC[oidcKey(user.OrganizationID, user.OIDCIssuer, user.OIDCSubject)] = user.ID
	}
	return nil
}

func (store *MemoryStore) UserByEmail(_ context.Context, organizationID, email string) (User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	id, ok := store.usersByEmail[emailKey(organizationID, email)]
	if !ok {
		return User{}, ErrInvalidCredentials
	}
	return store.usersByID[id], nil
}

func (store *MemoryStore) UserByOIDCIdentity(_ context.Context, organizationID, issuer, subject string) (User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	id, ok := store.usersByOIDC[oidcKey(organizationID, issuer, subject)]
	if !ok {
		return User{}, ErrInvalidCredentials
	}
	return store.usersByID[id], nil
}

func (store *MemoryStore) UserByID(_ context.Context, id string) (User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	user, ok := store.usersByID[id]
	if !ok {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (store *MemoryStore) UpdateUser(_ context.Context, user User) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.usersByID[user.ID] = user
	return nil
}

func (store *MemoryStore) SaveInvite(_ context.Context, invite Invite, tokenHash string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.invites[tokenHash] = inviteRecord{invite: invite, tokenHash: tokenHash}
	return nil
}

func (store *MemoryStore) InviteByTokenHash(_ context.Context, tokenHash string) (Invite, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.invites[tokenHash]
	if !ok {
		return Invite{}, ErrInviteNotFound
	}
	return record.invite, nil
}

func (store *MemoryStore) ConsumeInvite(_ context.Context, tokenHash string, consumedByUserID string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.invites[tokenHash]
	if !ok {
		return ErrInviteNotFound
	}
	consumedAt := at
	record.invite.ConsumedAt = &consumedAt
	store.invites[tokenHash] = record
	return nil
}

// InviteByEmail returns the most recently created invite for email,
// regardless of status — LoginOrLinkOIDCUser applies the same
// expiry/consumed/revoked/identity-type checks ConsumeInvite already
// applies for local invites.
func (store *MemoryStore) InviteByEmail(_ context.Context, organizationID, email string) (Invite, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var found *Invite
	for _, record := range store.invites {
		if record.invite.OrganizationID != organizationID || record.invite.Email != email {
			continue
		}
		if found == nil || record.invite.CreatedAt.After(found.CreatedAt) {
			invite := record.invite
			found = &invite
		}
	}
	if found == nil {
		return Invite{}, ErrInviteNotFound
	}
	return *found, nil
}

func (store *MemoryStore) ConsumeInviteByID(_ context.Context, inviteID string, _ string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for tokenHash, record := range store.invites {
		if record.invite.ID == inviteID {
			consumedAt := at
			record.invite.ConsumedAt = &consumedAt
			store.invites[tokenHash] = record
			return nil
		}
	}
	return ErrInviteNotFound
}

func (store *MemoryStore) SaveOIDCConfiguration(_ context.Context, config OIDCConfiguration) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.oidcConfigs[config.OrganizationID] = config
	return nil
}

func (store *MemoryStore) OIDCConfigurationByOrganization(_ context.Context, organizationID string) (OIDCConfiguration, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	config, ok := store.oidcConfigs[organizationID]
	if !ok {
		return OIDCConfiguration{}, ErrOIDCNotConfigured
	}
	return config, nil
}

func (store *MemoryStore) SaveRecoveryCodes(_ context.Context, userID string, codeHashes []string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	hashes := make(map[string]bool, len(codeHashes))
	for _, hash := range codeHashes {
		hashes[hash] = false
	}
	store.recoveryHashes[userID] = hashes
	return nil
}

func (store *MemoryStore) ConsumeRecoveryCode(_ context.Context, userID, codeHash string) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	hashes, ok := store.recoveryHashes[userID]
	if !ok {
		return false, nil
	}
	consumed, exists := hashes[codeHash]
	if !exists || consumed {
		return false, nil
	}
	hashes[codeHash] = true
	return true, nil
}

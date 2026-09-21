package authorization

import (
	"context"
	"sync"
)

type MemoryStore struct {
	mu          sync.Mutex
	memberships map[string]SiteMembership // "user_id\x00site_id" -> membership
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{memberships: make(map[string]SiteMembership)}
}

func membershipKey(userID, siteID string) string { return userID + "\x00" + siteID }

func (store *MemoryStore) AssignRole(_ context.Context, membership SiteMembership) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := membershipKey(membership.UserID, membership.SiteID)
	if existing, ok := store.memberships[key]; ok {
		membership.ID = existing.ID
		membership.CreatedAt = existing.CreatedAt
	}
	store.memberships[key] = membership
	return nil
}

func (store *MemoryStore) RevokeRole(_ context.Context, userID, siteID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.memberships, membershipKey(userID, siteID))
	return nil
}

func (store *MemoryStore) RoleForUserAtSite(_ context.Context, userID, siteID string) (SiteRole, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	membership, ok := store.memberships[membershipKey(userID, siteID)]
	if !ok {
		return "", ErrMembershipNotFound
	}
	return membership.Role, nil
}

func (store *MemoryStore) MembershipsForUser(_ context.Context, userID string) ([]SiteMembership, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var memberships []SiteMembership
	for _, membership := range store.memberships {
		if membership.UserID == userID {
			memberships = append(memberships, membership)
		}
	}
	return memberships, nil
}

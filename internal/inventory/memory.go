package inventory

import (
	"context"
	"sync"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

type MemoryStore struct {
	mu    sync.RWMutex
	hosts map[string]Host
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{hosts: make(map[string]Host)}
}

func (store *MemoryStore) Upsert(_ context.Context, host Host) error {
	if err := host.ValidateStored(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	key := host.OrganizationID + "\x00" + host.SiteID + "\x00" + host.AgentID
	if existing, exists := store.hosts[key]; exists {
		host.FirstSeenAt = existing.FirstSeenAt
	}
	host.IPAddresses = append([]string(nil), host.IPAddresses...)
	store.hosts[key] = host
	return nil
}

func (store *MemoryStore) List(_ context.Context, scope tenancy.Scope) ([]Host, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()

	hosts := make([]Host, 0, len(store.hosts))
	for _, host := range store.hosts {
		if host.Scope() != scope {
			continue
		}
		host.IPAddresses = append([]string(nil), host.IPAddresses...)
		hosts = append(hosts, host)
	}
	return hosts, nil
}

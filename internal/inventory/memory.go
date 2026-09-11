package inventory

import (
	"context"
	"sync"
)

type MemoryStore struct {
	mu    sync.RWMutex
	hosts map[string]Host
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{hosts: make(map[string]Host)}
}

func (store *MemoryStore) Upsert(_ context.Context, host Host) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	if existing, exists := store.hosts[host.AgentID]; exists {
		host.FirstSeenAt = existing.FirstSeenAt
	}
	host.IPAddresses = append([]string(nil), host.IPAddresses...)
	store.hosts[host.AgentID] = host
	return nil
}

func (store *MemoryStore) List(_ context.Context) ([]Host, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	hosts := make([]Host, 0, len(store.hosts))
	for _, host := range store.hosts {
		host.IPAddresses = append([]string(nil), host.IPAddresses...)
		hosts = append(hosts, host)
	}
	return hosts, nil
}

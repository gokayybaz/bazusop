package logstream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

type sharedTestStore struct {
	*MemoryStore
	mu          sync.RWMutex
	subscribers map[tenancy.Agent][]chan Entry
}

func newSharedTestStore() *sharedTestStore {
	return &sharedTestStore{MemoryStore: NewMemoryStore(), subscribers: make(map[tenancy.Agent][]chan Entry)}
}

func (store *sharedTestStore) AppendLogs(ctx context.Context, entries []Entry) ([]Entry, error) {
	stored, err := store.MemoryStore.AppendLogs(ctx, entries)
	if err != nil {
		return nil, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	for _, entry := range stored {
		for _, subscriber := range store.subscribers[tenancy.Agent{ID: entry.AgentID, OrganizationID: entry.OrganizationID, SiteID: entry.SiteID}] {
			subscriber <- entry
		}
	}
	return stored, nil
}

func TestIngestWithStableIDIsIdempotentForHistoryAndLiveStream(t *testing.T) {
	manager := NewService(NewMemoryStore())
	stream, err := manager.Subscribe(t.Context(), tenancy.DefaultScope(), "agent-01")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	batch := Batch{Entries: []Entry{{ID: "0123456789abcdef0123456789abcdef", OccurredAt: now, Collector: "journald", Source: "app.service", Severity: "info", Message: "started"}}}
	if err := manager.Ingest(t.Context(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, batch); err != nil {
		t.Fatal(err)
	}
	if err := manager.Ingest(t.Context(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, batch); err != nil {
		t.Fatalf("duplicate ingest: %v", err)
	}
	entries, err := manager.Search(t.Context(), Query{Scope: tenancy.DefaultScope(), AgentID: "agent-01", From: now.Add(-time.Second), To: now.Add(time.Second), Limit: 10})
	if err != nil || len(entries) != 1 || len(stream) != 1 {
		t.Fatalf("duplicate was stored or published: entries=%#v live=%d err=%v", entries, len(stream), err)
	}
}

func (store *sharedTestStore) SubscribeLogs(ctx context.Context, scope tenancy.Scope, agentID string) (<-chan Entry, error) {
	stream := make(chan Entry, 1)
	store.mu.Lock()
	store.subscribers[tenancy.Agent{ID: agentID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}] = append(store.subscribers[tenancy.Agent{ID: agentID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}], stream)
	store.mu.Unlock()
	return stream, nil
}

func TestIngestNormalizesAndSearchesBoundedLogs(t *testing.T) {
	t.Parallel()
	manager := NewService(NewMemoryStore())
	base := time.Date(2026, 9, 11, 4, 0, 0, 0, time.UTC)
	err := manager.Ingest(context.Background(), tenancy.Agent{ID: " agent-01 ", OrganizationID: "org_default", SiteID: "site_default"}, Batch{Entries: []Entry{
		{OccurredAt: base, Collector: "JOURNALD", Source: " nginx.service ", Severity: "WARNING", Message: " upstream timeout "},
		{OccurredAt: base.Add(time.Minute), Collector: "file", Source: "/var/log/nginx/error.log", Severity: "error", Message: "connection refused"},
		{OccurredAt: base.Add(2 * time.Minute), Collector: "windows_event", Source: "System", Severity: "info", Message: "service recovered"},
	}})
	if err != nil {
		t.Fatalf("ingest logs: %v", err)
	}

	entries, err := manager.Search(context.Background(), Query{Scope: tenancy.DefaultScope(),
		AgentID: "agent-01", From: base.Add(-time.Minute), To: base.Add(3 * time.Minute),
		Severity: SeverityError, Text: "refused", Limit: 50,
	})
	if err != nil {
		t.Fatalf("search logs: %v", err)
	}
	if len(entries) != 1 || entries[0].Collector != CollectorFile || entries[0].Message != "connection refused" {
		t.Fatalf("expected normalized matching log, got %#v", entries)
	}
	if entries[0].ID == "" {
		t.Fatal("expected generated log id")
	}

	entries, err = manager.Search(context.Background(), Query{Scope: tenancy.DefaultScope(), AgentID: "agent-01", From: base.Add(-time.Minute), To: base.Add(3 * time.Minute), Limit: 2})
	if err != nil || len(entries) != 2 || !entries[0].OccurredAt.Equal(base.Add(time.Minute)) || !entries[1].OccurredAt.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("expected latest two logs in chronological order, got %#v, %v", entries, err)
	}
}

func TestSubscribersReceiveOnlyTheirAgentLogs(t *testing.T) {
	t.Parallel()
	manager := NewService(NewMemoryStore())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := manager.Subscribe(ctx, tenancy.DefaultScope(), "agent-01")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	now := time.Now().UTC()
	if err := manager.Ingest(context.Background(), tenancy.Agent{ID: "agent-02", OrganizationID: "org_default", SiteID: "site_default"}, Batch{Entries: []Entry{{OccurredAt: now, Collector: "file", Source: "app.log", Severity: "info", Message: "ignore"}}}); err != nil {
		t.Fatalf("ingest other agent: %v", err)
	}
	if err := manager.Ingest(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, Batch{Entries: []Entry{{OccurredAt: now, Collector: "file", Source: "app.log", Severity: "info", Message: "deliver"}}}); err != nil {
		t.Fatalf("ingest subscribed agent: %v", err)
	}
	select {
	case entry := <-stream:
		if entry.Message != "deliver" || entry.AgentID != "agent-01" {
			t.Fatalf("unexpected streamed entry: %#v", entry)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live log")
	}
}

func TestServicesUseSharedStoreForCrossReplicaLiveLogs(t *testing.T) {
	t.Parallel()
	store := newSharedTestStore()
	ingestingReplica := NewService(store)
	streamingReplica := NewService(store)
	stream, err := streamingReplica.Subscribe(t.Context(), tenancy.DefaultScope(), "agent-01")
	if err != nil {
		t.Fatalf("subscribe through shared store: %v", err)
	}
	if err := ingestingReplica.Ingest(t.Context(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, Batch{Entries: []Entry{{
		OccurredAt: time.Now().UTC(), Collector: "file", Source: "app.log", Severity: "info", Message: "cross replica",
	}}}); err != nil {
		t.Fatalf("ingest through other replica: %v", err)
	}
	select {
	case entry := <-stream:
		if entry.Message != "cross replica" {
			t.Fatalf("unexpected shared entry: %#v", entry)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for cross-replica log")
	}
}

func TestMaximumBatchFansOutWithoutLossWithinLoadTarget(t *testing.T) {
	t.Parallel()
	manager := NewService(NewMemoryStore())
	const subscriberCount = 32
	streams := make([]<-chan Entry, 0, subscriberCount)
	for index := 0; index < subscriberCount; index++ {
		stream, err := manager.Subscribe(t.Context(), tenancy.DefaultScope(), "agent-load")
		if err != nil {
			t.Fatalf("subscribe %d: %v", index, err)
		}
		streams = append(streams, stream)
	}
	entries := make([]Entry, 1000)
	now := time.Now().UTC()
	for index := range entries {
		entries[index] = Entry{OccurredAt: now.Add(time.Duration(index) * time.Nanosecond), Collector: "file", Source: "load.log", Severity: "info", Message: "load target"}
	}
	started := time.Now()
	if err := manager.Ingest(t.Context(), tenancy.Agent{ID: "agent-load", OrganizationID: "org_default", SiteID: "site_default"}, Batch{Entries: entries}); err != nil {
		t.Fatalf("ingest maximum batch: %v", err)
	}
	elapsed := time.Since(started)
	t.Logf("fan-out: %d entries x %d subscribers in %s", len(entries), subscriberCount, elapsed)
	if elapsed > time.Second {
		t.Fatalf("fan-out took %s, target is at most 1s", elapsed)
	}
	for index, stream := range streams {
		if received := len(stream); received != len(entries) {
			t.Errorf("subscriber %d buffered %d entries, want %d", index, received, len(entries))
		}
	}
}

func TestIngestAndSearchRejectInvalidInputs(t *testing.T) {
	t.Parallel()
	manager := NewService(NewMemoryStore())
	if err := manager.Ingest(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, Batch{Entries: []Entry{{Collector: "syslog", Severity: "loud"}}}); !errors.Is(err, ErrInvalidLogs) {
		t.Fatalf("expected invalid batch, got %v", err)
	}
	if err := manager.Ingest(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, Batch{Entries: []Entry{{ID: "not-a-stable-id", OccurredAt: time.Now(), Collector: "file", Source: "app.log", Severity: "info", Message: "started"}}}); !errors.Is(err, ErrInvalidLogs) {
		t.Fatalf("expected invalid entry ID, got %v", err)
	}
	if _, err := manager.Search(context.Background(), Query{Scope: tenancy.DefaultScope(), AgentID: "agent-01", From: time.Now(), To: time.Now().Add(-time.Minute), Limit: 10}); !errors.Is(err, ErrInvalidLogs) {
		t.Fatalf("expected invalid query, got %v", err)
	}
}

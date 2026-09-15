package logstream

import (
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestLogsAndSubscribersAreIsolatedByStoredScope(t *testing.T) {
	manager := NewService(NewMemoryStore())
	agents := []tenancy.Agent{
		{ID: "agent-1", OrganizationID: "org-a", SiteID: "site-old"},
		{ID: "agent-1", OrganizationID: "org-a", SiteID: "site-new"},
		{ID: "agent-1", OrganizationID: "org-b", SiteID: "site-old"},
	}
	streams := make([]<-chan Entry, len(agents))
	for i, agent := range agents {
		var err error
		streams[i], err = manager.Subscribe(t.Context(), agent.Scope(), agent.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	for i, agent := range agents {
		batch := Batch{Entries: []Entry{{ID: "0123456789abcdef0123456789abcdef", AgentID: "forged", OrganizationID: "forged", SiteID: "forged", OccurredAt: at, Collector: "file", Source: "app.log", Severity: "info", Message: agent.OrganizationID + "/" + agent.SiteID}}}
		if err := manager.Ingest(t.Context(), agent, batch); err != nil {
			t.Fatal(err)
		}
		select {
		case entry := <-streams[i]:
			if entry.AgentID != agent.ID || entry.OrganizationID != agent.OrganizationID || entry.SiteID != agent.SiteID {
				t.Fatalf("wrong live scope: %#v", entry)
			}
		case <-time.After(time.Second):
			t.Fatal("matching subscriber received no entry")
		}
		for j, stream := range streams {
			select {
			case entry := <-stream:
				t.Fatalf("subscriber %d received extra/cross-scope entry: %#v", j, entry)
			default:
			}
		}
	}
	for _, agent := range agents {
		entries, err := manager.Search(t.Context(), Query{Scope: agent.Scope(), AgentID: agent.ID, From: at.Add(-time.Second), To: at.Add(time.Hour), Limit: 10})
		if err != nil || len(entries) != 1 || entries[0].Message != agent.OrganizationID+"/"+agent.SiteID {
			t.Fatalf("history crossed scope: %#v, %v", entries, err)
		}
	}
}

func TestLogsRejectMissingScope(t *testing.T) {
	manager := NewService(NewMemoryStore())
	if err := manager.Ingest(t.Context(), tenancy.Agent{ID: "agent-1"}, Batch{Entries: []Entry{{OccurredAt: time.Now(), Collector: "file", Source: "app.log", Severity: "info", Message: "message"}}}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("ingest missing scope: %v", err)
	}
	if _, err := manager.Search(t.Context(), Query{AgentID: "agent-1", From: time.Now(), To: time.Now().Add(time.Hour), Limit: 10}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("search missing scope: %v", err)
	}
	if _, err := manager.Subscribe(t.Context(), tenancy.Scope{}, "agent-1"); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("subscribe missing scope: %v", err)
	}
}

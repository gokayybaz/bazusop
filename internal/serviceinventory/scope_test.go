package serviceinventory

import (
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestServiceSnapshotsAreIsolatedByStoredScope(t *testing.T) {
	manager := NewService(NewMemoryStore())
	agents := []tenancy.Agent{
		{ID: "agent-1", OrganizationID: "org-a", SiteID: "site-old"},
		{ID: "agent-1", OrganizationID: "org-a", SiteID: "site-new"},
		{ID: "agent-1", OrganizationID: "org-b", SiteID: "site-old"},
	}
	names := []string{"old-service", "new-service", "other-service"}
	for i, agent := range agents {
		if err := manager.Report(t.Context(), agent, Snapshot{ObservedAt: time.Now(), Services: []Fact{{Name: names[i]}}}); err != nil {
			t.Fatal(err)
		}
	}
	for i, agent := range agents {
		services, err := manager.List(t.Context(), agent.Scope(), agent.ID, Filter{})
		if err != nil || len(services) != 1 || services[0].Name != names[i] || services[0].OrganizationID != agent.OrganizationID || services[0].SiteID != agent.SiteID {
			t.Fatalf("services crossed scope: %#v, %v", services, err)
		}
	}
	if err := manager.Report(t.Context(), agents[1], Snapshot{ObservedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	old, err := manager.List(t.Context(), agents[0].Scope(), agents[0].ID, Filter{})
	if err != nil || len(old) != 1 || old[0].Name != "old-service" {
		t.Fatalf("empty replacement cleared old site: %#v, %v", old, err)
	}
}

func TestServicesRejectMissingScope(t *testing.T) {
	manager := NewService(NewMemoryStore())
	if err := manager.Report(t.Context(), tenancy.Agent{ID: "agent-1"}, Snapshot{ObservedAt: time.Now()}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("report missing scope: %v", err)
	}
	if _, err := manager.List(t.Context(), tenancy.Scope{}, "agent-1", Filter{}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("list missing scope: %v", err)
	}
}

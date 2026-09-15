package serviceinventory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestReportNormalizesAndReplacesAgentServices(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	service := NewService(store)
	observedAt := time.Date(2026, 9, 11, 4, 0, 0, 0, time.FixedZone("TRT", 3*60*60))

	err := service.Report(context.Background(), tenancy.Agent{ID: " agent-01 ", OrganizationID: "org_default", SiteID: "site_default"}, Snapshot{
		ObservedAt: observedAt,
		Services: []Fact{
			{Name: " SSH.Service ", DisplayName: " OpenSSH Server ", State: "RUNNING", StartupType: "Automatic"},
			{Name: "queue-worker", State: "failed", StartupType: "enabled"},
		},
	})
	if err != nil {
		t.Fatalf("report services: %v", err)
	}

	services, err := service.List(context.Background(), tenancy.DefaultScope(), "agent-01", Filter{})
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("expected two services, got %#v", services)
	}
	if services[0].Name != "queue-worker" || services[0].State != StateFailed || services[0].StartupType != StartupAutomatic {
		t.Fatalf("expected normalized queue worker first, got %#v", services[0])
	}
	if services[1].Name != "ssh.service" || services[1].DisplayName != "OpenSSH Server" {
		t.Fatalf("expected normalized OpenSSH service, got %#v", services[1])
	}
	if !services[1].ObservedAt.Equal(observedAt.UTC()) {
		t.Fatalf("expected UTC observation time, got %s", services[1].ObservedAt)
	}

	if err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, Snapshot{
		ObservedAt: observedAt.Add(time.Minute),
		Services:   []Fact{{Name: "ssh.service", State: "stopped", StartupType: "disabled"}},
	}); err != nil {
		t.Fatalf("replace services: %v", err)
	}
	services, err = service.List(context.Background(), tenancy.DefaultScope(), "agent-01", Filter{})
	if err != nil || len(services) != 1 || services[0].State != StateStopped {
		t.Fatalf("expected replacement snapshot, got %#v, %v", services, err)
	}
}

func TestListFiltersServicesByStateAndQuery(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	service := NewService(store)
	if err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, Snapshot{
		ObservedAt: time.Now(),
		Services: []Fact{
			{Name: "nginx.service", DisplayName: "NGINX Web Server", State: "running", StartupType: "enabled"},
			{Name: "postgresql.service", DisplayName: "PostgreSQL", State: "stopped", StartupType: "manual"},
		},
	}); err != nil {
		t.Fatalf("report services: %v", err)
	}

	services, err := service.List(context.Background(), tenancy.DefaultScope(), "agent-01", Filter{State: StateRunning, Query: "web"})
	if err != nil {
		t.Fatalf("filter services: %v", err)
	}
	if len(services) != 1 || services[0].Name != "nginx.service" {
		t.Fatalf("expected filtered nginx service, got %#v", services)
	}
}

func TestReportRejectsInvalidSnapshot(t *testing.T) {
	t.Parallel()
	service := NewService(NewMemoryStore())
	cases := []Snapshot{
		{},
		{ObservedAt: time.Now(), Services: []Fact{{Name: "", State: "running"}}},
		{ObservedAt: time.Now(), Services: []Fact{{Name: "nginx", State: "paused"}}},
	}
	for _, snapshot := range cases {
		if err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, snapshot); !errors.Is(err, ErrInvalidSnapshot) {
			t.Fatalf("expected invalid snapshot for %#v, got %v", snapshot, err)
		}
	}
}

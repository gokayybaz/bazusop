package inventory_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestAgentFactsAreNormalizedAndUpserted(t *testing.T) {
	t.Parallel()

	store := inventory.NewMemoryStore()
	now := time.Date(2026, time.September, 10, 20, 0, 0, 0, time.UTC)
	service := inventory.NewService(store, inventory.WithClock(func() time.Time { return now }))

	agent := tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}
	err := service.Report(context.Background(), agent, inventory.Facts{
		Hostname:      "EDGE-01.EXAMPLE.COM",
		OSFamily:      "LINUX",
		OSName:        "Ubuntu",
		OSVersion:     "24.04",
		Architecture:  "x86_64",
		KernelVersion: "6.8.0",
		CPUCores:      8,
		MemoryBytes:   16 * 1024 * 1024 * 1024,
		IPAddresses:   []string{"10.0.0.8", "10.0.0.8", "not-an-ip"},
		AgentVersion:  "0.2.0",
	})
	if err != nil {
		t.Fatalf("report host facts: %v", err)
	}

	now = now.Add(time.Minute)
	err = service.Report(context.Background(), agent, inventory.Facts{
		Hostname:      "edge-01.example.com",
		OSFamily:      "linux",
		OSName:        "Ubuntu",
		OSVersion:     "24.04.1",
		Architecture:  "amd64",
		KernelVersion: "6.8.1",
		CPUCores:      8,
		MemoryBytes:   16 * 1024 * 1024 * 1024,
		IPAddresses:   []string{"10.0.0.8", "2001:db8::8"},
		AgentVersion:  "0.2.1",
	})
	if err != nil {
		t.Fatalf("update host facts: %v", err)
	}

	hosts, err := service.List(context.Background(), agent.Scope())
	if err != nil {
		t.Fatalf("list hosts: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected one upserted host, got %d", len(hosts))
	}
	host := hosts[0]
	if host.Hostname != "edge-01.example.com" {
		t.Errorf("expected normalized hostname, got %q", host.Hostname)
	}
	if host.Architecture != "amd64" {
		t.Errorf("expected normalized architecture, got %q", host.Architecture)
	}
	if host.OSVersion != "24.04.1" || host.AgentVersion != "0.2.1" {
		t.Errorf("expected updated facts, got %#v", host)
	}
	if len(host.IPAddresses) != 2 || host.IPAddresses[1] != "2001:db8::8" {
		t.Errorf("expected normalized addresses, got %#v", host.IPAddresses)
	}
	if host.Status != inventory.StatusConnected {
		t.Errorf("expected connected status, got %q", host.Status)
	}
}

func TestInvalidHostFactsAreRejected(t *testing.T) {
	t.Parallel()

	service := inventory.NewService(inventory.NewMemoryStore())
	err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, inventory.Facts{
		Hostname:     "edge-01",
		OSFamily:     "plan9",
		Architecture: "amd64",
		CPUCores:     8,
		MemoryBytes:  1024,
	})
	if err == nil {
		t.Fatal("expected unsupported operating system to be rejected")
	}
}

func TestHostsAreIsolatedByOrganizationSiteAndAgent(t *testing.T) {
	store := inventory.NewMemoryStore()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service := inventory.NewService(store, inventory.WithClock(func() time.Time { return now }))
	agents := []tenancy.Agent{
		{ID: "shared-agent", OrganizationID: "org-a", SiteID: "site-a"},
		{ID: "shared-agent", OrganizationID: "org-a", SiteID: "site-b"},
		{ID: "shared-agent", OrganizationID: "org-b", SiteID: "site-a"},
		{ID: "other-agent", OrganizationID: "org-a", SiteID: "site-a"},
	}
	for index, agent := range agents {
		now = now.Add(time.Minute)
		if err := service.Report(t.Context(), agent, validFacts(string(rune('a'+index)))); err != nil {
			t.Fatal(err)
		}
	}
	for index, agent := range agents[:3] {
		hosts, err := service.List(t.Context(), agent.Scope())
		wantCount := 1
		if index == 0 {
			wantCount = 2
		}
		if err != nil || len(hosts) != wantCount {
			t.Fatalf("scope %v: hosts=%#v, err=%v", agent.Scope(), hosts, err)
		}
		if hosts[0].Hostname != string(rune('a'+index)) || hosts[0].AgentID != "shared-agent" || hosts[0].Scope() != agent.Scope() {
			t.Fatalf("cross-scope overwrite/leak: %#v", hosts)
		}
		wantFirstSeen := time.Date(2026, 9, 13, 12, index+1, 0, 0, time.UTC)
		if !hosts[0].FirstSeenAt.Equal(wantFirstSeen) {
			t.Fatalf("cross-scope first_seen: %#v", hosts[0])
		}
	}
}

func TestInventoryRejectsInvalidScopeAndAgent(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := inventory.NewService(store)
	for _, scope := range []tenancy.Scope{{}, {OrganizationID: "org-a"}, {SiteID: "site-a"}, {OrganizationID: " ", SiteID: "site-a"}, {OrganizationID: strings.Repeat("x", 129), SiteID: "site-a"}} {
		if _, err := service.List(t.Context(), scope); !errors.Is(err, tenancy.ErrInvalidScope) {
			t.Fatalf("service list %v: %v", scope, err)
		}
		if _, err := store.List(t.Context(), scope); !errors.Is(err, tenancy.ErrInvalidScope) {
			t.Fatalf("store list %v: %v", scope, err)
		}
		if err := service.Report(t.Context(), tenancy.Agent{ID: "agent", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, validFacts("edge")); !errors.Is(err, tenancy.ErrInvalidScope) {
			t.Fatalf("report %v: %v", scope, err)
		}
	}
	if err := service.Report(t.Context(), tenancy.Agent{OrganizationID: "org-a", SiteID: "site-a"}, validFacts("edge")); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("missing agent ID: %v", err)
	}
	if err := store.Upsert(t.Context(), inventory.Host{AgentID: "agent", Hostname: "edge", OSFamily: "linux", Architecture: "amd64"}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("unscoped stored host: %v", err)
	}
}

func TestReportUsesTrustedAgentScopeAndSerializesIt(t *testing.T) {
	var facts inventory.Facts
	if err := json.Unmarshal([]byte(`{"agent_id":"attacker","organization_id":"org-other","site_id":"site-other","hostname":"edge","os_family":"linux","architecture":"amd64","cpu_cores":2,"memory_bytes":1024}`), &facts); err != nil {
		t.Fatal(err)
	}
	service := inventory.NewService(inventory.NewMemoryStore())
	agent := tenancy.Agent{ID: "trusted-agent", OrganizationID: "org-trusted", SiteID: "site-trusted"}
	if err := service.Report(t.Context(), agent, facts); err != nil {
		t.Fatal(err)
	}
	hosts, err := service.List(t.Context(), agent.Scope())
	if err != nil || len(hosts) != 1 {
		t.Fatalf("hosts=%#v, err=%v", hosts, err)
	}
	payload, err := json.Marshal(hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["agent_id"] != "trusted-agent" || fields["organization_id"] != "org-trusted" || fields["site_id"] != "site-trusted" {
		t.Fatalf("untrusted scope or missing JSON fields: %s", payload)
	}
}

func validFacts(hostname string) inventory.Facts {
	return inventory.Facts{Hostname: hostname, OSFamily: "linux", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024}
}

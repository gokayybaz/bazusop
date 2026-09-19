package cloudinventory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestDiscoveryReconcilesOnlyVerifiedAgentIdentityAutomatically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 9, 11, 18, 0, 0, 0, time.UTC)
	hosts := inventory.NewService(inventory.NewMemoryStore(), inventory.WithClock(func() time.Time { return now }))
	if err := hosts.Report(ctx, tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, inventory.Facts{Hostname: "edge-01.example.com", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 4, MemoryBytes: 8 << 30, IPAddresses: []string{"10.0.0.8"}, AgentVersion: "0.1.0"}); err != nil {
		t.Fatalf("seed host: %v", err)
	}

	service := NewService(NewMemoryStore(), hosts, WithClock(func() time.Time { return now }))
	scope := tenancy.DefaultScope()
	account, err := service.CreateAccount(ctx, scope, AccountRequest{Name: "Üretim AWS", Provider: ProviderAWS, ExternalID: "123456789012"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	instances, err := service.Reconcile(ctx, scope, account.ID, []DiscoveredInstance{
		{ProviderInstanceID: "i-verified", Name: "edge-01", Region: "eu-central-1", State: "running", OSFamily: "linux", PrivateIPs: []string{"10.0.0.8"}, AgentIDHint: "agent-01"},
		{ProviderInstanceID: "i-candidate", Name: "edge-01.example.com", Region: "eu-central-1", State: "running", OSFamily: "linux", PrivateIPs: []string{"10.0.0.8"}},
	})
	if err != nil {
		t.Fatalf("reconcile discovery: %v", err)
	}
	if instances[0].MatchStatus != MatchVerified || instances[0].AgentID != "agent-01" {
		t.Fatalf("verified instance = %#v", instances[0])
	}
	if instances[1].MatchStatus != MatchCandidate || instances[1].AgentID != "" {
		t.Fatalf("candidate instance must not auto-match: %#v", instances[1])
	}
}

func TestAccountValidationSupportsKnownProvidersOnly(t *testing.T) {
	t.Parallel()
	service := NewService(NewMemoryStore(), inventory.NewService(inventory.NewMemoryStore()))
	scope := tenancy.DefaultScope()

	for _, provider := range []Provider{ProviderAWS, ProviderAzure, ProviderGCP} {
		if _, err := service.CreateAccount(context.Background(), scope, AccountRequest{Name: "Hesap", Provider: provider, ExternalID: "tenant-01"}); err != nil {
			t.Fatalf("provider %s rejected: %v", provider, err)
		}
	}
	if _, err := service.CreateAccount(context.Background(), scope, AccountRequest{Name: "Hesap", Provider: "other", ExternalID: "tenant-01"}); err != ErrInvalidCloudInventory {
		t.Fatalf("unknown provider error = %v", err)
	}
}

func TestDiscoveryIsolatedBySiteForAccountAndHostMatching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scopeA := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	scopeB := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-b"}
	hosts := inventory.NewService(inventory.NewMemoryStore())
	for _, host := range []tenancy.Agent{
		{ID: "agent-a", OrganizationID: scopeA.OrganizationID, SiteID: scopeA.SiteID},
		{ID: "agent-b", OrganizationID: scopeB.OrganizationID, SiteID: scopeB.SiteID},
	} {
		if err := hosts.Report(ctx, host, inventory.Facts{
			Hostname: "shared.example.com", OSFamily: "linux", OSName: "Linux", OSVersion: "1",
			Architecture: "amd64", CPUCores: 2, MemoryBytes: 1 << 30, IPAddresses: []string{"10.0.0.8"}, AgentVersion: "1",
		}); err != nil {
			t.Fatalf("seed host %s: %v", host.ID, err)
		}
	}
	service := NewService(NewMemoryStore(), hosts)
	accountA, err := service.CreateAccount(ctx, scopeA, AccountRequest{Name: "A", Provider: ProviderAWS, ExternalID: "same-tenant"})
	if err != nil {
		t.Fatalf("create account A: %v", err)
	}
	if _, err := service.CreateAccount(ctx, scopeB, AccountRequest{Name: "B", Provider: ProviderAWS, ExternalID: "same-tenant"}); err != nil {
		t.Fatalf("same external account should be valid across sites: %v", err)
	}

	instances, err := service.Reconcile(ctx, scopeA, accountA.ID, []DiscoveredInstance{
		{ProviderInstanceID: "i-shared", Name: "shared.example.com", Region: "eu-central-1", State: "running", OSFamily: "linux", PrivateIPs: []string{"10.0.0.8"}},
		{ProviderInstanceID: "i-cross-site-agent", Name: "other.example.com", Region: "eu-central-1", State: "running", OSFamily: "linux", AgentIDHint: "agent-b"},
	})
	if err != nil {
		t.Fatalf("reconcile site A: %v", err)
	}
	if len(instances) != 2 || instances[0].MatchStatus != MatchCandidate || instances[0].CandidateAgentID != "agent-a" || instances[0].OrganizationID != scopeA.OrganizationID || instances[0].SiteID != scopeA.SiteID {
		t.Fatalf("site A match escaped scope: %#v", instances)
	}
	if instances[1].MatchStatus != MatchUnmatched || instances[1].AgentID != "" || instances[1].MatchReason != "agent_identity_not_found" {
		t.Fatalf("cross-site agent hint matched: %#v", instances[1])
	}
	if _, err := service.Reconcile(ctx, scopeB, accountA.ID, nil); err != ErrCloudAccountNotFound {
		t.Fatalf("cross-site account reconcile error = %v", err)
	}
	accounts, err := service.ListAccounts(ctx, scopeB)
	if err != nil || len(accounts) != 1 || accounts[0].SiteID != scopeB.SiteID {
		t.Fatalf("site B accounts = %#v, err = %v", accounts, err)
	}
	instances, err = service.ListInstances(ctx, scopeB)
	if err != nil || len(instances) != 0 {
		t.Fatalf("site B instances = %#v, err = %v", instances, err)
	}
}

func TestCloudServiceRejectsInvalidScope(t *testing.T) {
	t.Parallel()
	service := NewService(NewMemoryStore(), inventory.NewService(inventory.NewMemoryStore()))
	if _, err := service.CreateAccount(t.Context(), tenancy.Scope{}, AccountRequest{Name: "A", Provider: ProviderAWS, ExternalID: "tenant"}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("create with invalid scope error = %v", err)
	}
	if _, err := service.ListAccounts(t.Context(), tenancy.Scope{}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("list accounts with invalid scope error = %v", err)
	}
	if _, err := service.ListInstances(t.Context(), tenancy.Scope{}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("list instances with invalid scope error = %v", err)
	}
	if _, err := service.Reconcile(t.Context(), tenancy.Scope{}, "account", nil); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("reconcile with invalid scope error = %v", err)
	}
}

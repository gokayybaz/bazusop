package cloudinventory

import (
	"context"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
)

func TestDiscoveryReconcilesOnlyVerifiedAgentIdentityAutomatically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 9, 11, 18, 0, 0, 0, time.UTC)
	hosts := inventory.NewService(inventory.NewMemoryStore(), inventory.WithClock(func() time.Time { return now }))
	if err := hosts.Report(ctx, "agent-01", inventory.Facts{Hostname: "edge-01.example.com", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 4, MemoryBytes: 8 << 30, IPAddresses: []string{"10.0.0.8"}, AgentVersion: "0.1.0"}); err != nil {
		t.Fatalf("seed host: %v", err)
	}

	service := NewService(NewMemoryStore(), hosts, WithClock(func() time.Time { return now }))
	account, err := service.CreateAccount(ctx, AccountRequest{Name: "Üretim AWS", Provider: ProviderAWS, ExternalID: "123456789012"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	instances, err := service.Reconcile(ctx, account.ID, []DiscoveredInstance{
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

	for _, provider := range []Provider{ProviderAWS, ProviderAzure, ProviderGCP} {
		if _, err := service.CreateAccount(context.Background(), AccountRequest{Name: "Hesap", Provider: provider, ExternalID: "tenant-01"}); err != nil {
			t.Fatalf("provider %s rejected: %v", provider, err)
		}
	}
	if _, err := service.CreateAccount(context.Background(), AccountRequest{Name: "Hesap", Provider: "other", ExternalID: "tenant-01"}); err != ErrInvalidCloudInventory {
		t.Fatalf("unknown provider error = %v", err)
	}
}

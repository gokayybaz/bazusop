package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresCloudInventoryIsScopedBySite(t *testing.T) {
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	orgID := "cloud-org-" + suffix
	scopeA := tenancy.Scope{OrganizationID: orgID, SiteID: "cloud-site-a-" + suffix}
	scopeB := tenancy.Scope{OrganizationID: orgID, SiteID: "cloud-site-b-" + suffix}
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id,name) VALUES ($1,$2)`, orgID, "Cloud test"); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []tenancy.Scope{scopeA, scopeB} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id,organization_id,name,slug) VALUES ($1,$2,$1,$1)`, scope.SiteID, orgID); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, table := range []string{"cloud_instances", "cloud_accounts", "sites", "organizations"} {
			column := "organization_id"
			if table == "organizations" {
				column = "id"
			}
			if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM "+table+" WHERE "+column+"=$1", orgID); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	}()

	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	accountA := cloudinventory.Account{OrganizationID: scopeA.OrganizationID, SiteID: scopeA.SiteID, ID: "cloud-account-a-" + suffix, AccountRequest: cloudinventory.AccountRequest{Name: "A", Provider: cloudinventory.ProviderAWS, ExternalID: "same-tenant"}, Status: cloudinventory.AccountPending, CreatedAt: now}
	accountB := cloudinventory.Account{OrganizationID: scopeB.OrganizationID, SiteID: scopeB.SiteID, ID: "cloud-account-b-" + suffix, AccountRequest: cloudinventory.AccountRequest{Name: "B", Provider: cloudinventory.ProviderAWS, ExternalID: "same-tenant"}, Status: cloudinventory.AccountPending, CreatedAt: now}
	if err := store.CreateCloudAccount(ctx, scopeA, accountA); err != nil {
		t.Fatalf("create account A: %v", err)
	}
	if err := store.CreateCloudAccount(ctx, scopeB, accountB); err != nil {
		t.Fatalf("same external account should be valid across sites: %v", err)
	}
	instanceA := cloudinventory.Instance{OrganizationID: scopeA.OrganizationID, SiteID: scopeA.SiteID, AccountID: accountA.ID, AccountName: accountA.Name, Provider: accountA.Provider, DiscoveredInstance: cloudinventory.DiscoveredInstance{ProviderInstanceID: "i-a", Name: "shared", Region: "eu-central-1", State: "running", OSFamily: "linux", PrivateIPs: []string{}, PublicIPs: []string{}}, MatchStatus: cloudinventory.MatchUnmatched, MatchReason: "no_agent_signal", DiscoveredAt: now}
	instanceB := instanceA
	instanceB.OrganizationID, instanceB.SiteID, instanceB.AccountID = scopeB.OrganizationID, scopeB.SiteID, accountB.ID
	if err := store.ReplaceCloudInstances(ctx, scopeA, accountA, []cloudinventory.Instance{instanceA}); err != nil {
		t.Fatalf("replace site A instances: %v", err)
	}
	if err := store.ReplaceCloudInstances(ctx, scopeB, accountB, []cloudinventory.Instance{instanceB}); err != nil {
		t.Fatalf("replace site B instances: %v", err)
	}
	if accounts, err := store.ListCloudAccounts(ctx, scopeA); err != nil || len(accounts) != 1 || accounts[0].ID != accountA.ID || accounts[0].SiteID != scopeA.SiteID {
		t.Fatalf("site A accounts = %#v, err = %v", accounts, err)
	}
	if accounts, err := store.ListCloudAccounts(ctx, scopeB); err != nil || len(accounts) != 1 || accounts[0].ID != accountB.ID || accounts[0].SiteID != scopeB.SiteID {
		t.Fatalf("site B accounts = %#v, err = %v", accounts, err)
	}
	if _, err := store.GetCloudAccount(ctx, scopeB, accountA.ID); !errors.Is(err, cloudinventory.ErrCloudAccountNotFound) {
		t.Fatalf("cross-site account lookup error = %v", err)
	}
	if instances, err := store.ListCloudInstances(ctx, scopeA); err != nil || len(instances) != 1 || instances[0].AccountID != accountA.ID || instances[0].SiteID != scopeA.SiteID {
		t.Fatalf("site A instances = %#v, err = %v", instances, err)
	}
	if instances, err := store.ListCloudInstances(ctx, scopeB); err != nil || len(instances) != 1 || instances[0].AccountID != accountB.ID || instances[0].SiteID != scopeB.SiteID {
		t.Fatalf("site B instances = %#v, err = %v", instances, err)
	}
}

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
)

func TestPostgresServiceAccountLifecycle(t *testing.T) {
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	orgID := "sat-org-" + suffix
	siteID := "sat-site-" + suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Service account test')`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1,$2,'SAT site','sat-site')`, siteID, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		store.pool.Exec(cleanupCtx, "DELETE FROM service_account_tokens WHERE service_account_id IN (SELECT id FROM service_accounts WHERE organization_id=$1)", orgID)
		store.pool.Exec(cleanupCtx, "DELETE FROM service_accounts WHERE organization_id=$1", orgID)
		store.pool.Exec(cleanupCtx, "DELETE FROM sites WHERE id=$1", siteID)
		store.pool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id=$1", orgID)
	}()

	now := time.Now().UTC()
	account := serviceaccounts.ServiceAccount{ID: "sat-account-" + suffix, OrganizationID: orgID, SiteID: siteID, Name: "ci-bot", Role: authorization.SiteRoleOperator, CreatedAt: now}
	if err := store.CreateAccount(ctx, account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	fetched, err := store.AccountByID(ctx, account.ID)
	if err != nil || fetched.Name != "ci-bot" || fetched.Role != authorization.SiteRoleOperator {
		t.Fatalf("expected to read back the account, got %#v %v", fetched, err)
	}
	accounts, err := store.AccountsForSite(ctx, siteID)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("expected one account for the site, got %#v %v", accounts, err)
	}

	token := serviceaccounts.Token{ID: "sat-token-" + suffix, ServiceAccountID: account.ID, CreatedAt: now, ExpiresAt: now.AddDate(0, 0, 90)}
	if err := store.CreateToken(ctx, token, "hash-1"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	fetchedToken, hash, err := store.TokenByID(ctx, token.ID)
	if err != nil || hash != "hash-1" || fetchedToken.ServiceAccountID != account.ID {
		t.Fatalf("expected to read back the token, got %#v %q %v", fetchedToken, hash, err)
	}

	touchedAt := now.Add(time.Hour)
	if err := store.TouchToken(ctx, token.ID, touchedAt, "10.0.0.5"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	afterTouch, _, err := store.TokenByID(ctx, token.ID)
	if err != nil || afterTouch.LastUsedIP != "10.0.0.5" || afterTouch.LastUsedAt == nil || !afterTouch.LastUsedAt.Equal(touchedAt) {
		t.Fatalf("expected last-used metadata to be updated, got %#v %v", afterTouch, err)
	}

	if err := store.RevokeToken(ctx, token.ID, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	revoked, _, err := store.TokenByID(ctx, token.ID)
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("expected the token to be revoked, got %#v %v", revoked, err)
	}

	secondToken := serviceaccounts.Token{ID: "sat-token-2-" + suffix, ServiceAccountID: account.ID, CreatedAt: now, ExpiresAt: now.AddDate(0, 0, 90)}
	if err := store.CreateToken(ctx, secondToken, "hash-2"); err != nil {
		t.Fatalf("create second token: %v", err)
	}
	if err := store.RevokeActiveTokensForAccount(ctx, account.ID, now.Add(3*time.Hour)); err != nil {
		t.Fatalf("revoke active tokens: %v", err)
	}
	secondRevoked, _, err := store.TokenByID(ctx, secondToken.ID)
	if err != nil || secondRevoked.RevokedAt == nil {
		t.Fatalf("expected the second token to be revoked too, got %#v %v", secondRevoked, err)
	}

	if err := store.DisableAccount(ctx, account.ID, now.Add(4*time.Hour)); err != nil {
		t.Fatalf("disable account: %v", err)
	}
	disabledAccount, err := store.AccountByID(ctx, account.ID)
	if err != nil || disabledAccount.DisabledAt == nil {
		t.Fatalf("expected the account to be disabled, got %#v %v", disabledAccount, err)
	}

	if _, err := store.AccountByID(ctx, "does-not-exist"); !errors.Is(err, serviceaccounts.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
	if _, _, err := store.TokenByID(ctx, "does-not-exist"); !errors.Is(err, serviceaccounts.ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

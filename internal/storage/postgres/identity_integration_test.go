package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/identity"
)

func TestPostgresIdentityBootstrapInviteAndRecoveryCodeLifecycle(t *testing.T) {
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
	orgID := "identity-org-" + suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Identity test')`, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, table := range []string{"recovery_codes", "invites", "users", "organizations"} {
			column := "organization_id"
			if table == "organizations" {
				column = "id"
			}
			if table == "recovery_codes" {
				continue // no organization_id column; cascades from users
			}
			if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM "+table+" WHERE "+column+"=$1", orgID); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	}()

	bootstrapped, err := store.IsBootstrapped(ctx)
	if err != nil || bootstrapped {
		t.Fatalf("expected a fresh test database to be unbootstrapped, got %v %v", bootstrapped, err)
	}

	admin := identity.User{ID: "identity-admin-" + suffix, OrganizationID: orgID, Email: "admin@example.com", Role: identity.RolePlatformAdmin, PasswordHash: "hash", CreatedAt: time.Now().UTC()}
	if err := store.CompleteBootstrap(ctx, admin); err != nil {
		t.Fatalf("complete bootstrap: %v", err)
	}
	if bootstrapped, err := store.IsBootstrapped(ctx); err != nil || !bootstrapped {
		t.Fatalf("expected the database to report bootstrapped, got %v %v", bootstrapped, err)
	}
	if err := store.CompleteBootstrap(ctx, admin); err == nil {
		t.Fatal("expected a second CompleteBootstrap to fail")
	}

	fetched, err := store.UserByEmail(ctx, orgID, "admin@example.com")
	if err != nil || fetched.ID != admin.ID {
		t.Fatalf("expected to read back the bootstrapped admin, got %#v %v", fetched, err)
	}

	invite := identity.Invite{ID: "identity-invite-" + suffix, OrganizationID: orgID, Email: "new-admin@example.com", Role: identity.RolePlatformAdmin, CreatedBy: "admin", ExpiresAt: time.Now().UTC().Add(24 * time.Hour), CreatedAt: time.Now().UTC()}
	tokenHash := "token-hash-" + suffix
	if err := store.SaveInvite(ctx, invite, tokenHash); err != nil {
		t.Fatalf("save invite: %v", err)
	}
	fetchedInvite, err := store.InviteByTokenHash(ctx, tokenHash)
	if err != nil || fetchedInvite.Email != invite.Email {
		t.Fatalf("expected to read back the invite, got %#v %v", fetchedInvite, err)
	}

	newUser := identity.User{ID: "identity-new-admin-" + suffix, OrganizationID: orgID, Email: invite.Email, Role: identity.RolePlatformAdmin, PasswordHash: "hash2", CreatedAt: time.Now().UTC()}
	if err := store.CreateUser(ctx, newUser); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.ConsumeInvite(ctx, tokenHash, newUser.ID, time.Now().UTC()); err != nil {
		t.Fatalf("consume invite: %v", err)
	}
	consumedInvite, err := store.InviteByTokenHash(ctx, tokenHash)
	if err != nil || consumedInvite.ConsumedAt == nil {
		t.Fatalf("expected the invite to be marked consumed, got %#v %v", consumedInvite, err)
	}

	if err := store.SaveRecoveryCodes(ctx, newUser.ID, []string{"hash-a", "hash-b"}); err != nil {
		t.Fatalf("save recovery codes: %v", err)
	}
	consumed, err := store.ConsumeRecoveryCode(ctx, newUser.ID, "hash-a")
	if err != nil || !consumed {
		t.Fatalf("expected the first consumption to succeed, got %v %v", consumed, err)
	}
	consumedAgain, err := store.ConsumeRecoveryCode(ctx, newUser.ID, "hash-a")
	if err != nil || consumedAgain {
		t.Fatalf("expected the second consumption of the same code to fail, got %v %v", consumedAgain, err)
	}
}

func TestPostgresInviteWithSiteRoleGrantsPersistsAndReadsBack(t *testing.T) {
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
	orgID := "invite-site-org-" + suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Invite site test')`, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		store.pool.Exec(cleanupCtx, "DELETE FROM invites WHERE organization_id=$1", orgID)
		store.pool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id=$1", orgID)
	}()

	invite := identity.Invite{
		ID: "invite-site-" + suffix, OrganizationID: orgID, Email: "operator@example.com", Role: "",
		SiteRoleGrants: []identity.SiteRoleGrant{{SiteID: "site_default", Role: "operator"}},
		CreatedBy:      "admin", ExpiresAt: time.Now().UTC().Add(24 * time.Hour), CreatedAt: time.Now().UTC(),
	}
	tokenHash := "site-token-hash-" + suffix
	if err := store.SaveInvite(ctx, invite, tokenHash); err != nil {
		t.Fatalf("save invite: %v", err)
	}
	fetched, err := store.InviteByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("fetch invite: %v", err)
	}
	if fetched.Role != "" || len(fetched.SiteRoleGrants) != 1 || fetched.SiteRoleGrants[0] != invite.SiteRoleGrants[0] {
		t.Fatalf("expected the site-role grant to round-trip, got %#v", fetched)
	}
}

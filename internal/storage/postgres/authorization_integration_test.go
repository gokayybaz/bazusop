package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/authorization"
)

func TestPostgresSiteMembershipLifecycle(t *testing.T) {
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
	orgID := "authz-org-" + suffix
	siteID := "authz-site-" + suffix
	userID := "authz-user-" + suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Authz test')`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1,$2,'Authz site','authz-site')`, siteID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO users (id, organization_id, email, role, password_hash, created_at) VALUES ($1,$2,'authz-test@example.com','','hash', now())`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		store.pool.Exec(cleanupCtx, "DELETE FROM site_memberships WHERE organization_id=$1", orgID)
		store.pool.Exec(cleanupCtx, "DELETE FROM users WHERE id=$1", userID)
		store.pool.Exec(cleanupCtx, "DELETE FROM sites WHERE id=$1", siteID)
		store.pool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id=$1", orgID)
	}()

	if _, err := store.RoleForUserAtSite(ctx, userID, siteID); !errors.Is(err, authorization.ErrMembershipNotFound) {
		t.Fatalf("expected ErrMembershipNotFound before any assignment, got %v", err)
	}

	membership := authorization.SiteMembership{ID: "membership-" + suffix, UserID: userID, OrganizationID: orgID, SiteID: siteID, Role: authorization.SiteRoleViewer, CreatedAt: time.Now().UTC()}
	if err := store.AssignRole(ctx, membership); err != nil {
		t.Fatalf("assign role: %v", err)
	}
	role, err := store.RoleForUserAtSite(ctx, userID, siteID)
	if err != nil || role != authorization.SiteRoleViewer {
		t.Fatalf("expected viewer, got %v %v", role, err)
	}

	membership.Role = authorization.SiteRoleAdmin
	if err := store.AssignRole(ctx, membership); err != nil {
		t.Fatalf("reassign role: %v", err)
	}
	role, err = store.RoleForUserAtSite(ctx, userID, siteID)
	if err != nil || role != authorization.SiteRoleAdmin {
		t.Fatalf("expected the role to be replaced with site-admin, got %v %v", role, err)
	}

	memberships, err := store.MembershipsForUser(ctx, userID)
	if err != nil || len(memberships) != 1 {
		t.Fatalf("expected exactly one membership, got %#v %v", memberships, err)
	}

	if err := store.RevokeRole(ctx, userID, siteID); err != nil {
		t.Fatalf("revoke role: %v", err)
	}
	if _, err := store.RoleForUserAtSite(ctx, userID, siteID); !errors.Is(err, authorization.ErrMembershipNotFound) {
		t.Fatalf("expected ErrMembershipNotFound after revoke, got %v", err)
	}
}

package authorization_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gokayybaz/bazusop/internal/authorization"
)

func newTestService(t *testing.T, platformAdmins map[string]bool) *authorization.Service {
	t.Helper()
	checker := func(_ context.Context, userID string) (bool, error) { return platformAdmins[userID], nil }
	service, err := authorization.NewService(authorization.NewMemoryStore(), checker)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestPlatformAdminSatisfiesEveryPermissionEverywhere(t *testing.T) {
	t.Parallel()
	service := newTestService(t, map[string]bool{"admin-1": true})
	permissions := []authorization.Permission{
		authorization.PermissionManageUsers, authorization.PermissionManageOrgSecurity,
		authorization.PermissionViewSite, authorization.PermissionManageAgents,
		authorization.PermissionManageAlerts, authorization.PermissionManageCloudAccounts,
		authorization.PermissionCreateJobs, authorization.PermissionAcknowledgeIncidents,
		authorization.PermissionViewActivity, authorization.PermissionViewAuditEvents,
	}
	for _, permission := range permissions {
		allowed, err := service.Can(t.Context(), "admin-1", permission, "site-anything")
		if err != nil || !allowed {
			t.Fatalf("expected platform-admin to satisfy %s everywhere, got %v %v", permission, allowed, err)
		}
	}
}

func TestOrgScopedPermissionsAreDeniedToEverySiteRole(t *testing.T) {
	t.Parallel()
	service := newTestService(t, nil)
	if err := service.AssignRole(t.Context(), "user-1", "org_default", "site-1", authorization.SiteRoleAdmin); err != nil {
		t.Fatal(err)
	}
	for _, permission := range []authorization.Permission{authorization.PermissionManageUsers, authorization.PermissionManageOrgSecurity} {
		allowed, err := service.Can(t.Context(), "user-1", permission, "site-1")
		if err != nil || allowed {
			t.Fatalf("expected %s to be denied to a site-admin (org-scoped, platform-admin only), got %v %v", permission, allowed, err)
		}
	}
}

// TestFullPermissionMatrixMatchesTheSpec encodes the exact table from the
// design spec ("Sabit RBAC modeli" -> "Roller"): for every permission, the
// site roles that satisfy it. Platform-admin is covered separately above
// (it satisfies everything); this test proves the remaining three roles
// match the table exactly, including permissions none of them satisfy.
func TestFullPermissionMatrixMatchesTheSpec(t *testing.T) {
	t.Parallel()
	expected := map[authorization.Permission]map[authorization.SiteRole]bool{
		authorization.PermissionViewSite:             {authorization.SiteRoleAdmin: true, authorization.SiteRoleOperator: true, authorization.SiteRoleViewer: true},
		authorization.PermissionManageAgents:         {authorization.SiteRoleAdmin: true, authorization.SiteRoleOperator: false, authorization.SiteRoleViewer: false},
		authorization.PermissionManageAlerts:         {authorization.SiteRoleAdmin: true, authorization.SiteRoleOperator: false, authorization.SiteRoleViewer: false},
		authorization.PermissionManageCloudAccounts:  {authorization.SiteRoleAdmin: true, authorization.SiteRoleOperator: false, authorization.SiteRoleViewer: false},
		authorization.PermissionCreateJobs:           {authorization.SiteRoleAdmin: true, authorization.SiteRoleOperator: true, authorization.SiteRoleViewer: false},
		authorization.PermissionAcknowledgeIncidents: {authorization.SiteRoleAdmin: true, authorization.SiteRoleOperator: true, authorization.SiteRoleViewer: false},
		authorization.PermissionViewActivity:         {authorization.SiteRoleAdmin: true, authorization.SiteRoleOperator: true, authorization.SiteRoleViewer: true},
		authorization.PermissionViewAuditEvents:      {authorization.SiteRoleAdmin: true, authorization.SiteRoleOperator: true, authorization.SiteRoleViewer: false},
	}
	roles := []authorization.SiteRole{authorization.SiteRoleAdmin, authorization.SiteRoleOperator, authorization.SiteRoleViewer}
	for permission, byRole := range expected {
		for _, role := range roles {
			service := newTestService(t, nil)
			if err := service.AssignRole(t.Context(), "user-1", "org_default", "site-1", role); err != nil {
				t.Fatal(err)
			}
			allowed, err := service.Can(t.Context(), "user-1", permission, "site-1")
			if err != nil {
				t.Fatalf("%s/%s: %v", permission, role, err)
			}
			if allowed != byRole[role] {
				t.Fatalf("%s/%s: expected allowed=%v, got %v", permission, role, byRole[role], allowed)
			}
		}
	}
}

func TestCanDeniesAUserWithNoMembershipAtTheSite(t *testing.T) {
	t.Parallel()
	service := newTestService(t, nil)
	if err := service.AssignRole(t.Context(), "user-1", "org_default", "site-1", authorization.SiteRoleAdmin); err != nil {
		t.Fatal(err)
	}
	allowed, err := service.Can(t.Context(), "user-1", authorization.PermissionViewSite, "site-2")
	if err != nil || allowed {
		t.Fatalf("expected no access to a site the user has no membership at, got %v %v", allowed, err)
	}
}

func TestAssignRoleReplacesAnExistingRoleAtTheSameSite(t *testing.T) {
	t.Parallel()
	service := newTestService(t, nil)
	if err := service.AssignRole(t.Context(), "user-1", "org_default", "site-1", authorization.SiteRoleViewer); err != nil {
		t.Fatal(err)
	}
	role, err := service.RoleForUserAtSite(t.Context(), "user-1", "site-1")
	if err != nil || role != authorization.SiteRoleViewer {
		t.Fatalf("expected viewer, got %v %v", role, err)
	}
	if err := service.AssignRole(t.Context(), "user-1", "org_default", "site-1", authorization.SiteRoleAdmin); err != nil {
		t.Fatal(err)
	}
	role, err = service.RoleForUserAtSite(t.Context(), "user-1", "site-1")
	if err != nil || role != authorization.SiteRoleAdmin {
		t.Fatalf("expected the role to be replaced with site-admin, got %v %v", role, err)
	}
}

func TestRevokeRoleRemovesAccess(t *testing.T) {
	t.Parallel()
	service := newTestService(t, nil)
	if err := service.AssignRole(t.Context(), "user-1", "org_default", "site-1", authorization.SiteRoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeRole(t.Context(), "user-1", "site-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := service.RoleForUserAtSite(t.Context(), "user-1", "site-1"); !errors.Is(err, authorization.ErrMembershipNotFound) {
		t.Fatalf("expected ErrMembershipNotFound after revoke, got %v", err)
	}
}

func TestMembershipsForUserListsEverySiteTheyBelongTo(t *testing.T) {
	t.Parallel()
	service := newTestService(t, nil)
	if err := service.AssignRole(t.Context(), "user-1", "org_default", "site-1", authorization.SiteRoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := service.AssignRole(t.Context(), "user-1", "org_default", "site-2", authorization.SiteRoleViewer); err != nil {
		t.Fatal(err)
	}
	memberships, err := service.MembershipsForUser(t.Context(), "user-1")
	if err != nil || len(memberships) != 2 {
		t.Fatalf("expected 2 memberships, got %#v %v", memberships, err)
	}
}

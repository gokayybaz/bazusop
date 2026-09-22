package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func newRBACHandler(t *testing.T) (http.Handler, *identity.Service, *authorization.Service, *sessions.Service) {
	t.Helper()
	identityService := identity.NewService(identity.NewMemoryStore(), "test-totp-encryption-key")
	authzService, err := authorization.NewService(authorization.NewMemoryStore(), identityService.IsPlatformAdmin)
	if err != nil {
		t.Fatal(err)
	}
	sessionService, err := sessions.NewService(sessions.NewMemoryStore(), identityService.IsUserActive)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.NewHandler(
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)
	return handler, identityService, authzService, sessionService
}

func TestRequirePermissionAllowsAPlatformAdminSession(t *testing.T) {
	t.Parallel()
	handler, identityService, authzService, sessionService := newRBACHandler(t)

	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, token, _, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	request.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected a platform-admin session to be allowed to assign a site role, got %d: %s", response.Code, response.Body.String())
	}

	role, err := authzService.RoleForUserAtSite(t.Context(), "some-user", "site_default")
	if err != nil || role != authorization.SiteRoleViewer {
		t.Fatalf("expected the assignment to take effect, got %v %v", role, err)
	}
}

func TestRequirePermissionDeniesASessionLackingThePermission(t *testing.T) {
	t.Parallel()
	handler, identityService, authzService, sessionService := newRBACHandler(t)

	if _, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}

	_, token, err := identityService.CreateInvite(t.Context(), "admin", tenancy.DefaultOrganizationID, "viewer@example.com", "", []identity.SiteRoleGrant{{SiteID: "site_default", Role: "viewer"}})
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := identityService.ConsumeInvite(t.Context(), token, "a brand new password")
	if err != nil {
		t.Fatal(err)
	}
	if err := authzService.AssignRole(t.Context(), viewer.ID, tenancy.DefaultOrganizationID, "site_default", authorization.SiteRoleViewer); err != nil {
		t.Fatal(err)
	}
	_, sessionToken, _, err := sessionService.Create(t.Context(), viewer.ID, viewer.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "another-user", "role": "viewer"}))
	request.AddCookie(&http.Cookie{Name: "bazusop_session", Value: sessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected a viewer session to be denied PermissionManageUsers, got %d: %s", response.Code, response.Body.String())
	}
}

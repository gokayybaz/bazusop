package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

// newOperatorAndSiteAdminSessions builds a full hub handler plus one
// site-admin and one operator session for tenancy.DefaultSiteID, using
// only the service layer (not HTTP) to set them up — sidesteps the fact
// that HTTP login always requires a real TOTP code, which the HTTP layer
// never exposes a way to compute (by design, see spike 11.3). This test's
// purpose is permission enforcement, not the login flow itself (already
// covered by spike 11.4's tests), so building the session cookie directly
// via sessions.Service.Create is a legitimate shortcut here.
func newOperatorAndSiteAdminSessions(t *testing.T) (handler http.Handler, siteAdminCookies, operatorCookies []*http.Cookie) {
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
	registry := inventory.NewService(inventory.NewMemoryStore())
	agent := tenancy.Agent{ID: "edge-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}
	if err := registry.Report(t.Context(), agent, inventory.Facts{Hostname: "edge-01", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}); err != nil {
		t.Fatal(err)
	}
	jobService, err := jobs.NewService(jobs.NewMemoryStore(), jobs.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	alertService, err := alerting.NewService(alerting.NewMemoryStore(), alerting.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	handler = server.NewHandler(
		server.WithDefaultScope(tenancy.DefaultScope()),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithJobs(jobService),
		server.WithAlerts(alertService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)

	if _, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}

	siteAdmin := inviteConsumeAndAssign(t, identityService, authzService, "site-admin@example.com", authorization.SiteRoleAdmin)
	operator := inviteConsumeAndAssign(t, identityService, authzService, "operator@example.com", authorization.SiteRoleOperator)

	_, siteAdminToken, _, err := sessionService.Create(t.Context(), siteAdmin.ID, siteAdmin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	_, operatorToken, _, err := sessionService.Create(t.Context(), operator.ID, operator.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	siteAdminCookies = []*http.Cookie{{Name: "bazusop_session", Value: siteAdminToken}}
	operatorCookies = []*http.Cookie{{Name: "bazusop_session", Value: operatorToken}}
	return handler, siteAdminCookies, operatorCookies
}

func inviteConsumeAndAssign(t *testing.T, identityService *identity.Service, authzService *authorization.Service, email string, role authorization.SiteRole) identity.User {
	t.Helper()
	_, token, err := identityService.CreateInvite(t.Context(), "admin", tenancy.DefaultOrganizationID, email, "", []identity.SiteRoleGrant{{SiteID: tenancy.DefaultSiteID, Role: string(role)}})
	if err != nil {
		t.Fatal(err)
	}
	user, err := identityService.ConsumeInvite(t.Context(), token, "a brand new password")
	if err != nil {
		t.Fatal(err)
	}
	if err := authzService.AssignRole(t.Context(), user.ID, tenancy.DefaultOrganizationID, tenancy.DefaultSiteID, role); err != nil {
		t.Fatal(err)
	}
	return user
}

func TestSiteAdminCanCreateJobsButNotManageAlerts(t *testing.T) {
	t.Parallel()
	handler, siteAdminCookies, _ := newOperatorAndSiteAdminSessions(t)

	createJob := httptest.NewRequest(http.MethodPost, "/api/v1/instances/edge-01/jobs", encodeJSON(t, map[string]string{
		"action": "service.restart", "target": "nginx.service", "approved_by": "site-admin@example.com", "reason": "test",
	}))
	for _, cookie := range siteAdminCookies {
		createJob.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, createJob)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected a site-admin session to be allowed to create a job, got %d: %s", response.Code, response.Body.String())
	}
}

func TestSiteAdminCanManageAlertsButOperatorCannot(t *testing.T) {
	t.Parallel()
	handler, siteAdminCookies, operatorCookies := newOperatorAndSiteAdminSessions(t)

	siteAdminRequest := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", encodeJSON(t, map[string]any{
		"name": "Yüksek CPU", "kind": "metric", "metric": "cpu", "threshold": 90, "severity": "critical", "enabled": true,
	}))
	for _, cookie := range siteAdminCookies {
		siteAdminRequest.AddCookie(cookie)
	}
	siteAdminResponse := httptest.NewRecorder()
	handler.ServeHTTP(siteAdminResponse, siteAdminRequest)
	if siteAdminResponse.Code != http.StatusCreated {
		t.Fatalf("expected a site-admin session to manage alerts, got %d: %s", siteAdminResponse.Code, siteAdminResponse.Body.String())
	}

	operatorRequest := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", encodeJSON(t, map[string]any{
		"name": "Yüksek CPU 2", "kind": "metric", "metric": "cpu", "threshold": 90, "severity": "critical", "enabled": true,
	}))
	for _, cookie := range operatorCookies {
		operatorRequest.AddCookie(cookie)
	}
	operatorResponse := httptest.NewRecorder()
	handler.ServeHTTP(operatorResponse, operatorRequest)
	if operatorResponse.Code != http.StatusForbidden {
		t.Fatalf("expected an operator session to be denied alert management, got %d: %s", operatorResponse.Code, operatorResponse.Body.String())
	}
}

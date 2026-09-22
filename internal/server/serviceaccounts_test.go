package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func newServiceAccountHandler(t *testing.T) (http.Handler, []*http.Cookie) {
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
	serviceAccountService, err := serviceaccounts.NewService(serviceaccounts.NewMemoryStore(), "test-pepper")
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
	handler := server.NewHandler(
		server.WithDefaultScope(tenancy.DefaultScope()),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
		server.WithJobs(jobService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)

	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, sessionToken, _, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	return handler, []*http.Cookie{{Name: "bazusop_session", Value: sessionToken}}
}

func TestServiceAccountTokenAuthenticatesAndCreatesAJob(t *testing.T) {
	t.Parallel()
	handler, adminCookies := newServiceAccountHandler(t)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{
		"name": "ci-bot", "role": "operator",
	}))
	for _, cookie := range adminCookies {
		createRequest.AddCookie(cookie)
	}
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from service account creation, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var created struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil || created.Token == "" {
		t.Fatalf("expected a one-time token, got %v", err)
	}

	jobRequest := httptest.NewRequest(http.MethodPost, "/api/v1/instances/edge-01/jobs", encodeJSON(t, map[string]string{
		"action": "service.restart", "target": "nginx.service", "approved_by": "ci-bot", "reason": "automated rollout",
	}))
	jobRequest.Header.Set("Authorization", "Bearer "+created.Token)
	jobResponse := httptest.NewRecorder()
	handler.ServeHTTP(jobResponse, jobRequest)
	if jobResponse.Code != http.StatusCreated {
		t.Fatalf("expected the service account token to authenticate a real job creation, got %d: %s", jobResponse.Code, jobResponse.Body.String())
	}
}

func TestRevokedServiceAccountTokenIsRejected(t *testing.T) {
	t.Parallel()
	handler, adminCookies := newServiceAccountHandler(t)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{
		"name": "ci-bot", "role": "operator",
	}))
	for _, cookie := range adminCookies {
		createRequest.AddCookie(cookie)
	}
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	var created struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	_ = json.NewDecoder(createResponse.Body).Decode(&created)
	tokenID, _, ok := serviceaccounts.ParseToken(created.Token)
	if !ok {
		t.Fatal("expected a parseable token")
	}

	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/service-accounts/tokens/"+tokenID, nil)
	for _, cookie := range adminCookies {
		revokeRequest.AddCookie(cookie)
	}
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from token revocation, got %d: %s", revokeResponse.Code, revokeResponse.Body.String())
	}

	jobRequest := httptest.NewRequest(http.MethodPost, "/api/v1/instances/edge-01/jobs", encodeJSON(t, map[string]string{
		"action": "service.restart", "target": "nginx.service", "approved_by": "ci-bot", "reason": "automated rollout",
	}))
	jobRequest.Header.Set("Authorization", "Bearer "+created.Token)
	jobResponse := httptest.NewRecorder()
	handler.ServeHTTP(jobResponse, jobRequest)
	if jobResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected the revoked token to be rejected, got %d", jobResponse.Code)
	}
}

func TestServiceAccountTokenCannotManageOtherServiceAccounts(t *testing.T) {
	t.Parallel()
	handler, adminCookies := newServiceAccountHandler(t)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{
		"name": "ci-bot", "role": "site-admin",
	}))
	for _, cookie := range adminCookies {
		createRequest.AddCookie(cookie)
	}
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	var created struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(createResponse.Body).Decode(&created)

	escalationRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{
		"name": "second-bot", "role": "viewer",
	}))
	escalationRequest.Header.Set("Authorization", "Bearer "+created.Token)
	escalationResponse := httptest.NewRecorder()
	handler.ServeHTTP(escalationResponse, escalationRequest)
	if escalationResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected a service account token to be unable to manage service accounts, got %d: %s", escalationResponse.Code, escalationResponse.Body.String())
	}
}

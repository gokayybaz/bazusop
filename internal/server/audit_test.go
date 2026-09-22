package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func newAuditTrailHandler(t *testing.T) (http.Handler, *audittrail.Service, []*http.Cookie) {
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
	auditTrailService := audittrail.NewService(audittrail.NewMemoryStore())
	handler := server.NewHandler(
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
		server.WithAuditTrail(auditTrailService),
	)
	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, token, _, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	return handler, auditTrailService, []*http.Cookie{{Name: "bazusop_session", Value: token}}
}

func TestAuditTrailSearchRequiresPermissionAndFiltersResults(t *testing.T) {
	t.Parallel()
	handler, _, adminCookies := newAuditTrailHandler(t)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/audit/events", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", unauthenticated.Code)
	}

	// The bootstrap/session-creation and service-account calls above already
	// produced real audit_events rows via registerAudited; assert at least
	// one is visible to the platform admin, and that filtering by an
	// unmatched resource type returns none.
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events", nil)
	for _, cookie := range adminCookies {
		listRequest.AddCookie(cookie)
	}
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var payload struct {
		Events []audittrail.Event `json:"events"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&payload); err != nil || len(payload.Events) == 0 {
		t.Fatalf("expected at least one audit event, got %#v, %v", payload, err)
	}

	filteredOut := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?resource_type=no-such-resource", nil)
	for _, cookie := range adminCookies {
		filteredOut.AddCookie(cookie)
	}
	filteredResponse := httptest.NewRecorder()
	handler.ServeHTTP(filteredResponse, filteredOut)
	var filteredPayload struct {
		Events []audittrail.Event `json:"events"`
	}
	if err := json.NewDecoder(filteredResponse.Body).Decode(&filteredPayload); err != nil || len(filteredPayload.Events) != 0 {
		t.Fatalf("expected an unmatched resource_type filter to return no events, got %#v, %v", filteredPayload, err)
	}

	invalidSince := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?since=not-a-date", nil)
	for _, cookie := range adminCookies {
		invalidSince.AddCookie(cookie)
	}
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidSince)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid since timestamp, got %d", invalidResponse.Code)
	}
}

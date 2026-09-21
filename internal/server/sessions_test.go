package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/sessions"
)

func newSessionHandler(t *testing.T, adminToken string) (http.Handler, *identity.Service) {
	t.Helper()
	identityService := identity.NewService(identity.NewMemoryStore(), "test-totp-encryption-key")
	sessionService, err := sessions.NewService(sessions.NewMemoryStore(), identityService.IsUserActive)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.NewHandler(
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAdminToken(adminToken),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)
	return handler, identityService
}

func TestLoginRequiresAValidTOTPCode(t *testing.T) {
	t.Parallel()
	handler, _ := newSessionHandler(t, "admin-token")
	email, password := "admin@example.com", "correct horse battery staple"
	bootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapResponse, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "bootstrap-secret", "email": email, "password": password,
	})))
	if bootstrapResponse.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", bootstrapResponse.Code, bootstrapResponse.Body.String())
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": email, "password": password, "totp_code": "000000",
	})))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a wrong TOTP code, got %d: %s", response.Code, response.Body.String())
	}
}

func TestLoginWhoAmILogoutEndToEnd(t *testing.T) {
	t.Parallel()
	handler, _ := newSessionHandler(t, "admin-token")

	email, password := "admin@example.com", "correct horse battery staple"
	bootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapResponse, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "bootstrap-secret", "email": email, "password": password,
	})))
	if bootstrapResponse.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", bootstrapResponse.Code, bootstrapResponse.Body.String())
	}
	var bootstrapPayload struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.NewDecoder(bootstrapResponse.Body).Decode(&bootstrapPayload); err != nil || len(bootstrapPayload.RecoveryCodes) == 0 {
		t.Fatalf("expected recovery codes from bootstrap: %v", err)
	}

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": email, "password": password, "recovery_code": bootstrapPayload.RecoveryCodes[0],
	})))
	if loginResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from login, got %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
	var loginPayload struct {
		UserID    string `json:"user_id"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&loginPayload); err != nil || loginPayload.UserID == "" || loginPayload.CSRFToken == "" {
		t.Fatalf("expected a user id and CSRF token from login: %v", err)
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) < 2 {
		t.Fatalf("expected both the session and CSRF cookies to be set, got %d cookies", len(cookies))
	}

	whoAmIRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range cookies {
		whoAmIRequest.AddCookie(cookie)
	}
	whoAmIResponse := httptest.NewRecorder()
	handler.ServeHTTP(whoAmIResponse, whoAmIRequest)
	if whoAmIResponse.Code != http.StatusOK {
		t.Fatalf("expected 200 from whoami with a valid session cookie, got %d: %s", whoAmIResponse.Code, whoAmIResponse.Body.String())
	}

	logoutWithoutCSRF := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions", nil)
	for _, cookie := range cookies {
		logoutWithoutCSRF.AddCookie(cookie)
	}
	logoutWithoutCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutWithoutCSRFResponse, logoutWithoutCSRF)
	if logoutWithoutCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for logout without a CSRF header, got %d", logoutWithoutCSRFResponse.Code)
	}

	logoutRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions", nil)
	for _, cookie := range cookies {
		logoutRequest.AddCookie(cookie)
	}
	logoutRequest.Header.Set("X-CSRF-Token", loginPayload.CSRFToken)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from logout, got %d: %s", logoutResponse.Code, logoutResponse.Body.String())
	}

	whoAmIAfterLogout := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range cookies {
		whoAmIAfterLogout.AddCookie(cookie)
	}
	whoAmIAfterLogoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(whoAmIAfterLogoutResponse, whoAmIAfterLogout)
	if whoAmIAfterLogoutResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 from whoami after logout, got %d", whoAmIAfterLogoutResponse.Code)
	}
}

func TestAdminCanRevokeAllSessionsForAUser(t *testing.T) {
	t.Parallel()
	handler, _ := newSessionHandler(t, "admin-token")
	email, password := "admin@example.com", "correct horse battery staple"
	bootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapResponse, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "bootstrap-secret", "email": email, "password": password,
	})))
	var bootstrapPayload struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.NewDecoder(bootstrapResponse.Body).Decode(&bootstrapPayload)

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": email, "password": password, "recovery_code": bootstrapPayload.RecoveryCodes[0],
	})))
	var loginPayload struct {
		UserID string `json:"user_id"`
	}
	_ = json.NewDecoder(loginResponse.Body).Decode(&loginPayload)
	cookies := loginResponse.Result().Cookies()

	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+loginPayload.UserID+"/sessions", nil)
	revokeRequest.Header.Set("Authorization", "Bearer admin-token")
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from admin revoke, got %d: %s", revokeResponse.Code, revokeResponse.Body.String())
	}

	whoAmIRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range cookies {
		whoAmIRequest.AddCookie(cookie)
	}
	whoAmIResponse := httptest.NewRecorder()
	handler.ServeHTTP(whoAmIResponse, whoAmIRequest)
	if whoAmIResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected the revoked session to be rejected, got %d", whoAmIResponse.Code)
	}
}

func TestAdminCanRevokeAllSessionsOrgWide(t *testing.T) {
	t.Parallel()
	handler, _ := newSessionHandler(t, "admin-token")
	email, password := "admin@example.com", "correct horse battery staple"
	bootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapResponse, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "bootstrap-secret", "email": email, "password": password,
	})))
	var bootstrapPayload struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.NewDecoder(bootstrapResponse.Body).Decode(&bootstrapPayload)

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": email, "password": password, "recovery_code": bootstrapPayload.RecoveryCodes[0],
	})))
	cookies := loginResponse.Result().Cookies()

	revokeWithoutAuth := httptest.NewRecorder()
	handler.ServeHTTP(revokeWithoutAuth, httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/all", nil))
	if revokeWithoutAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for org-wide revoke without a token, got %d", revokeWithoutAuth.Code)
	}

	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/all", nil)
	revokeRequest.Header.Set("Authorization", "Bearer admin-token")
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from org-wide admin revoke, got %d: %s", revokeResponse.Code, revokeResponse.Body.String())
	}

	whoAmIRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range cookies {
		whoAmIRequest.AddCookie(cookie)
	}
	whoAmIResponse := httptest.NewRecorder()
	handler.ServeHTTP(whoAmIResponse, whoAmIRequest)
	if whoAmIResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected the org-wide-revoked session to be rejected, got %d", whoAmIResponse.Code)
	}
}

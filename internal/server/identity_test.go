package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/sessions"
)

func newIdentityHandler(t *testing.T, bootstrapSecret string) http.Handler {
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
	return server.NewHandler(
		server.WithIdentity(identityService, bootstrapSecret),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)
}

// bootstrapAndLogin bootstraps the first platform-admin over HTTP and logs
// them in with the first recovery code, returning the session cookies —
// the only way to authenticate a mutation now that spike 11.7 removed the
// legacy bearer bridge.
func bootstrapAndLogin(t *testing.T, handler http.Handler, bootstrapSecret, email, password string) []*http.Cookie {
	t.Helper()
	bootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapResponse, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": bootstrapSecret, "email": email, "password": password,
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
		t.Fatalf("login failed: %d %s", loginResponse.Code, loginResponse.Body.String())
	}
	return loginResponse.Result().Cookies()
}

// csrfTokenFromCookies extracts the CSRF cookie's value from a cookie
// slice returned by a real HTTP login (e.g. bootstrapAndLogin) or a
// manually-assembled test session — the CSRF cookie is deliberately
// non-HttpOnly (see setSessionCookies) so a real browser client can read
// it and echo it back as the X-CSRF-Token header; tests do the same.
func csrfTokenFromCookies(cookies []*http.Cookie) string {
	for _, cookie := range cookies {
		if cookie.Name == "bazusop_csrf" {
			return cookie.Value
		}
	}
	return ""
}

func TestBootstrapRequiresTheConfiguredSecret(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "wrong-secret", "email": "admin@example.com", "password": "correct horse battery staple",
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a wrong bootstrap secret, got %d: %s", response.Code, response.Body.String())
	}
}

func TestBootstrapInviteConsumeAndConfirmTOTPEndToEnd(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	email, password := "admin@example.com", "correct horse battery staple"
	cookies := bootstrapAndLogin(t, handler, "correct-secret", email, password)

	secondBootstrap := httptest.NewRecorder()
	handler.ServeHTTP(secondBootstrap, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "correct-secret", "email": "someone-else@example.com", "password": "another password entirely",
	})))
	if secondBootstrap.Code != http.StatusConflict {
		t.Fatalf("expected 409 on a second bootstrap attempt, got %d", secondBootstrap.Code)
	}

	inviteWithoutAuth := httptest.NewRecorder()
	handler.ServeHTTP(inviteWithoutAuth, httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-admin@example.com"})))
	if inviteWithoutAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invite creation without a session, got %d", inviteWithoutAuth.Code)
	}

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-admin@example.com"}))
	for _, cookie := range cookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(cookies))
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	if inviteResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from invite creation, got %d: %s", inviteResponse.Code, inviteResponse.Body.String())
	}
	var invitePayload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(inviteResponse.Body).Decode(&invitePayload); err != nil || invitePayload.Token == "" {
		t.Fatalf("expected a non-empty invite token, got %#v, %v", invitePayload, err)
	}

	consumeResponse := httptest.NewRecorder()
	handler.ServeHTTP(consumeResponse, httptest.NewRequest(http.MethodPost, "/api/v1/invites/"+invitePayload.Token+"/consume", encodeJSON(t, map[string]string{"password": "a brand new password"})))
	if consumeResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from invite consumption, got %d: %s", consumeResponse.Code, consumeResponse.Body.String())
	}
	var consumedUser struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(consumeResponse.Body).Decode(&consumedUser); err != nil || consumedUser.ID == "" {
		t.Fatalf("expected a user id, got %#v, %v", consumedUser, err)
	}

	badTOTP := httptest.NewRecorder()
	handler.ServeHTTP(badTOTP, httptest.NewRequest(http.MethodPost, "/api/v1/users/"+consumedUser.ID+"/confirm-totp", encodeJSON(t, map[string]string{"code": "000000"})))
	if badTOTP.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a wrong TOTP code, got %d: %s", badTOTP.Code, badTOTP.Body.String())
	}
}

func TestCreateInviteWithASiteRoleGrantsMembershipOnConsumption(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	cookies := bootstrapAndLogin(t, handler, "correct-secret", "admin@example.com", "correct horse battery staple")

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "operator@example.com", "role": "operator", "site_ids": []string{"site_default"},
	}))
	for _, cookie := range cookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(cookies))
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	if inviteResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from a site-role invite, got %d: %s", inviteResponse.Code, inviteResponse.Body.String())
	}
}

func TestCreateInviteRejectsAnInvalidRole(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	cookies := bootstrapAndLogin(t, handler, "correct-secret", "admin@example.com", "correct horse battery staple")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "broken@example.com", "role": "operator",
	}))
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	request.Header.Set("X-CSRF-Token", csrfTokenFromCookies(cookies))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a site role with no site_ids, got %d: %s", response.Code, response.Body.String())
	}
}

func TestConfirmTOTPIsRateLimitedPerSourceIP(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	cookies := bootstrapAndLogin(t, handler, "correct-secret", "admin@example.com", "correct horse battery staple")

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-user@example.com"}))
	for _, cookie := range cookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(cookies))
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	var invitePayload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(inviteResponse.Body).Decode(&invitePayload); err != nil || invitePayload.Token == "" {
		t.Fatalf("expected a non-empty invite token, got %#v, %v", invitePayload, err)
	}

	consumeResponse := httptest.NewRecorder()
	handler.ServeHTTP(consumeResponse, httptest.NewRequest(http.MethodPost, "/api/v1/invites/"+invitePayload.Token+"/consume", encodeJSON(t, map[string]string{"password": "a brand new password"})))
	var consumedUser struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(consumeResponse.Body).Decode(&consumedUser); err != nil || consumedUser.ID == "" {
		t.Fatalf("expected a user id, got %#v, %v", consumedUser, err)
	}

	for i := 0; i < 10; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/users/"+consumedUser.ID+"/confirm-totp", encodeJSON(t, map[string]string{"code": "000000"})))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: expected 400 for a wrong TOTP code within the rate limit, got %d", i+1, response.Code)
		}
	}

	limited := httptest.NewRecorder()
	handler.ServeHTTP(limited, httptest.NewRequest(http.MethodPost, "/api/v1/users/"+consumedUser.ID+"/confirm-totp", encodeJSON(t, map[string]string{"code": "000000"})))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the 11th attempt within 5 minutes to be rate limited, got %d", limited.Code)
	}
}

func TestConsumeInviteIsRateLimitedPerSourceIP(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	for i := 0; i < 10; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/invites/not-a-real-token/consume", encodeJSON(t, map[string]string{"password": "whatever"})))
		if response.Code != http.StatusNotFound {
			t.Fatalf("attempt %d: expected 404 for an unknown invite token within the rate limit, got %d", i+1, response.Code)
		}
	}

	limited := httptest.NewRecorder()
	handler.ServeHTTP(limited, httptest.NewRequest(http.MethodPost, "/api/v1/invites/not-a-real-token/consume", encodeJSON(t, map[string]string{"password": "whatever"})))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the 11th attempt within 5 minutes to be rate limited, got %d", limited.Code)
	}
}

func TestWhoAmIResponseNeverIncludesPasswordOrTOTPSecret(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	cookies := bootstrapAndLogin(t, handler, "correct-secret", "admin@example.com", "correct horse battery staple")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, forbidden := range []string{"correct horse battery staple", "password_hash", "totp_secret", "PasswordHash", "TOTPSecretEncrypted"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("whoami response leaked a secret field/value (%q): %s", forbidden, body)
		}
	}
}

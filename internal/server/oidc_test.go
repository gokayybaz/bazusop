package server_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

// mockIdP mirrors internal/oidc/oidc_test.go's helper — see that file's
// doc comment for why /authorize is never actually served.
type mockIdP struct {
	server            *httptest.Server
	key               *rsa.PrivateKey
	audience          string
	subject           string
	email             string
	expectedNonce     string
	expectedChallenge string
}

func newOIDCMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	idp := &mockIdP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"issuer": idp.server.URL, "authorization_endpoint": idp.server.URL + "/authorize",
			"token_endpoint": idp.server.URL + "/token", "jwks_uri": idp.server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &idp.key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(response http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		if idp.expectedChallenge != "" {
			sum := sha256.Sum256([]byte(request.FormValue("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != idp.expectedChallenge {
				http.Error(response, "pkce mismatch", http.StatusBadRequest)
				return
			}
		}
		claims := map[string]any{
			"iss": idp.server.URL, "sub": idp.subject, "aud": idp.audience,
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": idp.expectedNonce,
			"email": idp.email, "email_verified": true,
		}
		payload, err := json.Marshal(claims)
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		signed, err := signer.Sign(payload)
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		compact, err := signed.CompactSerialize()
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"access_token": "test-access-token", "token_type": "Bearer", "id_token": compact})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func newOIDCTestHandler(t *testing.T) (http.Handler, *identity.Service, *sessions.Service) {
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
	activityService := activity.NewService(activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore()))
	handler := server.NewHandler(
		server.WithDefaultScope(tenancy.DefaultScope()),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithActivity(activityService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)
	return handler, identityService, sessionService
}

func TestOIDCLoginPKCEFlowCreatesSessionAndLocalLoginStaysIndependent(t *testing.T) {
	t.Parallel()
	idp := newOIDCMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-abc", "sso-user@example.com"

	handler, _, _ := newOIDCTestHandler(t)
	adminEmail, adminPassword := "admin@example.com", "correct horse battery staple"
	adminCookies := bootstrapAndLogin(t, handler, "bootstrap-secret", adminEmail, adminPassword)

	configRequest := httptest.NewRequest(http.MethodPut, "/api/v1/organization/oidc", encodeJSON(t, map[string]string{
		"discovery_url": idp.server.URL + "/.well-known/openid-configuration",
		"issuer":        idp.server.URL, "client_id": "test-client", "client_secret": "test-secret",
		"redirect_url": "https://hub.example.com/api/v1/oidc/callback",
	}))
	for _, cookie := range adminCookies {
		configRequest.AddCookie(cookie)
	}
	configRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(adminCookies))
	configResponse := httptest.NewRecorder()
	handler.ServeHTTP(configResponse, configRequest)
	if configResponse.Code != http.StatusOK {
		t.Fatalf("set oidc configuration: %d %s", configResponse.Code, configResponse.Body.String())
	}
	if strings.Contains(configResponse.Body.String(), "test-secret") {
		t.Fatal("client secret must never appear in the configuration response")
	}

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "sso-user@example.com", "identity_type": "oidc",
	}))
	for _, cookie := range adminCookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(adminCookies))
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	if inviteResponse.Code != http.StatusCreated {
		t.Fatalf("create oidc invite: %d %s", inviteResponse.Code, inviteResponse.Body.String())
	}

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodGet, "/api/v1/oidc/login", nil))
	if loginResponse.Code != http.StatusFound {
		t.Fatalf("expected a redirect to the IdP, got %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
	authorizeURL, err := url.Parse(loginResponse.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	idp.expectedChallenge = authorizeURL.Query().Get("code_challenge")
	idp.expectedNonce = authorizeURL.Query().Get("nonce")
	flowCookies := loginResponse.Result().Cookies()

	callbackRequest := httptest.NewRequest(http.MethodGet, "/api/v1/oidc/callback?code=test-code&state="+authorizeURL.Query().Get("state"), nil)
	for _, cookie := range flowCookies {
		callbackRequest.AddCookie(cookie)
	}
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callbackRequest)
	if callbackResponse.Code != http.StatusFound {
		t.Fatalf("expected the callback to redirect after login, got %d: %s", callbackResponse.Code, callbackResponse.Body.String())
	}
	ssoCookies := callbackResponse.Result().Cookies()
	if len(ssoCookies) == 0 {
		t.Fatal("expected the callback to set session cookies")
	}

	whoAmIRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range ssoCookies {
		whoAmIRequest.AddCookie(cookie)
	}
	whoAmIResponse := httptest.NewRecorder()
	handler.ServeHTTP(whoAmIResponse, whoAmIRequest)
	if whoAmIResponse.Code != http.StatusOK {
		t.Fatalf("expected the sso session to be valid, got %d: %s", whoAmIResponse.Code, whoAmIResponse.Body.String())
	}
	var whoAmI struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(whoAmIResponse.Body).Decode(&whoAmI); err != nil || whoAmI.Email != "sso-user@example.com" {
		t.Fatalf("unexpected whoami: %#v, %v", whoAmI, err)
	}

	// Local login is entirely unaffected by OIDC being configured: a wrong
	// TOTP code is still evaluated independently and rejected the same way
	// it always was.
	localLoginResponse := httptest.NewRecorder()
	handler.ServeHTTP(localLoginResponse, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": adminEmail, "password": adminPassword, "totp_code": "000000",
	})))
	if localLoginResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected local login to keep working independently, got %d", localLoginResponse.Code)
	}
}

func TestOIDCCallbackRejectsAMismatchedState(t *testing.T) {
	t.Parallel()
	idp := newOIDCMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-1", "sso-user@example.com"
	handler, _, _ := newOIDCTestHandler(t)
	adminCookies := bootstrapAndLogin(t, handler, "bootstrap-secret", "admin@example.com", "correct horse battery staple")

	configRequest := httptest.NewRequest(http.MethodPut, "/api/v1/organization/oidc", encodeJSON(t, map[string]string{
		"discovery_url": idp.server.URL + "/.well-known/openid-configuration",
		"issuer":        idp.server.URL, "client_id": "test-client", "client_secret": "test-secret",
		"redirect_url": "https://hub.example.com/api/v1/oidc/callback",
	}))
	for _, cookie := range adminCookies {
		configRequest.AddCookie(cookie)
	}
	configRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(adminCookies))
	handler.ServeHTTP(httptest.NewRecorder(), configRequest)

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodGet, "/api/v1/oidc/login", nil))
	flowCookies := loginResponse.Result().Cookies()

	callbackRequest := httptest.NewRequest(http.MethodGet, "/api/v1/oidc/callback?code=test-code&state=a-forged-state", nil)
	for _, cookie := range flowCookies {
		callbackRequest.AddCookie(cookie)
	}
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callbackRequest)
	if callbackResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a mismatched state, got %d", callbackResponse.Code)
	}
}

func TestOIDCCallbackRejectsWithoutAPendingInvite(t *testing.T) {
	t.Parallel()
	idp := newOIDCMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-uninvited", "uninvited@example.com"
	handler, _, _ := newOIDCTestHandler(t)
	adminCookies := bootstrapAndLogin(t, handler, "bootstrap-secret", "admin@example.com", "correct horse battery staple")

	configRequest := httptest.NewRequest(http.MethodPut, "/api/v1/organization/oidc", encodeJSON(t, map[string]string{
		"discovery_url": idp.server.URL + "/.well-known/openid-configuration",
		"issuer":        idp.server.URL, "client_id": "test-client", "client_secret": "test-secret",
		"redirect_url": "https://hub.example.com/api/v1/oidc/callback",
	}))
	for _, cookie := range adminCookies {
		configRequest.AddCookie(cookie)
	}
	configRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(adminCookies))
	handler.ServeHTTP(httptest.NewRecorder(), configRequest)

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodGet, "/api/v1/oidc/login", nil))
	authorizeURL, err := url.Parse(loginResponse.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	idp.expectedChallenge = authorizeURL.Query().Get("code_challenge")
	idp.expectedNonce = authorizeURL.Query().Get("nonce")
	flowCookies := loginResponse.Result().Cookies()

	callbackRequest := httptest.NewRequest(http.MethodGet, "/api/v1/oidc/callback?code=test-code&state="+authorizeURL.Query().Get("state"), nil)
	for _, cookie := range flowCookies {
		callbackRequest.AddCookie(cookie)
	}
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callbackRequest)
	if callbackResponse.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without a pending oidc invite, got %d: %s", callbackResponse.Code, callbackResponse.Body.String())
	}
}

func TestOIDCConfigurationRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()
	handler, identityService, sessionService := newOIDCTestHandler(t)
	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, viewerToken, _, err := sessionService.Create(t.Context(), admin.ID+"-not-admin", admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/organization/oidc", nil)
	request.AddCookie(&http.Cookie{Name: "bazusop_session", Value: viewerToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unrelated/invalid session, got %d", response.Code)
	}
}

func TestOIDCLoginReturns404WhenNotConfigured(t *testing.T) {
	t.Parallel()
	handler, _, _ := newOIDCTestHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/oidc/login", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when OIDC is not configured, got %d", response.Code)
	}
}

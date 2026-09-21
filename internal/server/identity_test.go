package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/server"
)

func newIdentityHandler(t *testing.T, bootstrapSecret, adminToken string) http.Handler {
	t.Helper()
	service := identity.NewService(identity.NewMemoryStore(), "test-totp-encryption-key")
	return server.NewHandler(
		server.WithIdentity(service, bootstrapSecret),
		server.WithAdminToken(adminToken),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)
}

func TestBootstrapRequiresTheConfiguredSecret(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret", "admin-token")

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
	handler := newIdentityHandler(t, "correct-secret", "admin-token")

	bootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapResponse, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "correct-secret", "email": "admin@example.com", "password": "correct horse battery staple",
	})))
	if bootstrapResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from bootstrap, got %d: %s", bootstrapResponse.Code, bootstrapResponse.Body.String())
	}
	var bootstrapPayload struct {
		ProvisioningURI string `json:"provisioning_uri"`
	}
	if err := json.NewDecoder(bootstrapResponse.Body).Decode(&bootstrapPayload); err != nil {
		t.Fatalf("decode bootstrap response: %v", err)
	}
	if bootstrapPayload.ProvisioningURI == "" {
		t.Fatal("expected a non-empty provisioning URI")
	}

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
		t.Fatalf("expected 401 for invite creation without a token, got %d", inviteWithoutAuth.Code)
	}

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-admin@example.com"}))
	inviteRequest.Header.Set("Authorization", "Bearer admin-token")
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
	handler := newIdentityHandler(t, "correct-secret", "admin-token")

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "correct-secret", "email": "admin@example.com", "password": "correct horse battery staple",
	})))

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "operator@example.com", "role": "operator", "site_ids": []string{"site_default"},
	}))
	inviteRequest.Header.Set("Authorization", "Bearer admin-token")
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	if inviteResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from a site-role invite, got %d: %s", inviteResponse.Code, inviteResponse.Body.String())
	}
}

func TestCreateInviteRejectsAnInvalidRole(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret", "admin-token")

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "correct-secret", "email": "admin@example.com", "password": "correct horse battery staple",
	})))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "broken@example.com", "role": "operator",
	}))
	request.Header.Set("Authorization", "Bearer admin-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a site role with no site_ids, got %d: %s", response.Code, response.Body.String())
	}
}

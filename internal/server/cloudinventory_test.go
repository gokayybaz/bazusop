package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestCloudDiscoveryAPIReconcilesProviderInventory(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hosts := inventory.NewService(inventory.NewMemoryStore())
	if err := hosts.Report(ctx, tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, inventory.Facts{Hostname: "edge-01.example.com", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 4, MemoryBytes: 8 << 30, IPAddresses: []string{"10.0.0.8"}, AgentVersion: "0.1.0"}); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	cloud := cloudinventory.NewService(cloudinventory.NewMemoryStore(), hosts)

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
	handler := server.NewHandler(
		server.WithCloudInventory(cloud),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
	)

	admin, _, err := identityService.Bootstrap(ctx, tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, _, err := sessionService.Create(ctx, admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	createAccount := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{"name": "cloud-bot", "role": "site-admin"}))
	createAccount.AddCookie(&http.Cookie{Name: "bazusop_session", Value: adminToken})
	accountResponse := httptest.NewRecorder()
	handler.ServeHTTP(accountResponse, createAccount)
	if accountResponse.Code != http.StatusCreated {
		t.Fatalf("create service account: %d %s", accountResponse.Code, accountResponse.Body.String())
	}
	var account struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(accountResponse.Body).Decode(&account); err != nil {
		t.Fatalf("decode service account: %v", err)
	}

	create := httptest.NewRequest(http.MethodPost, "/api/v1/cloud/accounts", encodeJSON(t, cloudinventory.AccountRequest{Name: "Üretim AWS", Provider: cloudinventory.ProviderAWS, ExternalID: "123456789012"}))
	create.Header.Set("Authorization", "Bearer "+account.Token)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create account status = %d: %s", created.Code, created.Body.String())
	}
	var cloudAccount cloudinventory.Account
	if err := json.NewDecoder(created.Body).Decode(&cloudAccount); err != nil {
		t.Fatalf("decode account: %v", err)
	}

	syncRequest := httptest.NewRequest(http.MethodPut, "/api/v1/cloud/accounts/"+cloudAccount.ID+"/instances", encodeJSON(t, map[string]any{"instances": []cloudinventory.DiscoveredInstance{{ProviderInstanceID: "i-0123", Name: "edge-01", Region: "eu-central-1", State: "running", OSFamily: "linux", AgentIDHint: "agent-01"}}}))
	syncRequest.Header.Set("Authorization", "Bearer "+account.Token)
	synced := httptest.NewRecorder()
	handler.ServeHTTP(synced, syncRequest)
	if synced.Code != http.StatusOK {
		t.Fatalf("sync status = %d: %s", synced.Code, synced.Body.String())
	}

	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/api/v1/cloud/instances", nil))
	if listed.Code != http.StatusOK {
		t.Fatalf("list instances status = %d: %s", listed.Code, listed.Body.String())
	}
	var payload struct {
		Instances []cloudinventory.Instance `json:"instances"`
	}
	if err := json.NewDecoder(listed.Body).Decode(&payload); err != nil || len(payload.Instances) != 1 || payload.Instances[0].MatchStatus != cloudinventory.MatchVerified {
		t.Fatalf("instances = %#v, error = %v", payload.Instances, err)
	}
}

func TestCloudDiscoveryMutationsRequireAuthentication(t *testing.T) {
	t.Parallel()
	cloud := cloudinventory.NewService(cloudinventory.NewMemoryStore(), inventory.NewService(inventory.NewMemoryStore()))
	handler := server.NewHandler(server.WithCloudInventory(cloud))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/cloud/accounts", encodeJSON(t, cloudinventory.AccountRequest{})))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/server"
)

func TestCloudDiscoveryAPIReconcilesProviderInventory(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hosts := inventory.NewService(inventory.NewMemoryStore())
	if err := hosts.Report(ctx, "agent-01", inventory.Facts{Hostname: "edge-01.example.com", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 4, MemoryBytes: 8 << 30, IPAddresses: []string{"10.0.0.8"}, AgentVersion: "0.1.0"}); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	cloud := cloudinventory.NewService(cloudinventory.NewMemoryStore(), hosts)
	handler := server.NewHandler(server.WithCloudInventory(cloud, "operator-secret"))

	create := httptest.NewRequest(http.MethodPost, "/api/v1/cloud/accounts", encodeJSON(t, cloudinventory.AccountRequest{Name: "Üretim AWS", Provider: cloudinventory.ProviderAWS, ExternalID: "123456789012"}))
	create.Header.Set("Authorization", "Bearer operator-secret")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create account status = %d: %s", created.Code, created.Body.String())
	}
	var account cloudinventory.Account
	if err := json.NewDecoder(created.Body).Decode(&account); err != nil {
		t.Fatalf("decode account: %v", err)
	}

	syncRequest := httptest.NewRequest(http.MethodPut, "/api/v1/cloud/accounts/"+account.ID+"/instances", encodeJSON(t, map[string]any{"instances": []cloudinventory.DiscoveredInstance{{ProviderInstanceID: "i-0123", Name: "edge-01", Region: "eu-central-1", State: "running", OSFamily: "linux", AgentIDHint: "agent-01"}}}))
	syncRequest.Header.Set("Authorization", "Bearer operator-secret")
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

func TestCloudDiscoveryMutationsRequireOperatorToken(t *testing.T) {
	t.Parallel()
	cloud := cloudinventory.NewService(cloudinventory.NewMemoryStore(), inventory.NewService(inventory.NewMemoryStore()))
	handler := server.NewHandler(server.WithCloudInventory(cloud, "operator-secret"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/cloud/accounts", encodeJSON(t, cloudinventory.AccountRequest{})))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

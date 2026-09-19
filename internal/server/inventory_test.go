package server_test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestHumanInventoryUsesConfiguredScope(t *testing.T) {
	t.Parallel()
	scope := tenancy.Scope{OrganizationID: "org_custom", SiteID: "site_custom"}
	service := inventory.NewService(inventory.NewMemoryStore())
	if err := service.Report(t.Context(), tenancy.Agent{ID: "agent-custom", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, inventory.Facts{
		Hostname: "custom-edge", OSFamily: "linux", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024,
	}); err != nil {
		t.Fatalf("seed custom host: %v", err)
	}
	handler := server.NewHandler(server.WithInventory(service), server.WithDefaultScope(scope))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/instances?organization_id=org_other&site_id=site_other", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var payload struct {
		Instances []inventory.Host `json:"instances"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode instances: %v", err)
	}
	if len(payload.Instances) != 1 || payload.Instances[0].Scope() != scope {
		t.Fatalf("configured scope was not used: %#v", payload.Instances)
	}
}

func TestAgentReportIgnoresUntrustedSiteSelectors(t *testing.T) {
	t.Parallel()
	authority, identity := enrolledIdentity(t)
	service := inventory.NewService(inventory.NewMemoryStore())
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithInventory(service))
	facts := inventory.Facts{Hostname: "edge-01", OSFamily: "linux", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/agents/inventory?site_id=site_other", encodeJSON(t, facts))
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, identity.CertificatePEM)}}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	other, err := service.List(t.Context(), tenancy.Scope{OrganizationID: tenancy.DefaultOrganizationID, SiteID: "site_other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("query parameter changed trusted scope: %#v", other)
	}
	defaults, err := service.List(t.Context(), tenancy.DefaultScope())
	if err != nil || len(defaults) != 1 {
		t.Fatalf("default scoped report missing: %#v, %v", defaults, err)
	}

	spoofed := httptest.NewRequest(http.MethodPut, "/api/v1/agents/inventory", encodeJSON(t, map[string]any{
		"hostname": "edge-02", "os_family": "linux", "architecture": "amd64", "cpu_cores": 2, "memory_bytes": 1024, "site_id": "site_other",
	}))
	spoofed.TLS = request.TLS
	spoofedResponse := httptest.NewRecorder()
	handler.ServeHTTP(spoofedResponse, spoofed)
	if spoofedResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected unknown site field to be rejected with 400, got %d", spoofedResponse.Code)
	}
	defaults, err = service.List(t.Context(), tenancy.DefaultScope())
	if err != nil || len(defaults) != 1 {
		t.Fatalf("spoofed body caused an additional host write: %#v, %v", defaults, err)
	}
}

func TestAgentReportRejectsCertificateWithoutPersistentMapping(t *testing.T) {
	t.Parallel()
	store := newServerEnrollmentStateStore()
	authority, err := enrollment.NewPersistentAuthority(t.Context(), "persistent-bootstrap", tenancy.DefaultScope(), store)
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	identity, err := authority.Enroll(enrollment.Request{
		BootstrapToken: "persistent-bootstrap", Name: "edge-01", OperatingSystem: "linux", CSRPEM: serverCSR(t, "edge-01"),
	})
	if err != nil {
		t.Fatalf("enroll identity: %v", err)
	}
	store.deleteAgent(identity.AgentID)
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithInventory(inventory.NewService(inventory.NewMemoryStore())))
	request := httptest.NewRequest(http.MethodPut, "/api/v1/agents/inventory", encodeJSON(t, inventory.Facts{Hostname: "edge-01", OSFamily: "linux", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024}))
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, identity.CertificatePEM)}}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

type serverEnrollmentStateStore struct {
	mu       sync.Mutex
	state    enrollment.AuthorityState
	tokens   map[[sha256.Size]byte]tenancy.Scope
	consumed map[[sha256.Size]byte]bool
	agents   map[string]tenancy.Agent
}

func newServerEnrollmentStateStore() *serverEnrollmentStateStore {
	return &serverEnrollmentStateStore{tokens: make(map[[sha256.Size]byte]tenancy.Scope), consumed: make(map[[sha256.Size]byte]bool), agents: make(map[string]tenancy.Agent)}
}

func (store *serverEnrollmentStateStore) LoadOrCreateEnrollmentAuthority(_ context.Context, candidate enrollment.AuthorityState) (enrollment.AuthorityState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.state.CertificatePEM == "" {
		store.state = candidate
	}
	return store.state, nil
}

func (store *serverEnrollmentStateStore) RegisterEnrollmentToken(_ context.Context, hash [sha256.Size]byte, scope tenancy.Scope) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.tokens[hash] = scope
	return nil
}

func (store *serverEnrollmentStateStore) ConsumeEnrollmentToken(_ context.Context, hash [sha256.Size]byte, agentID string) (tenancy.Agent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	scope, ok := store.tokens[hash]
	if !ok {
		return tenancy.Agent{}, enrollment.ErrInvalidToken
	}
	if store.consumed[hash] {
		return tenancy.Agent{}, enrollment.ErrTokenConsumed
	}
	store.consumed[hash] = true
	agent := tenancy.Agent{ID: agentID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}
	store.agents[agentID] = agent
	return agent, nil
}

func (store *serverEnrollmentStateStore) ResolveAgent(_ context.Context, agentID string) (tenancy.Agent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	agent, ok := store.agents[agentID]
	if !ok {
		return tenancy.Agent{}, tenancy.ErrNotFound
	}
	return agent, nil
}

func (store *serverEnrollmentStateStore) deleteAgent(agentID string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.agents, agentID)
}

var _ enrollment.StateStore = (*serverEnrollmentStateStore)(nil)

func TestEnrolledAgentAppearsInInstanceInventory(t *testing.T) {
	t.Parallel()

	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	identity, err := authority.Enroll(enrollment.Request{
		BootstrapToken:  "bootstrap-secret",
		Name:            "edge-01",
		OperatingSystem: "linux",
		CSRPEM:          serverCSR(t, "edge-01"),
	})
	if err != nil {
		t.Fatalf("enroll identity: %v", err)
	}

	service := inventory.NewService(inventory.NewMemoryStore())
	handler := server.NewHandler(
		server.WithEnrollment(authority),
		server.WithInventory(service),
	)
	reportRequest := httptest.NewRequest(http.MethodPut, "/api/v1/agents/inventory", encodeJSON(t, inventory.Facts{
		Hostname:      "edge-01.example.com",
		OSFamily:      "linux",
		OSName:        "Ubuntu",
		OSVersion:     "24.04",
		Architecture:  "x86_64",
		KernelVersion: "6.8.0",
		CPUCores:      8,
		MemoryBytes:   16 * 1024 * 1024 * 1024,
		IPAddresses:   []string{"10.0.0.8"},
		AgentVersion:  "0.2.0",
	}))
	reportRequest.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, identity.CertificatePEM)}}
	reportResponse := httptest.NewRecorder()
	handler.ServeHTTP(reportResponse, reportRequest)
	if reportResponse.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNoContent, reportResponse.Code, reportResponse.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, listResponse.Code)
	}
	var payload struct {
		Instances []inventory.Host `json:"instances"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&payload); err != nil {
		t.Fatalf("decode instances: %v", err)
	}
	if len(payload.Instances) != 1 || payload.Instances[0].Hostname != "edge-01.example.com" {
		t.Fatalf("expected enrolled host in inventory, got %#v", payload.Instances)
	}
}

func TestInventoryReportRequiresAgentIdentity(t *testing.T) {
	t.Parallel()

	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	handler := server.NewHandler(
		server.WithEnrollment(authority),
		server.WithInventory(inventory.NewService(inventory.NewMemoryStore())),
	)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/agents/inventory", encodeJSON(t, inventory.Facts{}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

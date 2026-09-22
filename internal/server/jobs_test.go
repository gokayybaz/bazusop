package server_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func newServerJobService(t *testing.T, agents ...tenancy.Agent) *jobs.Service {
	t.Helper()
	registry := inventory.NewService(inventory.NewMemoryStore())
	for _, agent := range agents {
		if err := registry.Report(t.Context(), agent, inventory.Facts{Hostname: "host-" + agent.ID + "-" + agent.SiteID, OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	service, err := jobs.NewService(jobs.NewMemoryStore(), jobs.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestApprovedJobFlowsToAuthenticatedAgent(t *testing.T) {
	t.Parallel()
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	agentIdentity, err := authority.Enroll(enrollment.Request{BootstrapToken: "bootstrap-secret", Name: "edge-01", OperatingSystem: "linux", CSRPEM: serverCSR(t, "edge-01")})
	if err != nil {
		t.Fatalf("enroll identity: %v", err)
	}
	jobService := newServerJobService(t, tenancy.Agent{ID: agentIdentity.AgentID, OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})

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
		server.WithEnrollment(authority), server.WithJobs(jobService),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
	)

	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, adminCSRF, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	createAccount := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+tenancy.DefaultSiteID+"/service-accounts", encodeJSON(t, map[string]any{"name": "ci-bot", "role": "operator"}))
	createAccount.AddCookie(&http.Cookie{Name: "bazusop_session", Value: adminToken})
	createAccount.Header.Set("X-CSRF-Token", adminCSRF)
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

	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+agentIdentity.AgentID+"/jobs", encodeJSON(t, jobs.CreateRequest{
		Action: jobs.ActionServiceRestart, Target: "nginx.service", ApprovedBy: "gokay", Reason: "config rollout",
	}))
	createRequest.Header.Set("Authorization", "Bearer "+account.Token)
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var created jobs.Job
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode job: %v", err)
	}

	claim := httptest.NewRequest(http.MethodGet, "/api/v1/agents/jobs/next", nil)
	claim.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, agentIdentity.CertificatePEM)}}
	claimResponse := httptest.NewRecorder()
	handler.ServeHTTP(claimResponse, claim)
	if claimResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", claimResponse.Code, claimResponse.Body.String())
	}

	event := httptest.NewRequest(http.MethodPost, "/api/v1/agents/jobs/"+created.ID+"/events", encodeJSON(t, jobs.EventRequest{Sequence: 2, Type: jobs.EventSucceeded, Message: "nginx restarted"}))
	event.TLS = claim.TLS
	eventResponse := httptest.NewRecorder()
	handler.ServeHTTP(eventResponse, event)
	if eventResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", eventResponse.Code, eventResponse.Body.String())
	}

	auditResponse := httptest.NewRecorder()
	handler.ServeHTTP(auditResponse, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+agentIdentity.AgentID+"/jobs/"+created.ID+"/events", nil))
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", auditResponse.Code, auditResponse.Body.String())
	}
	var auditPayload struct {
		Events []jobs.Event `json:"events"`
	}
	if err := json.NewDecoder(auditResponse.Body).Decode(&auditPayload); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(auditPayload.Events) != 3 || auditPayload.Events[2].Type != jobs.EventSucceeded {
		t.Fatalf("unexpected audit: %#v", auditPayload.Events)
	}
}

func TestJobClaimRequiresAgentIdentity(t *testing.T) {
	t.Parallel()
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	jobService := newServerJobService(t)
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithJobs(jobService))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/agents/jobs/next", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestJobCreationRequiresAuthentication(t *testing.T) {
	t.Parallel()
	jobService := newServerJobService(t)
	handler := server.NewHandler(server.WithJobs(jobService))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/instances/agent-01/jobs", encodeJSON(t, jobs.CreateRequest{
		Action: jobs.ActionHostReboot, ApprovedBy: "gokay", Reason: "kernel rollout",
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

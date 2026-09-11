package server_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/server"
)

func TestApprovedJobFlowsToAuthenticatedAgent(t *testing.T) {
	t.Parallel()
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	identity, err := authority.Enroll(enrollment.Request{BootstrapToken: "bootstrap-secret", Name: "edge-01", OperatingSystem: "linux", CSRPEM: serverCSR(t, "edge-01")})
	if err != nil {
		t.Fatalf("enroll identity: %v", err)
	}
	jobService, err := jobs.NewService(jobs.NewMemoryStore())
	if err != nil {
		t.Fatalf("create job service: %v", err)
	}
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithJobs(jobService, "operator-secret"))

	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+identity.AgentID+"/jobs", encodeJSON(t, jobs.CreateRequest{
		Action: jobs.ActionServiceRestart, Target: "nginx.service", ApprovedBy: "gokay", Reason: "config rollout",
	}))
	createRequest.Header.Set("Authorization", "Bearer operator-secret")
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var created jobs.Job
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode job: %v", err)
	}

	claim := httptest.NewRequest(http.MethodGet, "/api/v1/agents/jobs/next", nil)
	claim.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, identity.CertificatePEM)}}
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
	handler.ServeHTTP(auditResponse, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+identity.AgentID+"/jobs/"+created.ID+"/events", nil))
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", auditResponse.Code, auditResponse.Body.String())
	}
	var audit struct {
		Events []jobs.Event `json:"events"`
	}
	if err := json.NewDecoder(auditResponse.Body).Decode(&audit); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(audit.Events) != 3 || audit.Events[2].Type != jobs.EventSucceeded {
		t.Fatalf("unexpected audit: %#v", audit.Events)
	}
}

func TestJobClaimRequiresAgentIdentity(t *testing.T) {
	t.Parallel()
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	jobService, err := jobs.NewService(jobs.NewMemoryStore())
	if err != nil {
		t.Fatalf("create job service: %v", err)
	}
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithJobs(jobService, "operator-secret"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/agents/jobs/next", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestJobCreationRequiresOperatorToken(t *testing.T) {
	t.Parallel()
	jobService, err := jobs.NewService(jobs.NewMemoryStore())
	if err != nil {
		t.Fatalf("create job service: %v", err)
	}
	handler := server.NewHandler(server.WithJobs(jobService, "operator-secret"))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/instances/agent-01/jobs", encodeJSON(t, jobs.CreateRequest{
		Action: jobs.ActionHostReboot, ApprovedBy: "gokay", Reason: "kernel rollout",
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestJobCreationIsUnavailableWithoutConfiguredOperatorToken(t *testing.T) {
	t.Parallel()
	jobService, err := jobs.NewService(jobs.NewMemoryStore())
	if err != nil {
		t.Fatalf("create job service: %v", err)
	}
	handler := server.NewHandler(server.WithJobs(jobService, ""))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/instances/agent-01/jobs", encodeJSON(t, jobs.CreateRequest{
		Action: jobs.ActionHostReboot, ApprovedBy: "gokay", Reason: "kernel rollout",
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}
}

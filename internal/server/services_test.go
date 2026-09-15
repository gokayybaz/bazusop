package server_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestEnrolledAgentReportsFilterableServices(t *testing.T) {
	t.Parallel()
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	identity, err := authority.Enroll(enrollment.Request{
		BootstrapToken: "bootstrap-secret", Name: "edge-01", OperatingSystem: "linux", CSRPEM: serverCSR(t, "edge-01"),
	})
	if err != nil {
		t.Fatalf("enroll identity: %v", err)
	}
	service := serviceinventory.NewService(serviceinventory.NewMemoryStore())
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithServiceInventory(service))

	reportRequest := httptest.NewRequest(http.MethodPut, "/api/v1/agents/services", encodeJSON(t, serviceinventory.Snapshot{
		ObservedAt: time.Date(2026, 9, 11, 5, 0, 0, 0, time.UTC),
		Services: []serviceinventory.Fact{
			{Name: "nginx.service", DisplayName: "NGINX Web Server", State: "running", StartupType: "enabled"},
			{Name: "postgresql.service", DisplayName: "PostgreSQL", State: "stopped", StartupType: "manual"},
		},
	}))
	reportRequest.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, identity.CertificatePEM)}}
	reportResponse := httptest.NewRecorder()
	handler.ServeHTTP(reportResponse, reportRequest)
	if reportResponse.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNoContent, reportResponse.Code, reportResponse.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+identity.AgentID+"/services?state=running&q=web", nil)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, listResponse.Code, listResponse.Body.String())
	}
	var payload struct {
		Services []serviceinventory.Service `json:"services"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&payload); err != nil {
		t.Fatalf("decode services: %v", err)
	}
	if len(payload.Services) != 1 || payload.Services[0].Name != "nginx.service" {
		t.Fatalf("expected filtered nginx service, got %#v", payload.Services)
	}
	if payload.Services[0].AgentID != identity.AgentID || payload.Services[0].OrganizationID != tenancy.DefaultOrganizationID || payload.Services[0].SiteID != tenancy.DefaultSiteID {
		t.Fatalf("untrusted service scope: %#v", payload.Services)
	}
}

func TestServiceReportRequiresAgentIdentity(t *testing.T) {
	t.Parallel()
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	handler := server.NewHandler(
		server.WithEnrollment(authority),
		server.WithServiceInventory(serviceinventory.NewService(serviceinventory.NewMemoryStore())),
	)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/agents/services", encodeJSON(t, serviceinventory.Snapshot{}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

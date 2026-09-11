package server_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/server"
)

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

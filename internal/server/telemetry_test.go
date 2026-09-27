package server_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestAgentTelemetryReachesInstanceHistory(t *testing.T) {
	t.Parallel()

	authority, identity := enrolledIdentity(t)
	service := telemetry.NewService(telemetry.NewMemoryStore())
	handler := server.NewHandler(
		server.WithEnrollment(authority),
		server.WithTelemetry(service),
	)
	recordedAt := time.Date(2026, time.September, 11, 4, 0, 0, 0, time.UTC)
	report := httptest.NewRequest(http.MethodPost, "/api/v1/agents/telemetry", encodeJSON(t, telemetry.Sample{
		AgentID: "forged-agent", OrganizationID: "forged-org", SiteID: "forged-site",
		RecordedAt:     recordedAt,
		CPUPercent:     42.5,
		MemoryPercent:  63.4,
		DiskPercent:    71.1,
		NetworkRXBytes: 1024,
		NetworkTXBytes: 512,
	}))
	report.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, identity.CertificatePEM)}}
	reportResponse := httptest.NewRecorder()
	handler.ServeHTTP(reportResponse, report)
	if reportResponse.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNoContent, reportResponse.Code, reportResponse.Body.String())
	}

	query := url.Values{
		"from":            []string{recordedAt.Add(-time.Minute).Format(time.RFC3339)},
		"to":              []string{recordedAt.Add(time.Minute).Format(time.RFC3339)},
		"limit":           []string{"100"},
		"organization_id": []string{"forged-org"}, "site_id": []string{"forged-site"},
	}
	history := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+identity.AgentID+"/telemetry?"+query.Encode(), nil)
	historyResponse := httptest.NewRecorder()
	handler.ServeHTTP(historyResponse, history)
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, historyResponse.Code, historyResponse.Body.String())
	}
	var payload struct {
		Latest  *telemetry.Sample  `json:"latest"`
		Samples []telemetry.Sample `json:"samples"`
	}
	if err := json.NewDecoder(historyResponse.Body).Decode(&payload); err != nil {
		t.Fatalf("decode telemetry history: %v", err)
	}
	if payload.Latest == nil || payload.Latest.CPUPercent != 42.5 || len(payload.Samples) != 1 {
		t.Fatalf("expected current and historical telemetry, got %#v", payload)
	}
	if payload.Latest.AgentID != identity.AgentID || payload.Latest.OrganizationID != tenancy.DefaultOrganizationID || payload.Latest.SiteID != tenancy.DefaultSiteID {
		t.Fatalf("untrusted telemetry scope: %#v", payload.Latest)
	}
}

func TestTelemetryReportRequiresAgentIdentity(t *testing.T) {
	t.Parallel()

	authority, _ := enrolledIdentity(t)
	handler := server.NewHandler(
		server.WithEnrollment(authority),
		server.WithTelemetry(telemetry.NewService(telemetry.NewMemoryStore())),
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/telemetry", encodeJSON(t, telemetry.Sample{}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func enrolledIdentity(t *testing.T) (*enrollment.Authority, enrollment.Identity) {
	t.Helper()

	authority, err := enrollment.NewAuthority("telemetry-bootstrap")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	identity, err := authority.Enroll(enrollment.Request{
		BootstrapToken:  "telemetry-bootstrap",
		Name:            "edge-01",
		OperatingSystem: "linux",
		CSRPEM:          serverCSR(t, "edge-01"),
	})
	if err != nil {
		t.Fatalf("enroll identity: %v", err)
	}
	return authority, identity
}

func TestFleetAverageEndpointReflectsRecentTelemetryOnly(t *testing.T) {
	t.Parallel()

	service := telemetry.NewService(telemetry.NewMemoryStore())
	handler := server.NewHandler(
		server.WithDefaultScope(tenancy.DefaultScope()),
		server.WithTelemetry(service),
	)
	scope := tenancy.DefaultScope()
	now := time.Now().UTC()

	if err := service.Report(t.Context(), tenancy.Agent{ID: "fresh-agent", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, telemetry.Sample{RecordedAt: now, CPUPercent: 80, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatal(err)
	}
	if err := service.Report(t.Context(), tenancy.Agent{ID: "stale-agent", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, telemetry.Sample{RecordedAt: now.Add(-10 * time.Minute), CPUPercent: 20, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/fleet-average", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		AverageCPUPercent float64 `json:"average_cpu_percent"`
		DeviceCount       int     `json:"device_count"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode fleet average: %v", err)
	}
	if payload.DeviceCount != 1 || payload.AverageCPUPercent != 80 {
		t.Fatalf("expected only the fresh agent counted (80%%, 1 device), got %#v", payload)
	}
}

func TestFleetAverageEndpointReturnsZeroDevicesForAnEmptyFleet(t *testing.T) {
	t.Parallel()

	handler := server.NewHandler(
		server.WithDefaultScope(tenancy.DefaultScope()),
		server.WithTelemetry(telemetry.NewService(telemetry.NewMemoryStore())),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/fleet-average", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		AverageCPUPercent float64 `json:"average_cpu_percent"`
		DeviceCount       int     `json:"device_count"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode fleet average: %v", err)
	}
	if payload.DeviceCount != 0 || payload.AverageCPUPercent != 0 {
		t.Fatalf("expected a zero-value response for an empty fleet, got %#v", payload)
	}
}

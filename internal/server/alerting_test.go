package server_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/telemetry"
)

func TestMetricRuleCreatesAndAcknowledgesIncident(t *testing.T) {
	t.Parallel()
	authority, identity := enrolledIdentity(t)
	alerts := alerting.NewService(alerting.NewMemoryStore())
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithTelemetry(telemetry.NewService(telemetry.NewMemoryStore())), server.WithAlerts(alerts, "operator-secret"))

	createRule := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", encodeJSON(t, alerting.RuleRequest{Name: "Yüksek CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}))
	createRule.Header.Set("Authorization", "Bearer operator-secret")
	ruleResponse := httptest.NewRecorder()
	handler.ServeHTTP(ruleResponse, createRule)
	if ruleResponse.Code != http.StatusCreated {
		t.Fatalf("expected rule 201, got %d: %s", ruleResponse.Code, ruleResponse.Body.String())
	}

	recordedAt := time.Now().UTC()
	report := httptest.NewRequest(http.MethodPost, "/api/v1/agents/telemetry", encodeJSON(t, telemetry.Sample{RecordedAt: recordedAt, CPUPercent: 97, MemoryPercent: 50, DiskPercent: 40}))
	report.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, identity.CertificatePEM)}}
	reportResponse := httptest.NewRecorder()
	handler.ServeHTTP(reportResponse, report)
	if reportResponse.Code != http.StatusNoContent {
		t.Fatalf("expected telemetry 204, got %d", reportResponse.Code)
	}
	older := httptest.NewRequest(http.MethodPost, "/api/v1/agents/telemetry", encodeJSON(t, telemetry.Sample{RecordedAt: recordedAt.Add(-time.Hour), CPUPercent: 20, MemoryPercent: 50, DiskPercent: 40}))
	older.TLS = report.TLS
	olderResponse := httptest.NewRecorder()
	handler.ServeHTTP(olderResponse, older)
	if olderResponse.Code != http.StatusNoContent {
		t.Fatalf("expected historical telemetry 204, got %d", olderResponse.Code)
	}

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil))
	var payload struct {
		Incidents []alerting.Incident `json:"incidents"`
	}
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected incidents 200, got %d", listResponse.Code)
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&payload); err != nil || len(payload.Incidents) != 1 || payload.Incidents[0].Status != alerting.StatusOpen {
		t.Fatalf("decode incidents: %#v, %v", payload, err)
	}

	ack := httptest.NewRequest(http.MethodPost, "/api/v1/incidents/"+payload.Incidents[0].ID+"/acknowledge", encodeJSON(t, map[string]string{"actor": "gokay"}))
	ack.Header.Set("Authorization", "Bearer operator-secret")
	ackResponse := httptest.NewRecorder()
	handler.ServeHTTP(ackResponse, ack)
	if ackResponse.Code != http.StatusOK {
		t.Fatalf("expected acknowledge 200, got %d: %s", ackResponse.Code, ackResponse.Body.String())
	}
}

func TestAlertMutationsRequireOperatorToken(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(server.WithAlerts(alerting.NewService(alerting.NewMemoryStore()), "operator-secret"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", encodeJSON(t, alerting.RuleRequest{})))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

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
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func newAlertService(t *testing.T) *alerting.Service {
	t.Helper()
	registry := inventory.NewService(inventory.NewMemoryStore())
	service, err := alerting.NewService(alerting.NewMemoryStore(), alerting.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestMetricRuleCreatesAndAcknowledgesIncident(t *testing.T) {
	t.Parallel()
	authority, agentIdentity := enrolledIdentity(t)
	alerts := newAlertService(t)

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
		server.WithEnrollment(authority), server.WithTelemetry(telemetry.NewService(telemetry.NewMemoryStore())), server.WithAlerts(alerts),
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
	createAccount := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+tenancy.DefaultSiteID+"/service-accounts", encodeJSON(t, map[string]any{"name": "ops-bot", "role": "site-admin"}))
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

	createRule := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", encodeJSON(t, alerting.RuleRequest{Name: "Yüksek CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}))
	createRule.Header.Set("Authorization", "Bearer "+account.Token)
	ruleResponse := httptest.NewRecorder()
	handler.ServeHTTP(ruleResponse, createRule)
	if ruleResponse.Code != http.StatusCreated {
		t.Fatalf("expected rule 201, got %d: %s", ruleResponse.Code, ruleResponse.Body.String())
	}

	recordedAt := time.Now().UTC()
	report := httptest.NewRequest(http.MethodPost, "/api/v1/agents/telemetry", encodeJSON(t, telemetry.Sample{RecordedAt: recordedAt, CPUPercent: 97, MemoryPercent: 50, DiskPercent: 40}))
	report.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, agentIdentity.CertificatePEM)}}
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
	ack.Header.Set("Authorization", "Bearer "+account.Token)
	ackResponse := httptest.NewRecorder()
	handler.ServeHTTP(ackResponse, ack)
	if ackResponse.Code != http.StatusOK {
		t.Fatalf("expected acknowledge 200, got %d: %s", ackResponse.Code, ackResponse.Body.String())
	}
}

func TestAlertMutationsRequireAuthentication(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(server.WithAlerts(newAlertService(t)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", encodeJSON(t, alerting.RuleRequest{})))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

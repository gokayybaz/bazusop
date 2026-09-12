package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/server"
)

func TestRuntimeConfigurationExposesSafeRetentionState(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(server.WithRuntimeConfiguration(server.RuntimeConfiguration{
		Storage: "postgresql", TimescaleEnabled: true, TelemetryRetentionDays: 90, LogRetentionDays: 21,
		Version: "0.3.0", Commit: "abc123def456", BuildDate: "2026-09-12T09:30:00Z",
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system/configuration", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var configuration server.RuntimeConfiguration
	if err := json.NewDecoder(response.Body).Decode(&configuration); err != nil {
		t.Fatalf("decode configuration: %v", err)
	}
	if configuration.Storage != "postgresql" || !configuration.TimescaleEnabled || configuration.TelemetryRetentionDays != 90 || configuration.LogRetentionDays != 21 {
		t.Fatalf("unexpected runtime configuration: %#v", configuration)
	}
	if configuration.Version != "0.3.0" || configuration.Commit != "abc123def456" || configuration.BuildDate != "2026-09-12T09:30:00Z" {
		t.Fatalf("unexpected build identity: %#v", configuration)
	}
}

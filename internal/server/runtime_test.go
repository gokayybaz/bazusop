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
}

package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/server"
)

func TestCORSOmitsHeadersWhenNoTrustedOriginsAreConfigured(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Origin", "https://anywhere.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected the request to still succeed, got %d", response.Code)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected no CORS header when no trusted origins are configured")
	}
}

func TestCORSAllowsAConfiguredTrustedOrigin(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
		server.WithTrustedOrigins([]string{"https://ui.example"}),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Origin", "https://ui.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Header().Get("Access-Control-Allow-Origin") != "https://ui.example" {
		t.Fatalf("expected the trusted origin to be echoed back, got %q", response.Header().Get("Access-Control-Allow-Origin"))
	}
	if response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("expected credentials to be allowed for a trusted origin")
	}
}

func TestCORSRejectsAPreflightFromAnUntrustedOrigin(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
		server.WithTrustedOrigins([]string{"https://ui.example"}),
	)

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/health", nil)
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("Access-Control-Request-Method", "GET")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a preflight from an untrusted origin, got %d", response.Code)
	}
}

func TestCORSHandlesAPreflightFromATrustedOrigin(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
		server.WithTrustedOrigins([]string{"https://ui.example"}),
	)

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/health", nil)
	request.Header.Set("Origin", "https://ui.example")
	request.Header.Set("Access-Control-Request-Method", "POST")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for a trusted preflight, got %d", response.Code)
	}
	if response.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatal("expected Access-Control-Allow-Methods to be set")
	}
}

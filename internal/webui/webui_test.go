package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesApplicationRoutes(t *testing.T) {
	t.Parallel()

	for _, route := range []string{
		"/fleet",
		"/services",
		"/metrics",
		"/logs",
		"/jobs",
		"/alerts",
		"/cloud",
		"/audit",
		"/settings",
	} {
		route := route
		t.Run(route, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, route, nil)
			response := httptest.NewRecorder()
			Handler().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d", route, response.Code, http.StatusOK)
			}
			if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
				t.Fatalf("GET %s Content-Type = %q, want text/html", route, contentType)
			}
			if !strings.Contains(response.Body.String(), `<div id="root"></div>`) {
				t.Fatalf("GET %s did not return the application shell", route)
			}
		})
	}
}

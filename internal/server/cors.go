package server

import "net/http"

func newTrustedOrigins(origins []string) map[string]bool {
	trusted := make(map[string]bool, len(origins))
	for _, origin := range origins {
		if origin != "" {
			trusted[origin] = true
		}
	}
	return trusted
}

// corsMiddleware never blocks a request from reaching next — browsers
// already withhold the response body from cross-origin JS unless
// Access-Control-Allow-Origin matches, so omitting that header for an
// untrusted origin is sufficient for GET-style requests. The one case this
// middleware actively rejects is a CORS preflight (OPTIONS with
// Access-Control-Request-Method) from an untrusted origin, since answering
// it at all would let the browser proceed with the real cross-origin
// request. State-changing same-site requests are separately defended by
// the CSRF token check in internal/server/sessions.go — CORS here is
// defense in depth, not the primary control.
func corsMiddleware(trustedOrigins map[string]bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(response, request)
			return
		}
		isPreflight := request.Method == http.MethodOptions && request.Header.Get("Access-Control-Request-Method") != ""
		if !trustedOrigins[origin] {
			if isPreflight {
				http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			next.ServeHTTP(response, request)
			return
		}
		response.Header().Set("Access-Control-Allow-Origin", origin)
		response.Header().Set("Access-Control-Allow-Credentials", "true")
		response.Header().Set("Vary", "Origin")
		if isPreflight {
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			response.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, Authorization")
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}

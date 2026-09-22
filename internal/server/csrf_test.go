package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMutationsRequireACSRFTokenEvenWithAValidSession(t *testing.T) {
	t.Parallel()
	handler, identityService, _, sessionService := newRBACHandler(t)
	admin, _, err := identityService.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, token, csrfToken, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}

	missing := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	missing.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with no X-CSRF-Token header, got %d: %s", missingResponse.Code, missingResponse.Body.String())
	}

	wrong := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	wrong.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	wrong.Header.Set("X-CSRF-Token", "not-the-real-token")
	wrongResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongResponse, wrong)
	if wrongResponse.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with a wrong X-CSRF-Token, got %d: %s", wrongResponse.Code, wrongResponse.Body.String())
	}

	correct := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	correct.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	correct.Header.Set("X-CSRF-Token", csrfToken)
	correctResponse := httptest.NewRecorder()
	handler.ServeHTTP(correctResponse, correct)
	if correctResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 with the correct X-CSRF-Token, got %d: %s", correctResponse.Code, correctResponse.Body.String())
	}
}

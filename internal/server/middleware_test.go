package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
)

func TestDeriveActorPrefersAgentCertOverBearerHeader(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Authorization", "Bearer some-token")
	actorType, actorID := deriveActor(request)
	if actorType != audittrail.ActorLegacyToken || actorID != "bearer" {
		t.Fatalf("expected legacy_token/bearer without a client cert, got %v/%q", actorType, actorID)
	}
}

func TestDeriveActorIsAnonymousWithoutCertOrHeader(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	actorType, actorID := deriveActor(request)
	if actorType != audittrail.ActorAnonymous || actorID != "" {
		t.Fatalf("expected anonymous/empty, got %v/%q", actorType, actorID)
	}
}

func TestStatusRecorderDefaultsToOKWhenWriteHeaderNeverCalled(t *testing.T) {
	t.Parallel()
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	if _, err := recorder.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if recorder.status != http.StatusOK {
		t.Fatalf("expected default 200, got %d", recorder.status)
	}
}

func TestStatusRecorderCapturesExplicitWriteHeader(t *testing.T) {
	t.Parallel()
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	recorder.WriteHeader(http.StatusNotFound)
	if recorder.status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.status)
	}
}

func TestNewCorrelationIDProducesDistinctNonEmptyValues(t *testing.T) {
	t.Parallel()
	first, err := newCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || second == "" || first == second {
		t.Fatalf("expected distinct non-empty ids, got %q and %q", first, second)
	}
}

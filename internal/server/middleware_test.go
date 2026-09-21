package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/tenancy"
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

func TestRegisterAuditedRecordsOneEventPerRequestWithPathValues(t *testing.T) {
	t.Parallel()
	store := audittrail.NewMemoryStore()
	service := audittrail.NewService(store)
	mux := http.NewServeMux()
	scope := tenancy.DefaultScope()

	registerAudited(mux, "/api/v1/instances/{agentID}/jobs/{jobID}/events", http.MethodGet, "job_events", []string{"agentID", "jobID"}, service, scope,
		func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNotFound) })

	request := httptest.NewRequest(http.MethodGet, "/api/v1/instances/agent-1/jobs/job-1/events", nil)
	request.Header.Set("User-Agent", "test-client")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected the wrapped handler's status to pass through, got %d", response.Code)
	}
	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	event := events[0]
	if event.Action != http.MethodGet || event.ResourceType != "job_events" || event.ResourceID != "agent-1/job-1" {
		t.Fatalf("unexpected event shape: %#v", event)
	}
	if event.Outcome != audittrail.OutcomeFailure || event.ErrorCode != "404" {
		t.Fatalf("expected failure/404, got outcome=%v error_code=%q", event.Outcome, event.ErrorCode)
	}
	if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
		t.Fatalf("expected the configured scope on the event, got %#v", event)
	}
	if event.CorrelationID == "" {
		t.Fatal("expected a non-empty correlation id")
	}
	if event.UserAgent != "test-client" {
		t.Fatalf("expected the request's User-Agent to be captured, got %q", event.UserAgent)
	}
	if event.ActorType != audittrail.ActorAnonymous {
		t.Fatalf("expected anonymous actor for a request with no cert or auth header, got %v", event.ActorType)
	}
}

func TestRegisterAuditedIsANoOpWhenServiceIsNil(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	called := false
	registerAudited(mux, "/api/v1/health", http.MethodGet, "health", nil, nil, tenancy.DefaultScope(),
		func(response http.ResponseWriter, _ *http.Request) {
			called = true
			response.WriteHeader(http.StatusOK)
		})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if !called {
		t.Fatal("expected the wrapped handler to still run with a nil audit service")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
}

package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestActivityEventsRequireAuthentication(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(server.WithActivity(activity.NewService(activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore()))))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/activity/events", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestActivityEventsMergeAllSourcesOrderedByRecency(t *testing.T) {
	t.Parallel()
	scope := tenancy.DefaultScope()
	agent := tenancy.Agent{ID: "edge-01", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}

	registry := inventory.NewService(inventory.NewMemoryStore())
	if err := registry.Report(t.Context(), agent, inventory.Facts{Hostname: "edge-01", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}); err != nil {
		t.Fatal(err)
	}

	jobStore := jobs.NewMemoryStore()
	jobService, err := jobs.NewService(jobStore, jobs.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobService.Create(t.Context(), scope, agent.ID, jobs.CreateRequest{Action: jobs.ActionServiceRestart, Target: "nginx.service", ApprovedBy: "gokay", Reason: "config rollout"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	alertStore := alerting.NewMemoryStore()
	alertService, err := alerting.NewService(alertStore, alerting.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := alertService.CreateRule(t.Context(), scope, alerting.RuleRequest{Name: "Yüksek CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := alertService.EvaluateTelemetry(t.Context(), scope, agent.ID, alerting.Telemetry{CPUPercent: 97}); err != nil {
		t.Fatal(err)
	}

	activityService := activity.NewService(activity.NewMemoryStore(jobStore, alertStore))

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
		server.WithDefaultScope(scope),
		server.WithActivity(activityService),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)

	admin, _, err := identityService.Bootstrap(t.Context(), scope.OrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, _, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	adminCookie := &http.Cookie{Name: "bazusop_session", Value: adminToken}

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-viewer@example.com"}))
	inviteRequest.AddCookie(adminCookie)
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	if inviteResponse.Code != http.StatusCreated {
		t.Fatalf("create invite: %d %s", inviteResponse.Code, inviteResponse.Body.String())
	}

	assignRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+scope.SiteID+"/memberships", encodeJSON(t, map[string]string{"user_id": admin.ID, "role": "viewer"}))
	assignRequest.AddCookie(adminCookie)
	assignResponse := httptest.NewRecorder()
	handler.ServeHTTP(assignResponse, assignRequest)
	if assignResponse.Code != http.StatusNoContent {
		t.Fatalf("assign role: %d %s", assignResponse.Code, assignResponse.Body.String())
	}

	createAccountRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+scope.SiteID+"/service-accounts", encodeJSON(t, map[string]any{"name": "ci-bot", "role": "operator"}))
	createAccountRequest.AddCookie(adminCookie)
	createAccountResponse := httptest.NewRecorder()
	handler.ServeHTTP(createAccountResponse, createAccountRequest)
	if createAccountResponse.Code != http.StatusCreated {
		t.Fatalf("create service account: %d %s", createAccountResponse.Code, createAccountResponse.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/activity/events", nil)
	listRequest.AddCookie(adminCookie)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var payload struct {
		Events []activity.Event `json:"events"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&payload); err != nil {
		t.Fatalf("decode activity events: %v", err)
	}
	if len(payload.Events) != 5 {
		t.Fatalf("expected 5 merged events (job, alert, identity, site_role, service_account), got %#v", payload.Events)
	}
	seenSources := map[activity.Source]bool{}
	for _, event := range payload.Events {
		seenSources[event.Source] = true
	}
	for _, source := range []activity.Source{activity.SourceJob, activity.SourceAlert, activity.SourceIdentity, activity.SourceSiteRole, activity.SourceServiceAccount} {
		if !seenSources[source] {
			t.Fatalf("expected source %q in merged activity, got %#v", source, payload.Events)
		}
	}
	if payload.Events[len(payload.Events)-1].ReferenceID != job.ID {
		t.Fatalf("expected the job event to be the oldest (last), got %#v", payload.Events)
	}
}

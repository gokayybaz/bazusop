package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestOperatorCannotChangeAdministrativePolicy(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(
		server.WithAlerts(alerting.NewService(alerting.NewMemoryStore()), "operator-secret"),
		server.WithCloudInventory(cloudinventory.NewService(cloudinventory.NewMemoryStore(), inventory.NewService(inventory.NewMemoryStore())), "operator-secret"),
		server.WithAdminToken("admin-secret"),
	)

	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/alert-rules"},
		{http.MethodPost, "/api/v1/maintenance-windows"},
		{http.MethodPost, "/api/v1/cloud/accounts"},
		{http.MethodPut, "/api/v1/cloud/accounts/account-01/instances"},
	} {
		request := httptest.NewRequest(endpoint.method, endpoint.path, encodeJSON(t, map[string]any{}))
		request.Header.Set("Authorization", "Bearer operator-secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected operator to receive 403, got %d: %s", endpoint.method, endpoint.path, response.Code, response.Body.String())
		}
	}
}

func TestAdminCanChangePolicyAndRunOperations(t *testing.T) {
	t.Parallel()
	jobService := newServerJobService(t, tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})
	handler := server.NewHandler(
		server.WithJobs(jobService, "operator-secret"),
		server.WithAlerts(alerting.NewService(alerting.NewMemoryStore()), "operator-secret"),
		server.WithAdminToken("admin-secret"),
	)

	policyRequest := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", encodeJSON(t, alerting.RuleRequest{
		Name: "Yuksek CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU,
		Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true,
	}))
	policyRequest.Header.Set("Authorization", "Bearer admin-secret")
	policyResponse := httptest.NewRecorder()
	handler.ServeHTTP(policyResponse, policyRequest)
	if policyResponse.Code != http.StatusCreated {
		t.Fatalf("expected admin policy change 201, got %d: %s", policyResponse.Code, policyResponse.Body.String())
	}

	jobRequest := httptest.NewRequest(http.MethodPost, "/api/v1/instances/agent-01/jobs", encodeJSON(t, jobs.CreateRequest{
		Action: jobs.ActionHostReboot, ApprovedBy: "admin", Reason: "kernel rollout",
	}))
	jobRequest.Header.Set("Authorization", "Bearer admin-secret")
	jobResponse := httptest.NewRecorder()
	handler.ServeHTTP(jobResponse, jobRequest)
	if jobResponse.Code != http.StatusCreated {
		t.Fatalf("expected admin operation 201, got %d: %s", jobResponse.Code, jobResponse.Body.String())
	}
}

func TestUnknownBearerTokenIsUnauthorizedForAdminRoute(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(
		server.WithAlerts(alerting.NewService(alerting.NewMemoryStore()), "operator-secret"),
		server.WithAdminToken("admin-secret"),
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", encodeJSON(t, alerting.RuleRequest{}))
	request.Header.Set("Authorization", "Bearer unknown-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unknown token to receive 401, got %d", response.Code)
	}
}

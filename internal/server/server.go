package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/audit"
	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
	"github.com/gokayybaz/bazusop/internal/webui"
)

type Option func(*handlerOptions)

type handlerOptions struct {
	scope                tenancy.Scope
	enrollmentAuthority  *enrollment.Authority
	inventoryService     *inventory.Service
	telemetryService     *telemetry.Service
	serviceInventory     *serviceinventory.Manager
	logService           *logstream.Service
	jobService           *jobs.Service
	operatorToken        string
	adminToken           string
	alertService         *alerting.Service
	cloudInventory       *cloudinventory.Service
	auditService         *audit.Service
	runtimeConfiguration *RuntimeConfiguration
}

func WithDefaultScope(scope tenancy.Scope) Option {
	return func(options *handlerOptions) {
		options.scope = scope
	}
}

func NewHandler(options ...Option) http.Handler {
	configuration := handlerOptions{scope: tenancy.DefaultScope()}
	for _, option := range options {
		option(&configuration)
	}
	if err := configuration.scope.Validate(); err != nil {
		panic(fmt.Sprintf("invalid handler scope: %v", err))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", handleHealth)
	if configuration.runtimeConfiguration != nil {
		mux.HandleFunc("GET /api/v1/system/configuration", handleRuntimeConfiguration(*configuration.runtimeConfiguration))
	}
	if configuration.enrollmentAuthority != nil {
		mux.HandleFunc("POST /api/v1/agents/enroll", handleEnroll(configuration.enrollmentAuthority))
		mux.HandleFunc("POST /api/v1/agents/renew", handleRenew(configuration.enrollmentAuthority))
	}
	if configuration.inventoryService != nil {
		mux.HandleFunc("GET /api/v1/instances", handleListInstances(configuration.inventoryService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("PUT /api/v1/agents/inventory", handleInventoryReport(configuration.enrollmentAuthority, configuration.inventoryService))
		}
	}
	if configuration.telemetryService != nil {
		mux.HandleFunc("GET /api/v1/instances/{agentID}/telemetry", handleTelemetryHistory(configuration.telemetryService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("POST /api/v1/agents/telemetry", handleTelemetryReport(configuration.enrollmentAuthority, configuration.telemetryService, configuration.alertService))
		}
	}
	if configuration.serviceInventory != nil {
		mux.HandleFunc("GET /api/v1/instances/{agentID}/services", handleListServices(configuration.serviceInventory, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("PUT /api/v1/agents/services", handleServiceReport(configuration.enrollmentAuthority, configuration.serviceInventory))
		}
	}
	if configuration.logService != nil {
		mux.HandleFunc("GET /api/v1/instances/{agentID}/logs", handleSearchLogs(configuration.logService, configuration.scope))
		mux.HandleFunc("GET /api/v1/instances/{agentID}/logs/stream", handleStreamLogs(configuration.logService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("POST /api/v1/agents/logs", handleLogIngest(configuration.enrollmentAuthority, configuration.logService))
		}
	}
	if configuration.jobService != nil {
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		mux.HandleFunc("POST /api/v1/instances/{agentID}/jobs", handleCreateJob(configuration.jobService, tokens, configuration.scope))
		mux.HandleFunc("GET /api/v1/instances/{agentID}/jobs", handleListJobs(configuration.jobService, configuration.scope))
		mux.HandleFunc("GET /api/v1/instances/{agentID}/jobs/{jobID}/events", handleJobEvents(configuration.jobService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("GET /api/v1/agents/jobs/next", handleClaimJob(configuration.enrollmentAuthority, configuration.jobService))
			mux.HandleFunc("POST /api/v1/agents/jobs/{jobID}/events", handleReportJobEvent(configuration.enrollmentAuthority, configuration.jobService))
		}
	}
	if configuration.alertService != nil {
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		mux.HandleFunc("GET /api/v1/alert-rules", handleListAlertRules(configuration.alertService, configuration.scope))
		mux.HandleFunc("POST /api/v1/alert-rules", handleCreateAlertRule(configuration.alertService, tokens, configuration.scope))
		mux.HandleFunc("GET /api/v1/maintenance-windows", handleListMaintenance(configuration.alertService, configuration.scope))
		mux.HandleFunc("POST /api/v1/maintenance-windows", handleCreateMaintenance(configuration.alertService, tokens, configuration.scope))
		mux.HandleFunc("GET /api/v1/incidents", handleListIncidents(configuration.alertService, configuration.scope))
		mux.HandleFunc("GET /api/v1/incidents/{incidentID}/events", handleAlertEvents(configuration.alertService, configuration.scope))
		mux.HandleFunc("POST /api/v1/incidents/{incidentID}/acknowledge", handleAcknowledgeIncident(configuration.alertService, tokens, configuration.scope))
	}
	if configuration.cloudInventory != nil {
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		mux.HandleFunc("GET /api/v1/cloud/accounts", handleListCloudAccounts(configuration.cloudInventory, configuration.scope))
		mux.HandleFunc("POST /api/v1/cloud/accounts", handleCreateCloudAccount(configuration.cloudInventory, tokens, configuration.scope))
		mux.HandleFunc("GET /api/v1/cloud/instances", handleListCloudInstances(configuration.cloudInventory, configuration.scope))
		mux.HandleFunc("PUT /api/v1/cloud/accounts/{accountID}/instances", handleReconcileCloudInstances(configuration.cloudInventory, tokens, configuration.scope))
	}
	if configuration.auditService != nil {
		mux.HandleFunc("GET /api/v1/audit/events", handleListAuditEvents(configuration.auditService, configuration.scope))
	}
	mux.HandleFunc("/api/", func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	})
	mux.Handle("/", webui.Handler())
	return mux
}

func handleHealth(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(map[string]string{
		"service": "bazusop-hub",
		"status":  "ok",
	})
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return err
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

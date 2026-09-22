package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
	"github.com/gokayybaz/bazusop/internal/webui"
)

type Option func(*handlerOptions)

type handlerOptions struct {
	scope                  tenancy.Scope
	enrollmentAuthority    *enrollment.Authority
	inventoryService       *inventory.Service
	telemetryService       *telemetry.Service
	serviceInventory       *serviceinventory.Manager
	logService             *logstream.Service
	jobService             *jobs.Service
	alertService           *alerting.Service
	cloudInventory         *cloudinventory.Service
	activityService        *activity.Service
	auditTrail             *audittrail.Service
	runtimeConfiguration   *RuntimeConfiguration
	identityService        *identity.Service
	bootstrapSecret        string
	trustedOrigins         []string
	sessionService         *sessions.Service
	sessionIdentityService *identity.Service
	authorizationService   *authorization.Service
	serviceAccountService  *serviceaccounts.Service
}

func WithDefaultScope(scope tenancy.Scope) Option {
	return func(options *handlerOptions) {
		options.scope = scope
	}
}

func WithAuditTrail(service *audittrail.Service) Option {
	return func(options *handlerOptions) {
		options.auditTrail = service
	}
}

func WithTrustedOrigins(origins []string) Option {
	return func(options *handlerOptions) { options.trustedOrigins = origins }
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
	registerAudited(mux, "/api/v1/health", http.MethodGet, "health", nil, configuration.auditTrail, configuration.scope, handleHealth)
	if configuration.runtimeConfiguration != nil {
		registerAudited(mux, "/api/v1/system/configuration", http.MethodGet, "system_configuration", nil, configuration.auditTrail, configuration.scope, handleRuntimeConfiguration(*configuration.runtimeConfiguration))
	}
	if configuration.enrollmentAuthority != nil {
		registerAudited(mux, "/api/v1/agents/enroll", http.MethodPost, "enrollment", nil, configuration.auditTrail, configuration.scope, handleEnroll(configuration.enrollmentAuthority))
		registerAudited(mux, "/api/v1/agents/renew", http.MethodPost, "enrollment_renewal", nil, configuration.auditTrail, configuration.scope, handleRenew(configuration.enrollmentAuthority))
	}
	if configuration.inventoryService != nil {
		registerAudited(mux, "/api/v1/instances", http.MethodGet, "inventory", nil, configuration.auditTrail, configuration.scope, handleListInstances(configuration.inventoryService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			registerAudited(mux, "/api/v1/agents/inventory", http.MethodPut, "inventory", nil, configuration.auditTrail, configuration.scope, handleInventoryReport(configuration.enrollmentAuthority, configuration.inventoryService))
		}
	}
	if configuration.telemetryService != nil {
		registerAudited(mux, "/api/v1/instances/{agentID}/telemetry", http.MethodGet, "telemetry", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleTelemetryHistory(configuration.telemetryService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			registerAudited(mux, "/api/v1/agents/telemetry", http.MethodPost, "telemetry", nil, configuration.auditTrail, configuration.scope, handleTelemetryReport(configuration.enrollmentAuthority, configuration.telemetryService, configuration.alertService))
		}
	}
	if configuration.serviceInventory != nil {
		registerAudited(mux, "/api/v1/instances/{agentID}/services", http.MethodGet, "services", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleListServices(configuration.serviceInventory, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			registerAudited(mux, "/api/v1/agents/services", http.MethodPut, "services", nil, configuration.auditTrail, configuration.scope, handleServiceReport(configuration.enrollmentAuthority, configuration.serviceInventory))
		}
	}
	if configuration.logService != nil {
		registerAudited(mux, "/api/v1/instances/{agentID}/logs", http.MethodGet, "logs", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleSearchLogs(configuration.logService, configuration.scope))
		registerAudited(mux, "/api/v1/instances/{agentID}/logs/stream", http.MethodGet, "log_stream", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleStreamLogs(configuration.logService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			registerAudited(mux, "/api/v1/agents/logs", http.MethodPost, "logs", nil, configuration.auditTrail, configuration.scope, handleLogIngest(configuration.enrollmentAuthority, configuration.logService))
		}
	}
	if configuration.jobService != nil {
		registerAudited(mux, "/api/v1/instances/{agentID}/jobs", http.MethodPost, "jobs", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleCreateJob(configuration.jobService, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, configuration.scope))
		registerAudited(mux, "/api/v1/instances/{agentID}/jobs", http.MethodGet, "jobs", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleListJobs(configuration.jobService, configuration.scope))
		registerAudited(mux, "/api/v1/instances/{agentID}/jobs/{jobID}/events", http.MethodGet, "job_events", []string{"agentID", "jobID"}, configuration.auditTrail, configuration.scope, handleJobEvents(configuration.jobService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			registerAudited(mux, "/api/v1/agents/jobs/next", http.MethodGet, "jobs", nil, configuration.auditTrail, configuration.scope, handleClaimJob(configuration.enrollmentAuthority, configuration.jobService))
			registerAudited(mux, "/api/v1/agents/jobs/{jobID}/events", http.MethodPost, "job_events", []string{"jobID"}, configuration.auditTrail, configuration.scope, handleReportJobEvent(configuration.enrollmentAuthority, configuration.jobService))
		}
	}
	if configuration.alertService != nil {
		registerAudited(mux, "/api/v1/alert-rules", http.MethodGet, "alert_rules", nil, configuration.auditTrail, configuration.scope, handleListAlertRules(configuration.alertService, configuration.scope))
		registerAudited(mux, "/api/v1/alert-rules", http.MethodPost, "alert_rules", nil, configuration.auditTrail, configuration.scope, handleCreateAlertRule(configuration.alertService, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, configuration.scope))
		registerAudited(mux, "/api/v1/maintenance-windows", http.MethodGet, "maintenance_windows", nil, configuration.auditTrail, configuration.scope, handleListMaintenance(configuration.alertService, configuration.scope))
		registerAudited(mux, "/api/v1/maintenance-windows", http.MethodPost, "maintenance_windows", nil, configuration.auditTrail, configuration.scope, handleCreateMaintenance(configuration.alertService, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, configuration.scope))
		registerAudited(mux, "/api/v1/incidents", http.MethodGet, "incidents", nil, configuration.auditTrail, configuration.scope, handleListIncidents(configuration.alertService, configuration.scope))
		registerAudited(mux, "/api/v1/incidents/{incidentID}/events", http.MethodGet, "incident_events", []string{"incidentID"}, configuration.auditTrail, configuration.scope, handleAlertEvents(configuration.alertService, configuration.scope))
		registerAudited(mux, "/api/v1/incidents/{incidentID}/acknowledge", http.MethodPost, "incidents", []string{"incidentID"}, configuration.auditTrail, configuration.scope, handleAcknowledgeIncident(configuration.alertService, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, configuration.scope))
	}
	if configuration.cloudInventory != nil {
		registerAudited(mux, "/api/v1/cloud/accounts", http.MethodGet, "cloud_accounts", nil, configuration.auditTrail, configuration.scope, handleListCloudAccounts(configuration.cloudInventory, configuration.scope))
		registerAudited(mux, "/api/v1/cloud/accounts", http.MethodPost, "cloud_accounts", nil, configuration.auditTrail, configuration.scope, handleCreateCloudAccount(configuration.cloudInventory, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, configuration.scope))
		registerAudited(mux, "/api/v1/cloud/instances", http.MethodGet, "cloud_instances", nil, configuration.auditTrail, configuration.scope, handleListCloudInstances(configuration.cloudInventory, configuration.scope))
		registerAudited(mux, "/api/v1/cloud/accounts/{accountID}/instances", http.MethodPut, "cloud_instances", []string{"accountID"}, configuration.auditTrail, configuration.scope, handleReconcileCloudInstances(configuration.cloudInventory, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, configuration.scope))
	}
	if configuration.activityService != nil {
		registerAudited(mux, "/api/v1/activity/events", http.MethodGet, "activity_timeline", nil, configuration.auditTrail, configuration.scope, handleListActivityEvents(configuration.activityService, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, configuration.scope))
	}
	if configuration.identityService != nil {
		registerAudited(mux, "/api/v1/bootstrap", http.MethodPost, "bootstrap", nil, configuration.auditTrail, configuration.scope, handleBootstrap(configuration.identityService, configuration.bootstrapSecret))
		registerAudited(mux, "/api/v1/users/invites", http.MethodPost, "invites", nil, configuration.auditTrail, configuration.scope, handleCreateInvite(configuration.identityService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
		registerAudited(mux, "/api/v1/invites/{token}/consume", http.MethodPost, "invites", []string{"token"}, configuration.auditTrail, configuration.scope, handleConsumeInvite(configuration.identityService))
		registerAudited(mux, "/api/v1/users/{userID}/confirm-totp", http.MethodPost, "users", []string{"userID"}, configuration.auditTrail, configuration.scope, handleConfirmTOTP(configuration.identityService))
	}
	if configuration.sessionService != nil {
		registerAudited(mux, "/api/v1/sessions", http.MethodPost, "sessions", nil, configuration.auditTrail, configuration.scope, handleLogin(configuration.sessionIdentityService, configuration.sessionService))
		registerAudited(mux, "/api/v1/session", http.MethodGet, "sessions", nil, configuration.auditTrail, configuration.scope, handleWhoAmI(configuration.sessionService, configuration.sessionIdentityService))
		registerAudited(mux, "/api/v1/sessions", http.MethodDelete, "sessions", nil, configuration.auditTrail, configuration.scope, handleLogout(configuration.sessionService))
		registerAudited(mux, "/api/v1/users/{userID}/sessions", http.MethodDelete, "sessions", []string{"userID"}, configuration.auditTrail, configuration.scope, handleRevokeUserSessions(configuration.sessionService, configuration.authorizationService, configuration.scope))
		registerAudited(mux, "/api/v1/sessions/all", http.MethodDelete, "sessions", nil, configuration.auditTrail, configuration.scope, handleRevokeAllSessions(configuration.sessionService, configuration.authorizationService, configuration.scope))
	}
	if configuration.authorizationService != nil {
		registerAudited(mux, "/api/v1/sites/{siteID}/memberships", http.MethodPost, "site_memberships", []string{"siteID"}, configuration.auditTrail, configuration.scope, handleAssignSiteRole(configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
		registerAudited(mux, "/api/v1/sites/{siteID}/memberships/{userID}", http.MethodDelete, "site_memberships", []string{"siteID", "userID"}, configuration.auditTrail, configuration.scope, handleRevokeSiteRole(configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
	}
	if configuration.serviceAccountService != nil {
		registerAudited(mux, "/api/v1/sites/{siteID}/service-accounts", http.MethodPost, "service_accounts", []string{"siteID"}, configuration.auditTrail, configuration.scope, handleCreateServiceAccount(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
		registerAudited(mux, "/api/v1/sites/{siteID}/service-accounts", http.MethodGet, "service_accounts", []string{"siteID"}, configuration.auditTrail, configuration.scope, handleListServiceAccounts(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, configuration.scope))
		registerAudited(mux, "/api/v1/service-accounts/{accountID}/rotate", http.MethodPost, "service_accounts", []string{"accountID"}, configuration.auditTrail, configuration.scope, handleRotateServiceAccountToken(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
		registerAudited(mux, "/api/v1/service-accounts/tokens/{tokenID}", http.MethodDelete, "service_accounts", []string{"tokenID"}, configuration.auditTrail, configuration.scope, handleRevokeServiceAccountToken(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
		registerAudited(mux, "/api/v1/service-accounts/{accountID}", http.MethodDelete, "service_accounts", []string{"accountID"}, configuration.auditTrail, configuration.scope, handleDisableServiceAccount(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
	}
	mux.HandleFunc("/api/", func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	})
	mux.Handle("/", webui.Handler())
	return corsMiddleware(newTrustedOrigins(configuration.trustedOrigins), mux)
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

package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/webui"
)

type Option func(*handlerOptions)

type handlerOptions struct {
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
	runtimeConfiguration *RuntimeConfiguration
}

type RuntimeConfiguration struct {
	Storage                string `json:"storage"`
	TimescaleEnabled       bool   `json:"timescale_enabled"`
	TelemetryRetentionDays int    `json:"telemetry_retention_days"`
	LogRetentionDays       int    `json:"log_retention_days"`
	Version                string `json:"version"`
	Commit                 string `json:"commit"`
	BuildDate              string `json:"build_date"`
}

type accessRole uint8

const (
	roleOperator accessRole = iota + 1
	roleAdmin
)

type accessTokens struct {
	operator string
	admin    string
}

func WithInventory(service *inventory.Service) Option {
	return func(options *handlerOptions) {
		options.inventoryService = service
	}
}

func WithEnrollment(authority *enrollment.Authority) Option {
	return func(options *handlerOptions) {
		options.enrollmentAuthority = authority
	}
}

func WithTelemetry(service *telemetry.Service) Option {
	return func(options *handlerOptions) {
		options.telemetryService = service
	}
}

func WithServiceInventory(service *serviceinventory.Manager) Option {
	return func(options *handlerOptions) {
		options.serviceInventory = service
	}
}

func WithLogs(service *logstream.Service) Option {
	return func(options *handlerOptions) {
		options.logService = service
	}
}

func WithJobs(service *jobs.Service, operatorToken string) Option {
	return func(options *handlerOptions) {
		options.jobService = service
		options.operatorToken = operatorToken
	}
}

func WithAlerts(service *alerting.Service, operatorToken string) Option {
	return func(options *handlerOptions) {
		options.alertService = service
		options.operatorToken = operatorToken
	}
}

func WithCloudInventory(service *cloudinventory.Service, operatorToken string) Option {
	return func(options *handlerOptions) {
		options.cloudInventory = service
		options.operatorToken = operatorToken
	}
}

// WithAdminToken separates administrative policy changes from routine operations.
// When omitted, the operator token remains valid for both roles for compatibility.
func WithAdminToken(adminToken string) Option {
	return func(options *handlerOptions) {
		options.adminToken = adminToken
	}
}

func WithRuntimeConfiguration(configuration RuntimeConfiguration) Option {
	return func(options *handlerOptions) {
		options.runtimeConfiguration = &configuration
	}
}

func NewHandler(options ...Option) http.Handler {
	configuration := handlerOptions{}
	for _, option := range options {
		option(&configuration)
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
		mux.HandleFunc("GET /api/v1/instances", handleListInstances(configuration.inventoryService))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("PUT /api/v1/agents/inventory", handleInventoryReport(configuration.enrollmentAuthority, configuration.inventoryService))
		}
	}
	if configuration.telemetryService != nil {
		mux.HandleFunc("GET /api/v1/instances/{agentID}/telemetry", handleTelemetryHistory(configuration.telemetryService))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("POST /api/v1/agents/telemetry", handleTelemetryReport(configuration.enrollmentAuthority, configuration.telemetryService, configuration.alertService))
		}
	}
	if configuration.serviceInventory != nil {
		mux.HandleFunc("GET /api/v1/instances/{agentID}/services", handleListServices(configuration.serviceInventory))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("PUT /api/v1/agents/services", handleServiceReport(configuration.enrollmentAuthority, configuration.serviceInventory))
		}
	}
	if configuration.logService != nil {
		mux.HandleFunc("GET /api/v1/instances/{agentID}/logs", handleSearchLogs(configuration.logService))
		mux.HandleFunc("GET /api/v1/instances/{agentID}/logs/stream", handleStreamLogs(configuration.logService))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("POST /api/v1/agents/logs", handleLogIngest(configuration.enrollmentAuthority, configuration.logService))
		}
	}
	if configuration.jobService != nil {
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		mux.HandleFunc("POST /api/v1/instances/{agentID}/jobs", handleCreateJob(configuration.jobService, tokens))
		mux.HandleFunc("GET /api/v1/instances/{agentID}/jobs", handleListJobs(configuration.jobService))
		mux.HandleFunc("GET /api/v1/instances/{agentID}/jobs/{jobID}/events", handleJobEvents(configuration.jobService))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("GET /api/v1/agents/jobs/next", handleClaimJob(configuration.enrollmentAuthority, configuration.jobService))
			mux.HandleFunc("POST /api/v1/agents/jobs/{jobID}/events", handleReportJobEvent(configuration.enrollmentAuthority, configuration.jobService))
		}
	}
	if configuration.alertService != nil {
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		mux.HandleFunc("GET /api/v1/alert-rules", handleListAlertRules(configuration.alertService))
		mux.HandleFunc("POST /api/v1/alert-rules", handleCreateAlertRule(configuration.alertService, tokens))
		mux.HandleFunc("GET /api/v1/maintenance-windows", handleListMaintenance(configuration.alertService))
		mux.HandleFunc("POST /api/v1/maintenance-windows", handleCreateMaintenance(configuration.alertService, tokens))
		mux.HandleFunc("GET /api/v1/incidents", handleListIncidents(configuration.alertService))
		mux.HandleFunc("GET /api/v1/incidents/{incidentID}/events", handleAlertEvents(configuration.alertService))
		mux.HandleFunc("POST /api/v1/incidents/{incidentID}/acknowledge", handleAcknowledgeIncident(configuration.alertService, tokens))
	}
	if configuration.cloudInventory != nil {
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		mux.HandleFunc("GET /api/v1/cloud/accounts", handleListCloudAccounts(configuration.cloudInventory))
		mux.HandleFunc("POST /api/v1/cloud/accounts", handleCreateCloudAccount(configuration.cloudInventory, tokens))
		mux.HandleFunc("GET /api/v1/cloud/instances", handleListCloudInstances(configuration.cloudInventory))
		mux.HandleFunc("PUT /api/v1/cloud/accounts/{accountID}/instances", handleReconcileCloudInstances(configuration.cloudInventory, tokens))
	}
	mux.HandleFunc("/api/", func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	})
	mux.Handle("/", webui.Handler())
	return mux
}

func handleRuntimeConfiguration(configuration RuntimeConfiguration) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, configuration)
	}
}

func handleHealth(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(map[string]string{
		"service": "bazusop-hub",
		"status":  "ok",
	})
}

func handleEnroll(authority *enrollment.Authority) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !secureEnrollmentTransport(request) {
			http.Error(response, http.StatusText(http.StatusUpgradeRequired), http.StatusUpgradeRequired)
			return
		}
		var enrollmentRequest enrollment.Request
		if err := decodeJSON(response, request, &enrollmentRequest); err != nil {
			return
		}
		identity, err := authority.Enroll(enrollmentRequest)
		switch {
		case errors.Is(err, enrollment.ErrInvalidToken):
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		case errors.Is(err, enrollment.ErrTokenConsumed):
			http.Error(response, http.StatusText(http.StatusConflict), http.StatusConflict)
			return
		case errors.Is(err, enrollment.ErrInvalidRequest):
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		case err != nil:
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, identity)
	}
}

func secureEnrollmentTransport(request *http.Request) bool {
	if request.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return false
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func handleRenew(authority *enrollment.Authority) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		var renewalRequest struct {
			CSRPEM string `json:"csr"`
		}
		if err := decodeJSON(response, request, &renewalRequest); err != nil {
			return
		}
		identity, err := authority.Renew(request.TLS.PeerCertificates[0], renewalRequest.CSRPEM)
		if errors.Is(err, enrollment.ErrInvalidIdentity) {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, identity)
	}
}

func handleInventoryReport(authority *enrollment.Authority, service *inventory.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		agentID, err := authority.Authenticate(request.TLS.PeerCertificates[0])
		if err != nil {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		var facts inventory.Facts
		if err := decodeJSON(response, request, &facts); err != nil {
			return
		}
		if err := service.Report(request.Context(), agentID, facts); errors.Is(err, inventory.ErrInvalidFacts) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleListInstances(service *inventory.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		instances, err := service.List(request.Context())
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Instances []inventory.Host `json:"instances"`
		}{Instances: instances})
	}
}

func handleTelemetryReport(authority *enrollment.Authority, service *telemetry.Service, alerts *alerting.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		agentID, err := authority.Authenticate(request.TLS.PeerCertificates[0])
		if err != nil {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		var sample telemetry.Sample
		if err := decodeJSON(response, request, &sample); err != nil {
			return
		}
		sample.RecordedAt = sample.RecordedAt.UTC().Truncate(time.Microsecond)
		if err := service.Report(request.Context(), agentID, sample); errors.Is(err, telemetry.ErrInvalidSample) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if alerts != nil {
			upperBound := time.Now().UTC().Add(time.Second)
			if !sample.RecordedAt.Before(upperBound) {
				upperBound = sample.RecordedAt.Add(time.Second)
			}
			latest, err := service.History(request.Context(), agentID, sample.RecordedAt, upperBound, 1)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if len(latest) > 0 && latest[len(latest)-1].RecordedAt.Equal(sample.RecordedAt) {
				if err := alerts.EvaluateTelemetry(request.Context(), agentID, alerting.Telemetry{CPUPercent: sample.CPUPercent, MemoryPercent: sample.MemoryPercent, DiskPercent: sample.DiskPercent}); err != nil {
					http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
			}
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleTelemetryHistory(service *telemetry.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		to := time.Now().UTC()
		from := to.Add(-24 * time.Hour)
		limit := 288
		var err error
		if value := request.URL.Query().Get("from"); value != "" {
			from, err = time.Parse(time.RFC3339, value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}
		if value := request.URL.Query().Get("to"); value != "" {
			to, err = time.Parse(time.RFC3339, value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}
		if value := request.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}

		samples, err := service.History(request.Context(), request.PathValue("agentID"), from, to, limit)
		if errors.Is(err, telemetry.ErrInvalidSample) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		var latest *telemetry.Sample
		if len(samples) > 0 {
			latest = &samples[len(samples)-1]
		}
		writeJSON(response, http.StatusOK, struct {
			Latest  *telemetry.Sample  `json:"latest"`
			Samples []telemetry.Sample `json:"samples"`
		}{Latest: latest, Samples: samples})
	}
}

func handleServiceReport(authority *enrollment.Authority, service *serviceinventory.Manager) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		agentID, err := authority.Authenticate(request.TLS.PeerCertificates[0])
		if err != nil {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		var snapshot serviceinventory.Snapshot
		if err := decodeJSON(response, request, &snapshot); err != nil {
			return
		}
		if err := service.Report(request.Context(), agentID, snapshot); errors.Is(err, serviceinventory.ErrInvalidSnapshot) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleListServices(service *serviceinventory.Manager) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		filter := serviceinventory.Filter{
			State: serviceinventory.State(request.URL.Query().Get("state")),
			Query: request.URL.Query().Get("q"),
		}
		services, err := service.List(request.Context(), request.PathValue("agentID"), filter)
		if errors.Is(err, serviceinventory.ErrInvalidSnapshot) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Services []serviceinventory.Service `json:"services"`
		}{Services: services})
	}
}

func handleLogIngest(authority *enrollment.Authority, service *logstream.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		agentID, err := authority.Authenticate(request.TLS.PeerCertificates[0])
		if err != nil {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		var batch logstream.Batch
		if err := decodeJSON(response, request, &batch); err != nil {
			return
		}
		if err := service.Ingest(request.Context(), agentID, batch); errors.Is(err, logstream.ErrInvalidLogs) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleSearchLogs(service *logstream.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		to := time.Now().UTC()
		from := to.Add(-time.Hour)
		limit := 100
		var err error
		if value := request.URL.Query().Get("from"); value != "" {
			from, err = time.Parse(time.RFC3339, value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}
		if value := request.URL.Query().Get("to"); value != "" {
			to, err = time.Parse(time.RFC3339, value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}
		if value := request.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}
		entries, err := service.Search(request.Context(), logstream.Query{
			AgentID: request.PathValue("agentID"), From: from, To: to,
			Collector: logstream.Collector(request.URL.Query().Get("collector")),
			Severity:  logstream.Severity(request.URL.Query().Get("severity")),
			Source:    request.URL.Query().Get("source"), Text: request.URL.Query().Get("q"), Limit: limit,
		})
		if errors.Is(err, logstream.ErrInvalidLogs) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Entries []logstream.Entry `json:"entries"`
		}{Entries: entries})
	}
}

func handleStreamLogs(service *logstream.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		flusher, ok := response.(http.Flusher)
		if !ok {
			http.Error(response, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
			return
		}
		stream, err := service.Subscribe(request.Context(), request.PathValue("agentID"))
		if err != nil {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		response.Header().Set("Cache-Control", "no-cache")
		response.Header().Set("X-Accel-Buffering", "no")
		_, _ = fmt.Fprint(response, "event: ready\ndata: {}\n\n")
		flusher.Flush()
		for {
			select {
			case <-request.Context().Done():
				return
			case entry := <-stream:
				payload, err := json.Marshal(entry)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(response, "event: log\ndata: %s\n\n", payload); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

func handleCreateJob(service *jobs.Service, tokens accessTokens) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleOperator) {
			return
		}
		var createRequest jobs.CreateRequest
		if err := decodeJSON(response, request, &createRequest); err != nil {
			return
		}
		job, err := service.Create(request.Context(), request.PathValue("agentID"), createRequest)
		if errors.Is(err, jobs.ErrInvalidJob) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, job)
	}
}

func handleCreateAlertRule(service *alerting.Service, tokens accessTokens) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleAdmin) {
			return
		}
		var value alerting.RuleRequest
		if err := decodeJSON(response, request, &value); err != nil {
			return
		}
		rule, err := service.CreateRule(request.Context(), value)
		if errors.Is(err, alerting.ErrInvalidAlert) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, rule)
	}
}

func handleListAlertRules(service *alerting.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		values, err := service.ListRules(request.Context())
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Rules []alerting.Rule `json:"rules"`
		}{values})
	}
}

func handleCreateMaintenance(service *alerting.Service, tokens accessTokens) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleAdmin) {
			return
		}
		var value alerting.MaintenanceRequest
		if err := decodeJSON(response, request, &value); err != nil {
			return
		}
		window, err := service.CreateMaintenance(request.Context(), value)
		if errors.Is(err, alerting.ErrInvalidAlert) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, window)
	}
}

func handleListMaintenance(service *alerting.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		values, err := service.ListMaintenance(request.Context())
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Windows []alerting.MaintenanceWindow `json:"windows"`
		}{values})
	}
}

func handleListIncidents(service *alerting.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		limit := 100
		if value := request.URL.Query().Get("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			limit = parsed
		}
		values, err := service.ListIncidents(request.Context(), limit)
		if errors.Is(err, alerting.ErrInvalidAlert) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Incidents []alerting.Incident `json:"incidents"`
		}{values})
	}
}

func handleAlertEvents(service *alerting.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		values, err := service.ListEvents(request.Context(), request.PathValue("incidentID"))
		if errors.Is(err, alerting.ErrNotFound) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if errors.Is(err, alerting.ErrInvalidAlert) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Events []alerting.Event `json:"events"`
		}{values})
	}
}

func handleAcknowledgeIncident(service *alerting.Service, tokens accessTokens) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleOperator) {
			return
		}
		var body struct {
			Actor string `json:"actor"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		incident, err := service.Acknowledge(request.Context(), request.PathValue("incidentID"), body.Actor)
		if errors.Is(err, alerting.ErrInvalidAlert) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if errors.Is(err, alerting.ErrNotFound) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if errors.Is(err, alerting.ErrConflict) {
			http.Error(response, http.StatusText(http.StatusConflict), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, incident)
	}
}

func handleCreateCloudAccount(service *cloudinventory.Service, tokens accessTokens) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleAdmin) {
			return
		}
		var value cloudinventory.AccountRequest
		if err := decodeJSON(response, request, &value); err != nil {
			return
		}
		account, err := service.CreateAccount(request.Context(), value)
		if errors.Is(err, cloudinventory.ErrInvalidCloudInventory) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, account)
	}
}

func handleListCloudAccounts(service *cloudinventory.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		accounts, err := service.ListAccounts(request.Context())
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Accounts []cloudinventory.Account `json:"accounts"`
		}{Accounts: accounts})
	}
}

func handleReconcileCloudInstances(service *cloudinventory.Service, tokens accessTokens) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleAdmin) {
			return
		}
		var payload struct {
			Instances []cloudinventory.DiscoveredInstance `json:"instances"`
		}
		if err := decodeJSON(response, request, &payload); err != nil {
			return
		}
		instances, err := service.Reconcile(request.Context(), request.PathValue("accountID"), payload.Instances)
		if errors.Is(err, cloudinventory.ErrInvalidCloudInventory) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if errors.Is(err, cloudinventory.ErrCloudAccountNotFound) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Instances []cloudinventory.Instance `json:"instances"`
		}{Instances: instances})
	}
}

func handleListCloudInstances(service *cloudinventory.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		instances, err := service.ListInstances(request.Context())
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Instances []cloudinventory.Instance `json:"instances"`
		}{Instances: instances})
	}
}

func authorizeRole(response http.ResponseWriter, request *http.Request, tokens accessTokens, required accessRole) bool {
	adminToken := tokens.admin
	if adminToken == "" {
		adminToken = tokens.operator
	}
	if tokens.operator == "" && adminToken == "" {
		http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return false
	}
	authorization := request.Header.Get("Authorization")
	if validBearerToken(authorization, adminToken) {
		return true
	}
	if tokens.operator != "" && validBearerToken(authorization, tokens.operator) {
		if required == roleOperator {
			return true
		}
		http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return false
	}
	response.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	return false
}

func validBearerToken(authorization, expected string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return false
	}
	provided := strings.TrimPrefix(authorization, prefix)
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func handleListJobs(service *jobs.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		limit := 50
		var err error
		if value := request.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}
		values, err := service.List(request.Context(), request.PathValue("agentID"), limit)
		if errors.Is(err, jobs.ErrInvalidJob) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Jobs []jobs.Job `json:"jobs"`
		}{Jobs: values})
	}
}

func handleJobEvents(service *jobs.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		events, err := service.Events(request.Context(), request.PathValue("agentID"), request.PathValue("jobID"))
		if errors.Is(err, jobs.ErrInvalidJob) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if errors.Is(err, jobs.ErrJobNotFound) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Events []jobs.Event `json:"events"`
		}{Events: events})
	}
}

func handleClaimJob(authority *enrollment.Authority, service *jobs.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		agentID, ok := authenticateAgent(response, request, authority)
		if !ok {
			return
		}
		job, err := service.ClaimNext(request.Context(), agentID)
		if errors.Is(err, jobs.ErrInvalidJob) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		if job == nil {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(response, http.StatusOK, job)
	}
}

func handleReportJobEvent(authority *enrollment.Authority, service *jobs.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		agentID, ok := authenticateAgent(response, request, authority)
		if !ok {
			return
		}
		var eventRequest jobs.EventRequest
		if err := decodeJSON(response, request, &eventRequest); err != nil {
			return
		}
		job, err := service.Report(request.Context(), agentID, request.PathValue("jobID"), eventRequest)
		switch {
		case errors.Is(err, jobs.ErrInvalidJob):
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		case errors.Is(err, jobs.ErrJobNotFound):
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		case errors.Is(err, jobs.ErrJobConflict):
			http.Error(response, http.StatusText(http.StatusConflict), http.StatusConflict)
		case err != nil:
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		default:
			writeJSON(response, http.StatusOK, job)
		}
	}
}

func authenticateAgent(response http.ResponseWriter, request *http.Request, authority *enrollment.Authority) (string, bool) {
	if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
		http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return "", false
	}
	agentID, err := authority.Authenticate(request.TLS.PeerCertificates[0])
	if err != nil {
		http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return "", false
	}
	return agentID, true
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

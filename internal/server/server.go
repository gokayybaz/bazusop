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
	enrollmentAuthority *enrollment.Authority
	inventoryService    *inventory.Service
	telemetryService    *telemetry.Service
	serviceInventory    *serviceinventory.Manager
	logService          *logstream.Service
	jobService          *jobs.Service
	operatorToken       string
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

func NewHandler(options ...Option) http.Handler {
	configuration := handlerOptions{}
	for _, option := range options {
		option(&configuration)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", handleHealth)
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
			mux.HandleFunc("POST /api/v1/agents/telemetry", handleTelemetryReport(configuration.enrollmentAuthority, configuration.telemetryService))
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
		mux.HandleFunc("POST /api/v1/instances/{agentID}/jobs", handleCreateJob(configuration.jobService, configuration.operatorToken))
		mux.HandleFunc("GET /api/v1/instances/{agentID}/jobs", handleListJobs(configuration.jobService))
		mux.HandleFunc("GET /api/v1/instances/{agentID}/jobs/{jobID}/events", handleJobEvents(configuration.jobService))
		if configuration.enrollmentAuthority != nil {
			mux.HandleFunc("GET /api/v1/agents/jobs/next", handleClaimJob(configuration.enrollmentAuthority, configuration.jobService))
			mux.HandleFunc("POST /api/v1/agents/jobs/{jobID}/events", handleReportJobEvent(configuration.enrollmentAuthority, configuration.jobService))
		}
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

func handleTelemetryReport(authority *enrollment.Authority, service *telemetry.Service) http.HandlerFunc {
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
		if err := service.Report(request.Context(), agentID, sample); errors.Is(err, telemetry.ErrInvalidSample) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
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

func handleCreateJob(service *jobs.Service, operatorToken string) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if operatorToken == "" {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		if !validBearerToken(request.Header.Get("Authorization"), operatorToken) {
			response.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
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

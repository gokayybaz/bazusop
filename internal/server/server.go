package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/webui"
)

type Option func(*handlerOptions)

type handlerOptions struct {
	enrollmentAuthority *enrollment.Authority
	inventoryService    *inventory.Service
	telemetryService    *telemetry.Service
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

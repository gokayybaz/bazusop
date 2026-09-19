package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithTelemetry(service *telemetry.Service) Option {
	return func(options *handlerOptions) {
		options.telemetryService = service
	}
}

func handleTelemetryReport(authority *enrollment.Authority, service *telemetry.Service, alerts *alerting.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		agent, ok := authenticateAgent(response, request, authority)
		if !ok {
			return
		}
		var sample telemetry.Sample
		if err := decodeJSON(response, request, &sample); err != nil {
			return
		}
		sample.RecordedAt = sample.RecordedAt.UTC().Truncate(time.Microsecond)
		if err := service.Report(request.Context(), agent, sample); errors.Is(err, telemetry.ErrInvalidSample) {
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
			latest, err := service.History(request.Context(), agent.Scope(), agent.ID, sample.RecordedAt, upperBound, 1)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if len(latest) > 0 && latest[len(latest)-1].RecordedAt.Equal(sample.RecordedAt) {
				if err := alerts.EvaluateTelemetry(request.Context(), agent.Scope(), agent.ID, alerting.Telemetry{CPUPercent: sample.CPUPercent, MemoryPercent: sample.MemoryPercent, DiskPercent: sample.DiskPercent}); err != nil {
					http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
			}
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleTelemetryHistory(service *telemetry.Service, scope tenancy.Scope) http.HandlerFunc {
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

		samples, err := service.History(request.Context(), scope, request.PathValue("agentID"), from, to, limit)
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

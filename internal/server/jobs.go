package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithJobs(service *jobs.Service, operatorToken string) Option {
	return func(options *handlerOptions) {
		options.jobService = service
		options.operatorToken = operatorToken
	}
}

func handleCreateJob(service *jobs.Service, sessionService *sessions.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, authzService, authorization.PermissionCreateJobs, scope.SiteID, tokens, roleOperator); !ok {
			return
		}
		var createRequest jobs.CreateRequest
		if err := decodeJSON(response, request, &createRequest); err != nil {
			return
		}
		job, err := service.Create(request.Context(), scope, request.PathValue("agentID"), createRequest)
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

func handleListJobs(service *jobs.Service, scope tenancy.Scope) http.HandlerFunc {
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
		values, err := service.List(request.Context(), scope, request.PathValue("agentID"), limit)
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

func handleJobEvents(service *jobs.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		events, err := service.Events(request.Context(), scope, request.PathValue("agentID"), request.PathValue("jobID"))
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
		agent, ok := authenticateAgent(response, request, authority)
		if !ok {
			return
		}
		job, err := service.ClaimNext(request.Context(), agent)
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
		agent, ok := authenticateAgent(response, request, authority)
		if !ok {
			return
		}
		var eventRequest jobs.EventRequest
		if err := decodeJSON(response, request, &eventRequest); err != nil {
			return
		}
		job, err := service.Report(request.Context(), agent, request.PathValue("jobID"), eventRequest)
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

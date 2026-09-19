package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithAlerts(service *alerting.Service, operatorToken string) Option {
	return func(options *handlerOptions) {
		options.alertService = service
		options.operatorToken = operatorToken
	}
}

func handleCreateAlertRule(service *alerting.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleAdmin) {
			return
		}
		var value alerting.RuleRequest
		if err := decodeJSON(response, request, &value); err != nil {
			return
		}
		rule, err := service.CreateRule(request.Context(), scope, value)
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

func handleListAlertRules(service *alerting.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		values, err := service.ListRules(request.Context(), scope)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Rules []alerting.Rule `json:"rules"`
		}{values})
	}
}

func handleCreateMaintenance(service *alerting.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleAdmin) {
			return
		}
		var value alerting.MaintenanceRequest
		if err := decodeJSON(response, request, &value); err != nil {
			return
		}
		window, err := service.CreateMaintenance(request.Context(), scope, value)
		if errors.Is(err, alerting.ErrInvalidAlert) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if errors.Is(err, alerting.ErrNotFound) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, window)
	}
}

func handleListMaintenance(service *alerting.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		values, err := service.ListMaintenance(request.Context(), scope)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Windows []alerting.MaintenanceWindow `json:"windows"`
		}{values})
	}
}

func handleListIncidents(service *alerting.Service, scope tenancy.Scope) http.HandlerFunc {
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
		values, err := service.ListIncidents(request.Context(), scope, limit)
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

func handleAlertEvents(service *alerting.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		values, err := service.ListEvents(request.Context(), scope, request.PathValue("incidentID"))
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

func handleAcknowledgeIncident(service *alerting.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
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
		incident, err := service.Acknowledge(request.Context(), scope, request.PathValue("incidentID"), body.Actor)
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

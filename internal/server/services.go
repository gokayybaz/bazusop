package server

import (
	"errors"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithServiceInventory(service *serviceinventory.Manager) Option {
	return func(options *handlerOptions) {
		options.serviceInventory = service
	}
}

func handleServiceReport(authority *enrollment.Authority, service *serviceinventory.Manager) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		agent, ok := authenticateAgent(response, request, authority)
		if !ok {
			return
		}
		var snapshot serviceinventory.Snapshot
		if err := decodeJSON(response, request, &snapshot); err != nil {
			return
		}
		if err := service.Report(request.Context(), agent, snapshot); errors.Is(err, serviceinventory.ErrInvalidSnapshot) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleListServices(service *serviceinventory.Manager, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		filter := serviceinventory.Filter{
			State: serviceinventory.State(request.URL.Query().Get("state")),
			Query: request.URL.Query().Get("q"),
		}
		services, err := service.List(request.Context(), scope, request.PathValue("agentID"), filter)
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

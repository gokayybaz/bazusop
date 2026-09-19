package server

import (
	"errors"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithInventory(service *inventory.Service) Option {
	return func(options *handlerOptions) {
		options.inventoryService = service
	}
}

func handleInventoryReport(authority *enrollment.Authority, service *inventory.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		agent, ok := authenticateAgent(response, request, authority)
		if !ok {
			return
		}
		var facts inventory.Facts
		if err := decodeJSON(response, request, &facts); err != nil {
			return
		}
		if err := service.Report(request.Context(), agent, facts); errors.Is(err, inventory.ErrInvalidFacts) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleListInstances(service *inventory.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		instances, err := service.List(request.Context(), scope)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Instances []inventory.Host `json:"instances"`
		}{Instances: instances})
	}
}

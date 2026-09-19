package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gokayybaz/bazusop/internal/audit"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithAudit(service *audit.Service) Option {
	return func(options *handlerOptions) {
		options.auditService = service
	}
}

func handleListAuditEvents(service *audit.Service, scope tenancy.Scope) http.HandlerFunc {
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
		values, err := service.List(request.Context(), scope, limit)
		if errors.Is(err, audit.ErrInvalidAudit) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Events []audit.Event `json:"events"`
		}{values})
	}
}

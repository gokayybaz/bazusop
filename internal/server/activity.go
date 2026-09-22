package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithActivity(service *activity.Service) Option {
	return func(options *handlerOptions) {
		options.activityService = service
	}
}

func handleListActivityEvents(service *activity.Service, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, serviceAccountService, authzService, authorization.PermissionViewActivity, scope.SiteID); !ok {
			return
		}
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
		if errors.Is(err, activity.ErrInvalidActivity) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Events []activity.Event `json:"events"`
		}{values})
	}
}

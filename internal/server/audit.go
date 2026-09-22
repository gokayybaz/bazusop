package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func handleListAuditTrail(service *audittrail.Service, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, serviceAccountService, authzService, authorization.PermissionViewAuditEvents, scope.SiteID); !ok {
			return
		}
		query := request.URL.Query()
		limit := 100
		if value := query.Get("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			limit = parsed
		}
		filter := audittrail.Filter{
			ActorType:    audittrail.ActorType(query.Get("actor_type")),
			ResourceType: query.Get("resource_type"),
			Outcome:      audittrail.Outcome(query.Get("outcome")),
		}
		if raw := query.Get("since"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			filter.Since = parsed
		}
		if raw := query.Get("until"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			filter.Until = parsed
		}
		values, err := service.List(request.Context(), scope, filter, limit)
		if errors.Is(err, audittrail.ErrInvalidQuery) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Events []audittrail.Event `json:"events"`
		}{values})
	}
}

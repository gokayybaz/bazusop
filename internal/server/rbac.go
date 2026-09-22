package server

import (
	"net/http"
	"strings"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithAuthorization(service *authorization.Service) Option {
	return func(options *handlerOptions) { options.authorizationService = service }
}

// requirePermission authenticates the caller as either a human session or a
// service account and checks the real permission matrix — the only two
// ways to authenticate a mutation as of spike 11.7, which removed the
// legacy operator/admin bearer-token bridge entirely (see the design
// spec's "Servis hesapları" section). Neither present, or either present
// but invalid or lacking the permission, is rejected.
func requirePermission(response http.ResponseWriter, request *http.Request, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, permission authorization.Permission, siteID string) (string, bool) {
	if sessionService != nil {
		if cookie, err := request.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			session, err := sessionService.Validate(request.Context(), cookie.Value)
			if err != nil {
				clearSessionCookies(response, request)
				http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return "", false
			}
			if request.Method != http.MethodGet && request.Method != http.MethodHead && !requireMatchingCSRFToken(response, request, session) {
				return "", false
			}
			allowed, err := authzService.Can(request.Context(), session.UserID, permission, siteID)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return "", false
			}
			if !allowed {
				http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return "", false
			}
			return session.UserID, true
		}
	}
	if serviceAccountService != nil {
		if bearer, ok := bearerToken(request); ok && strings.HasPrefix(bearer, serviceaccounts.TokenPrefix) {
			account, err := serviceAccountService.Validate(request.Context(), bearer, sourceIP(request))
			if err != nil {
				http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return "", false
			}
			if account.OrganizationID != tenancy.DefaultOrganizationID || account.SiteID != siteID || !authorization.PermissionAllowsRole(permission, account.Role) {
				http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return "", false
			}
			return account.ID, true
		}
	}
	http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	return "", false
}

func bearerToken(request *http.Request) (string, bool) {
	const prefix = "Bearer "
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	return strings.TrimPrefix(header, prefix), true
}

func handleAssignSiteRole(sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID)
		if !ok {
			return
		}
		var body struct {
			UserID string `json:"user_id"`
			Role   string `json:"role"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		if err := authzService.AssignRole(request.Context(), body.UserID, scope.OrganizationID, request.PathValue("siteID"), authorization.SiteRole(body.Role)); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceSiteRole, ReferenceID: body.UserID, Type: "assigned",
				Actor: actorID, Message: body.Role + " rolü atandı",
			})
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeSiteRole(sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID)
		if !ok {
			return
		}
		userID := request.PathValue("userID")
		if err := authzService.RevokeRole(request.Context(), userID, request.PathValue("siteID")); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceSiteRole, ReferenceID: userID, Type: "revoked",
				Actor: actorID, Message: "site rolü kaldırıldı",
			})
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

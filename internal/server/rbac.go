package server

import (
	"net/http"
	"strings"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithAuthorization(service *authorization.Service) Option {
	return func(options *handlerOptions) { options.authorizationService = service }
}

// requirePermission tries the session-cookie path first: a valid session
// must also satisfy the real permission matrix, and a session that fails
// this check gets a clean 403 — it never falls through to the legacy
// bearer path (an authenticated-but-unauthorized human should not be
// silently let in via a stray Authorization header). Only when no session
// cookie is present at all does this fall back to the pre-existing
// operator/admin bearer token bridge, preserving every current bearer-token
// integration unchanged until spike 11.7 removes it.
func requirePermission(response http.ResponseWriter, request *http.Request, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, permission authorization.Permission, siteID string, tokens accessTokens, legacyRole accessRole) (string, bool) {
	if sessionService != nil {
		if cookie, err := request.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			session, err := sessionService.Validate(request.Context(), cookie.Value)
			if err != nil {
				clearSessionCookies(response, request)
				http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
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
	if !authorizeRole(response, request, tokens, legacyRole) {
		return "", false
	}
	return "", true
}

func bearerToken(request *http.Request) (string, bool) {
	const prefix = "Bearer "
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	return strings.TrimPrefix(header, prefix), true
}

func handleAssignSiteRole(sessionService *sessions.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID, tokens, roleAdmin); !ok {
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
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeSiteRole(sessionService *sessions.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID, tokens, roleAdmin); !ok {
			return
		}
		if err := authzService.RevokeRole(request.Context(), request.PathValue("userID"), request.PathValue("siteID")); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

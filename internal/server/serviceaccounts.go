package server

import (
	"errors"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithServiceAccounts(service *serviceaccounts.Service) Option {
	return func(options *handlerOptions) { options.serviceAccountService = service }
}

func handleCreateServiceAccount(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID); !ok {
			return
		}
		var body struct {
			Name       string `json:"name"`
			Role       string `json:"role"`
			ExpiryDays int    `json:"expiry_days"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		account, token, err := service.CreateAccount(request.Context(), scope.OrganizationID, request.PathValue("siteID"), body.Name, authorization.SiteRole(body.Role), body.ExpiryDays)
		if errors.Is(err, serviceaccounts.ErrInvalidRole) || errors.Is(err, serviceaccounts.ErrInvalidExpiry) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Role  string `json:"role"`
			Token string `json:"token"`
		}{account.ID, account.Name, string(account.Role), token})
	}
}

func handleListServiceAccounts(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID); !ok {
			return
		}
		accounts, err := service.ListForSite(request.Context(), request.PathValue("siteID"))
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			ServiceAccounts []serviceaccounts.ServiceAccount `json:"service_accounts"`
		}{accounts})
	}
}

func handleRotateServiceAccountToken(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID); !ok {
			return
		}
		var body struct {
			ExpiryDays int `json:"expiry_days"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		token, err := service.RotateToken(request.Context(), request.PathValue("accountID"), body.ExpiryDays)
		if errors.Is(err, serviceaccounts.ErrInvalidExpiry) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, struct {
			Token string `json:"token"`
		}{token})
	}
}

func handleRevokeServiceAccountToken(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID); !ok {
			return
		}
		if err := service.RevokeToken(request.Context(), request.PathValue("tokenID")); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleDisableServiceAccount(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID); !ok {
			return
		}
		if err := service.DisableAccount(request.Context(), request.PathValue("accountID")); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

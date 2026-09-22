package server

import (
	"errors"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithCloudInventory(service *cloudinventory.Service) Option {
	return func(options *handlerOptions) {
		options.cloudInventory = service
	}
}

func handleCreateCloudAccount(service *cloudinventory.Service, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, serviceAccountService, authzService, authorization.PermissionManageCloudAccounts, scope.SiteID); !ok {
			return
		}
		var value cloudinventory.AccountRequest
		if err := decodeJSON(response, request, &value); err != nil {
			return
		}
		account, err := service.CreateAccount(request.Context(), scope, value)
		if errors.Is(err, cloudinventory.ErrInvalidCloudInventory) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, account)
	}
}

func handleListCloudAccounts(service *cloudinventory.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		accounts, err := service.ListAccounts(request.Context(), scope)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Accounts []cloudinventory.Account `json:"accounts"`
		}{Accounts: accounts})
	}
}

func handleReconcileCloudInstances(service *cloudinventory.Service, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, serviceAccountService, authzService, authorization.PermissionManageCloudAccounts, scope.SiteID); !ok {
			return
		}
		var payload struct {
			Instances []cloudinventory.DiscoveredInstance `json:"instances"`
		}
		if err := decodeJSON(response, request, &payload); err != nil {
			return
		}
		instances, err := service.Reconcile(request.Context(), scope, request.PathValue("accountID"), payload.Instances)
		if errors.Is(err, cloudinventory.ErrInvalidCloudInventory) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if errors.Is(err, cloudinventory.ErrCloudAccountNotFound) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Instances []cloudinventory.Instance `json:"instances"`
		}{Instances: instances})
	}
}

func handleListCloudInstances(service *cloudinventory.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		instances, err := service.ListInstances(request.Context(), scope)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Instances []cloudinventory.Instance `json:"instances"`
		}{Instances: instances})
	}
}

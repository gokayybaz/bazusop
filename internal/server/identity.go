package server

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithIdentity(service *identity.Service, bootstrapSecret string) Option {
	return func(options *handlerOptions) {
		options.identityService = service
		options.bootstrapSecret = bootstrapSecret
	}
}

func handleBootstrap(service *identity.Service, bootstrapSecret string) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			Secret   string `json:"secret"`
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		if subtle.ConstantTimeCompare([]byte(body.Secret), []byte(bootstrapSecret)) != 1 {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		_, enrollment, err := service.Bootstrap(request.Context(), tenancy.DefaultOrganizationID, body.Email, body.Password)
		if errors.Is(err, identity.ErrAlreadyBootstrapped) {
			http.Error(response, http.StatusText(http.StatusConflict), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, struct {
			ProvisioningURI string   `json:"provisioning_uri"`
			RecoveryCodes   []string `json:"recovery_codes"`
		}{enrollment.ProvisioningURI, enrollment.RecoveryCodes})
	}
}

func handleCreateInvite(service *identity.Service, sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorUserID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID)
		if !ok {
			return
		}
		var body struct {
			Email   string   `json:"email"`
			Role    string   `json:"role"`
			SiteIDs []string `json:"site_ids"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		createdBy := "admin"
		if actorUserID != "" {
			createdBy = actorUserID
		}
		role := identity.RolePlatformAdmin
		var grants []identity.SiteRoleGrant
		if body.Role != "" {
			role = ""
			for _, siteID := range body.SiteIDs {
				grants = append(grants, identity.SiteRoleGrant{SiteID: siteID, Role: body.Role})
			}
		}
		invite, token, err := service.CreateInvite(request.Context(), createdBy, tenancy.DefaultOrganizationID, body.Email, role, grants)
		if errors.Is(err, identity.ErrInvalidInviteRole) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceIdentity, ReferenceID: invite.ID, Type: "invite_created",
				Actor: createdBy, Message: invite.Email + " davet edildi",
			})
		}
		writeJSON(response, http.StatusCreated, struct {
			Email string `json:"email"`
			Token string `json:"token"`
		}{invite.Email, token})
	}
}

func handleConsumeInvite(service *identity.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		user, err := service.ConsumeInvite(request.Context(), request.PathValue("token"), body.Password)
		if errors.Is(err, identity.ErrInviteNotFound) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if errors.Is(err, identity.ErrInviteExpired) {
			http.Error(response, http.StatusText(http.StatusConflict), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, struct {
			ID string `json:"id"`
		}{user.ID})
	}
}

func handleConfirmTOTP(service *identity.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			Code string `json:"code"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		enrollment, err := service.ConfirmTOTP(request.Context(), request.PathValue("userID"), func([]byte) string { return body.Code }, time.Now().UTC())
		if errors.Is(err, identity.ErrInvalidTOTPCode) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if errors.Is(err, identity.ErrTOTPAlreadyConfirmed) {
			http.Error(response, http.StatusText(http.StatusConflict), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			RecoveryCodes []string `json:"recovery_codes"`
		}{enrollment.RecoveryCodes})
	}
}

package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/oidc"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

const oidcFlowCookieName = "bazusop_oidc_flow"

type oidcFlowCookieValue struct {
	State        string `json:"state"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
}

// setOIDCFlowCookie carries state/nonce/PKCE verifier through the
// browser's round trip to the IdP and back. It is HttpOnly and scoped to
// the OIDC path only; a 10-minute Max-Age comfortably covers a human
// completing an IdP login screen while keeping a stale, unused flow from
// lingering indefinitely.
func setOIDCFlowCookie(response http.ResponseWriter, request *http.Request, flow oidc.Flow) error {
	payload, err := json.Marshal(oidcFlowCookieValue{State: flow.State, Nonce: flow.Nonce, CodeVerifier: flow.CodeVerifier})
	if err != nil {
		return err
	}
	http.SetCookie(response, &http.Cookie{
		Name: oidcFlowCookieName, Value: base64.RawURLEncoding.EncodeToString(payload),
		Path: "/api/v1/oidc", HttpOnly: true, Secure: request.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	return nil
}

func readAndClearOIDCFlowCookie(response http.ResponseWriter, request *http.Request) (oidc.Flow, bool) {
	cookie, err := request.Cookie(oidcFlowCookieName)
	if err != nil || cookie.Value == "" {
		return oidc.Flow{}, false
	}
	http.SetCookie(response, &http.Cookie{
		Name: oidcFlowCookieName, Value: "", Path: "/api/v1/oidc", HttpOnly: true,
		Secure: request.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	decoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return oidc.Flow{}, false
	}
	var value oidcFlowCookieValue
	if err := json.Unmarshal(decoded, &value); err != nil {
		return oidc.Flow{}, false
	}
	return oidc.Flow{State: value.State, Nonce: value.Nonce, CodeVerifier: value.CodeVerifier}, true
}

func handleSetOIDCConfiguration(service *identity.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageOrgSecurity, scope.SiteID)
		if !ok {
			return
		}
		var body struct {
			DiscoveryURL string `json:"discovery_url"`
			Issuer       string `json:"issuer"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			RedirectURL  string `json:"redirect_url"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		config, err := service.SetOIDCConfiguration(request.Context(), scope.OrganizationID, body.DiscoveryURL, body.Issuer, body.ClientID, body.ClientSecret, body.RedirectURL, actorID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, oidcConfigurationResponse(config))
	}
}

func handleGetOIDCConfiguration(service *identity.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageOrgSecurity, scope.SiteID); !ok {
			return
		}
		config, err := service.OIDCConfiguration(request.Context(), scope.OrganizationID)
		if errors.Is(err, identity.ErrOIDCNotConfigured) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, oidcConfigurationResponse(config))
	}
}

func oidcConfigurationResponse(config identity.OIDCConfiguration) any {
	return struct {
		DiscoveryURL string `json:"discovery_url"`
		Issuer       string `json:"issuer"`
		ClientID     string `json:"client_id"`
		RedirectURL  string `json:"redirect_url"`
		UpdatedAt    string `json:"updated_at"`
		UpdatedBy    string `json:"updated_by"`
	}{config.DiscoveryURL, config.Issuer, config.ClientID, config.RedirectURL, config.UpdatedAt.Format(timeLayout), config.UpdatedBy}
}

func handleOIDCLogin(service *identity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		config, secret, err := service.OIDCConfigurationWithSecret(request.Context(), scope.OrganizationID)
		if errors.Is(err, identity.ErrOIDCNotConfigured) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		provider, err := oidc.NewProvider(request.Context(), oidc.Configuration{
			DiscoveryURL: config.DiscoveryURL, Issuer: config.Issuer, ClientID: config.ClientID,
			ClientSecret: secret, RedirectURL: config.RedirectURL,
		})
		if err != nil {
			http.Error(response, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
			return
		}
		flow, err := oidc.NewFlow()
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if err := setOIDCFlowCookie(response, request, flow); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		http.Redirect(response, request, provider.AuthCodeURL(flow), http.StatusFound)
	}
}

func handleOIDCCallback(service *identity.Service, sessionService *sessions.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		flow, ok := readAndClearOIDCFlowCookie(response, request)
		if !ok {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		query := request.URL.Query()
		if query.Get("state") != flow.State {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		code := query.Get("code")
		if code == "" {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		config, secret, err := service.OIDCConfigurationWithSecret(request.Context(), scope.OrganizationID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		provider, err := oidc.NewProvider(request.Context(), oidc.Configuration{
			DiscoveryURL: config.DiscoveryURL, Issuer: config.Issuer, ClientID: config.ClientID,
			ClientSecret: secret, RedirectURL: config.RedirectURL,
		})
		if err != nil {
			http.Error(response, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
			return
		}
		claims, err := provider.Exchange(request.Context(), code, flow)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		user, err := service.LoginOrLinkOIDCUser(request.Context(), scope.OrganizationID, config.Issuer, claims.Subject, claims.Email)
		if errors.Is(err, identity.ErrInviteNotFound) || errors.Is(err, identity.ErrInviteExpired) || errors.Is(err, identity.ErrInviteWrongIdentityType) {
			http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		_, token, csrfToken, err := sessionService.Create(request.Context(), user.ID, user.OrganizationID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		setSessionCookies(response, request, token, csrfToken)
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceIdentity, ReferenceID: user.ID, Type: "oidc_login",
				Actor: user.ID, Message: user.Email + " OIDC ile giriş yaptı",
			})
		}
		http.Redirect(response, request, "/", http.StatusFound)
	}
}

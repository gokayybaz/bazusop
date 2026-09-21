package server

import (
	"crypto/subtle"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

const (
	sessionCookieName = "bazusop_session"
	csrfCookieName    = "bazusop_csrf"
	timeLayout        = "2006-01-02T15:04:05Z07:00"
)

func WithSessions(sessionService *sessions.Service, identityService *identity.Service) Option {
	return func(options *handlerOptions) {
		options.sessionService = sessionService
		options.sessionIdentityService = identityService
	}
}

func setSessionCookies(response http.ResponseWriter, request *http.Request, token, csrfToken string) {
	secure := request.TLS != nil
	http.SetCookie(response, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/api", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(response, &http.Cookie{Name: csrfCookieName, Value: csrfToken, Path: "/api", HttpOnly: false, Secure: secure, SameSite: http.SameSiteLaxMode})
}

func clearSessionCookies(response http.ResponseWriter, request *http.Request) {
	secure := request.TLS != nil
	http.SetCookie(response, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/api", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.SetCookie(response, &http.Cookie{Name: csrfCookieName, Value: "", Path: "/api", HttpOnly: false, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// authenticateSession resolves and validates the caller's session cookie.
// On any failure it writes a 401 itself and returns ok=false — callers just
// check ok and return.
func authenticateSession(response http.ResponseWriter, request *http.Request, sessionService *sessions.Service) (sessions.Session, string, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return sessions.Session{}, "", false
	}
	session, err := sessionService.Validate(request.Context(), cookie.Value)
	if err != nil {
		clearSessionCookies(response, request)
		http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return sessions.Session{}, "", false
	}
	return session, cookie.Value, true
}

func requireMatchingCSRFToken(response http.ResponseWriter, request *http.Request, session sessions.Session) bool {
	header := request.Header.Get("X-CSRF-Token")
	if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(session.CSRFToken)) != 1 {
		http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return false
	}
	return true
}

func handleLogin(identityService *identity.Service, sessionService *sessions.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			Email        string `json:"email"`
			Password     string `json:"password"`
			TOTPCode     string `json:"totp_code"`
			RecoveryCode string `json:"recovery_code"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		var user identity.User
		var err error
		if body.RecoveryCode != "" {
			user, err = identityService.VerifyCredentialsWithRecoveryCode(request.Context(), tenancy.DefaultOrganizationID, body.Email, body.Password, body.RecoveryCode)
		} else {
			user, err = identityService.VerifyCredentials(request.Context(), tenancy.DefaultOrganizationID, body.Email, body.Password, body.TOTPCode)
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		session, token, csrfToken, err := sessionService.Create(request.Context(), user.ID, user.OrganizationID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		setSessionCookies(response, request, token, csrfToken)
		writeJSON(response, http.StatusCreated, struct {
			UserID    string `json:"user_id"`
			Email     string `json:"email"`
			Role      string `json:"role"`
			CSRFToken string `json:"csrf_token"`
			ExpiresAt string `json:"expires_at"`
		}{user.ID, user.Email, string(user.Role), csrfToken, session.AbsoluteExpiresAt.Format(timeLayout)})
	}
}

func handleWhoAmI(sessionService *sessions.Service, identityService *identity.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		session, _, ok := authenticateSession(response, request, sessionService)
		if !ok {
			return
		}
		user, err := identityService.UserByID(request.Context(), session.UserID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			UserID    string `json:"user_id"`
			Email     string `json:"email"`
			Role      string `json:"role"`
			ExpiresAt string `json:"expires_at"`
		}{user.ID, user.Email, string(user.Role), session.AbsoluteExpiresAt.Format(timeLayout)})
	}
}

func handleLogout(sessionService *sessions.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		session, token, ok := authenticateSession(response, request, sessionService)
		if !ok {
			return
		}
		if !requireMatchingCSRFToken(response, request, session) {
			return
		}
		if err := sessionService.Revoke(request.Context(), token); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		clearSessionCookies(response, request)
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeUserSessions(sessionService *sessions.Service, tokens accessTokens) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleAdmin) {
			return
		}
		if err := sessionService.RevokeAllForUser(request.Context(), request.PathValue("userID")); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeAllSessions(sessionService *sessions.Service, tokens accessTokens) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleAdmin) {
			return
		}
		if err := sessionService.RevokeAllForOrganization(request.Context(), tenancy.DefaultOrganizationID); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

package server

import (
	"errors"
	"net"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithEnrollment(authority *enrollment.Authority) Option {
	return func(options *handlerOptions) {
		options.enrollmentAuthority = authority
	}
}

func handleEnroll(authority *enrollment.Authority) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !secureEnrollmentTransport(request) {
			http.Error(response, http.StatusText(http.StatusUpgradeRequired), http.StatusUpgradeRequired)
			return
		}
		var enrollmentRequest enrollment.Request
		if err := decodeJSON(response, request, &enrollmentRequest); err != nil {
			return
		}
		identity, err := authority.EnrollContext(request.Context(), enrollmentRequest)
		switch {
		case errors.Is(err, enrollment.ErrInvalidToken):
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		case errors.Is(err, enrollment.ErrTokenConsumed):
			http.Error(response, http.StatusText(http.StatusConflict), http.StatusConflict)
			return
		case errors.Is(err, enrollment.ErrInvalidRequest):
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		case err != nil:
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, identity)
	}
}

func secureEnrollmentTransport(request *http.Request) bool {
	if request.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return false
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func handleRenew(authority *enrollment.Authority) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := authenticateAgent(response, request, authority); !ok {
			return
		}
		var renewalRequest struct {
			CSRPEM string `json:"csr"`
		}
		if err := decodeJSON(response, request, &renewalRequest); err != nil {
			return
		}
		identity, err := authority.RenewContext(request.Context(), request.TLS.PeerCertificates[0], renewalRequest.CSRPEM)
		if errors.Is(err, enrollment.ErrInvalidIdentity) {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, identity)
	}
}

func authenticateAgent(response http.ResponseWriter, request *http.Request, authority *enrollment.Authority) (tenancy.Agent, bool) {
	if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
		http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return tenancy.Agent{}, false
	}
	agent, err := authority.AuthenticateContext(request.Context(), request.TLS.PeerCertificates[0])
	if err != nil {
		http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return tenancy.Agent{}, false
	}
	return agent, true
}

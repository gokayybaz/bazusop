package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/webui"
)

type Option func(*handlerOptions)

type handlerOptions struct {
	enrollmentAuthority *enrollment.Authority
}

func WithEnrollment(authority *enrollment.Authority) Option {
	return func(options *handlerOptions) {
		options.enrollmentAuthority = authority
	}
}

func NewHandler(options ...Option) http.Handler {
	configuration := handlerOptions{}
	for _, option := range options {
		option(&configuration)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", handleHealth)
	if configuration.enrollmentAuthority != nil {
		mux.HandleFunc("POST /api/v1/agents/enroll", handleEnroll(configuration.enrollmentAuthority))
		mux.HandleFunc("POST /api/v1/agents/renew", handleRenew(configuration.enrollmentAuthority))
	}
	mux.HandleFunc("/api/", func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	})
	mux.Handle("/", webui.Handler())
	return mux
}

func handleHealth(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(map[string]string{
		"service": "bazusop-hub",
		"status":  "ok",
	})
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
		identity, err := authority.Enroll(enrollmentRequest)
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
		if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		var renewalRequest struct {
			CSRPEM string `json:"csr"`
		}
		if err := decodeJSON(response, request, &renewalRequest); err != nil {
			return
		}
		identity, err := authority.Renew(request.TLS.PeerCertificates[0], renewalRequest.CSRPEM)
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

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return err
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

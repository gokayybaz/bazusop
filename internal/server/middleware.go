package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func newCorrelationID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// deriveActor makes a best-effort identification of who is calling, purely
// for audit labeling. It does not authenticate anything — a route's own
// handler (authenticateAgent, authorizeRole) independently verifies the
// client certificate or bearer token and is solely responsible for
// authorization decisions. An unverified or forged cert/header still gets a
// label here; the audit outcome/error_code on the same event reflects
// whether the handler actually accepted it.
func deriveActor(request *http.Request) (audittrail.ActorType, string) {
	if request.TLS != nil && len(request.TLS.PeerCertificates) > 0 {
		cert := request.TLS.PeerCertificates[0]
		if len(cert.URIs) > 0 {
			return audittrail.ActorAgent, cert.URIs[0].String()
		}
		return audittrail.ActorAgent, cert.Subject.CommonName
	}
	if cookie, err := request.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		sum := sha256.Sum256([]byte(cookie.Value))
		return audittrail.ActorHuman, hex.EncodeToString(sum[:])
	}
	if request.Header.Get("Authorization") != "" {
		return audittrail.ActorLegacyToken, "bearer"
	}
	return audittrail.ActorAnonymous, ""
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func registerAudited(mux *http.ServeMux, pattern, method, resourceType string, pathParams []string, service *audittrail.Service, scope tenancy.Scope, handler http.HandlerFunc) {
	mux.HandleFunc(method+" "+pattern, func(response http.ResponseWriter, request *http.Request) {
		if service == nil {
			handler(response, request)
			return
		}
		correlationID, err := newCorrelationID()
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}
		actorType, actorID := deriveActor(request)

		handler(recorder, request)

		resourceID := ""
		for index, name := range pathParams {
			if index > 0 {
				resourceID += "/"
			}
			resourceID += request.PathValue(name)
		}
		outcome, errorCode := audittrail.OutcomeSuccess, ""
		if recorder.status >= 400 {
			outcome, errorCode = audittrail.OutcomeFailure, strconv.Itoa(recorder.status)
		}
		event := audittrail.Event{
			CorrelationID: correlationID, ActorType: actorType, ActorID: actorID,
			OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
			Action: method, ResourceType: resourceType, ResourceID: resourceID,
			Outcome: outcome, ErrorCode: errorCode,
			SourceIP: sourceIP(request), UserAgent: request.UserAgent(),
		}
		if err := service.Record(request.Context(), event); err != nil {
			// Audit is best-effort at this layer (see the package doc comment
			// in internal/audittrail on the same-transaction boundary); a
			// write failure here must never take the API down.
			_ = err
		}
	})
}

func sourceIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

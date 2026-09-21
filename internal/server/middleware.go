package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/audittrail"
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

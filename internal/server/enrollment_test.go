package server_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/server"
)

func TestAgentEnrollmentAndRenewalAPI(t *testing.T) {
	t.Parallel()

	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	handler := server.NewHandler(server.WithEnrollment(authority))

	enrollmentBody := encodeJSON(t, enrollment.Request{
		BootstrapToken:  "bootstrap-secret",
		Name:            "edge-01",
		OperatingSystem: "linux",
		CSRPEM:          serverCSR(t, "edge-01"),
	})
	enrollRequest := httptest.NewRequest(http.MethodPost, "/api/v1/agents/enroll", enrollmentBody)
	enrollRequest.Header.Set("Content-Type", "application/json")
	enrollRequest.TLS = &tls.ConnectionState{}
	enrollResponse := httptest.NewRecorder()
	handler.ServeHTTP(enrollResponse, enrollRequest)

	if enrollResponse.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, enrollResponse.Code, enrollResponse.Body.String())
	}
	var identity enrollment.Identity
	if err := json.NewDecoder(enrollResponse.Body).Decode(&identity); err != nil {
		t.Fatalf("decode enrollment response: %v", err)
	}
	peer := serverCertificate(t, identity.CertificatePEM)

	renewBody := encodeJSON(t, map[string]string{"csr": serverCSR(t, "edge-01")})
	renewRequest := httptest.NewRequest(http.MethodPost, "/api/v1/agents/renew", renewBody)
	renewRequest.Header.Set("Content-Type", "application/json")
	renewRequest.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{peer}}
	renewResponse := httptest.NewRecorder()
	handler.ServeHTTP(renewResponse, renewRequest)

	if renewResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, renewResponse.Code, renewResponse.Body.String())
	}
	var renewed enrollment.Identity
	if err := json.NewDecoder(renewResponse.Body).Decode(&renewed); err != nil {
		t.Fatalf("decode renewal response: %v", err)
	}
	if renewed.AgentID != identity.AgentID {
		t.Fatalf("expected renewed agent ID %q, got %q", identity.AgentID, renewed.AgentID)
	}
}

func TestAgentEnrollmentRequiresSecureTransport(t *testing.T) {
	t.Parallel()

	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/enroll", encodeJSON(t, enrollment.Request{}))
	request.RemoteAddr = "203.0.113.10:51000"
	response := httptest.NewRecorder()

	server.NewHandler(server.WithEnrollment(authority)).ServeHTTP(response, request)

	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("expected status %d, got %d", http.StatusUpgradeRequired, response.Code)
	}
}

func TestAgentRenewalRequiresMTLSIdentity(t *testing.T) {
	t.Parallel()

	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	handler := server.NewHandler(server.WithEnrollment(authority))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/renew", encodeJSON(t, map[string]string{
		"csr": serverCSR(t, "edge-01"),
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func encodeJSON(t *testing.T, value any) *bytes.Reader {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode JSON: %v", err)
	}
	return bytes.NewReader(payload)
}

func serverCSR(t *testing.T, commonName string) string {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: commonName},
	}, privateKey)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

func serverCertificate(t *testing.T, certificatePEM string) *x509.Certificate {
	t.Helper()

	block, _ := pem.Decode([]byte(certificatePEM))
	if block == nil {
		t.Fatal("decode certificate PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return certificate
}

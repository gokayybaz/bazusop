package enrollment_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"testing"

	"github.com/gokayybaz/bazusop/internal/enrollment"
)

func TestBootstrapTokenBecomesRenewableAgentIdentity(t *testing.T) {
	t.Parallel()

	authority, err := enrollment.NewAuthority("enroll-once")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}

	firstCSR := newCSR(t, "edge-01")
	identity, err := authority.Enroll(enrollment.Request{
		BootstrapToken:  "enroll-once",
		Name:            "edge-01",
		OperatingSystem: "linux",
		CSRPEM:          firstCSR,
	})
	if err != nil {
		t.Fatalf("enroll agent: %v", err)
	}
	if identity.AgentID == "" || identity.CertificatePEM == "" || identity.CACertificatePEM == "" {
		t.Fatalf("expected complete agent identity, got %#v", identity)
	}

	_, err = authority.Enroll(enrollment.Request{
		BootstrapToken:  "enroll-once",
		Name:            "edge-02",
		OperatingSystem: "windows",
		CSRPEM:          newCSR(t, "edge-02"),
	})
	if !errors.Is(err, enrollment.ErrTokenConsumed) {
		t.Fatalf("expected consumed token error, got %v", err)
	}

	peer := parseCertificate(t, identity.CertificatePEM)
	renewed, err := authority.Renew(peer, newCSR(t, "edge-01"))
	if err != nil {
		t.Fatalf("renew identity: %v", err)
	}
	if renewed.AgentID != identity.AgentID {
		t.Fatalf("expected agent ID %q, got %q", identity.AgentID, renewed.AgentID)
	}
	if renewed.CertificatePEM == identity.CertificatePEM {
		t.Fatal("expected a newly issued certificate")
	}
}

func TestEnrollmentRejectsInvalidBootstrapToken(t *testing.T) {
	t.Parallel()

	authority, err := enrollment.NewAuthority("correct-token")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}

	_, err = authority.Enroll(enrollment.Request{
		BootstrapToken:  "wrong-token",
		Name:            "edge-01",
		OperatingSystem: "linux",
		CSRPEM:          newCSR(t, "edge-01"),
	})
	if !errors.Is(err, enrollment.ErrInvalidToken) {
		t.Fatalf("expected invalid token error, got %v", err)
	}
}

func newCSR(t *testing.T, commonName string) string {
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

func parseCertificate(t *testing.T, certificatePEM string) *x509.Certificate {
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

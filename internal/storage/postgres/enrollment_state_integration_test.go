package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/enrollment"
)

func TestPostgresSharesEnrollmentAuthorityAndConsumesTokenOnce(t *testing.T) {
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	firstStore, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	defer firstStore.Close()
	secondStore, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open second store: %v", err)
	}
	defer secondStore.Close()

	token := "integration-" + time.Now().UTC().Format("20060102150405.000000000")
	tokenHash := sha256.Sum256([]byte(token))
	defer func() {
		_, _ = firstStore.pool.Exec(context.Background(), "DELETE FROM enrollment_tokens WHERE token_hash=$1", tokenHash[:])
	}()
	first, err := enrollment.NewPersistentAuthority(ctx, token, firstStore)
	if err != nil {
		t.Fatalf("create first persistent authority: %v", err)
	}
	second, err := enrollment.NewPersistentAuthority(ctx, token, secondStore)
	if err != nil {
		t.Fatalf("create second persistent authority: %v", err)
	}
	identity, err := first.EnrollContext(ctx, enrollment.Request{
		BootstrapToken: token, Name: "pg-agent-01", OperatingSystem: "linux", CSRPEM: integrationCSR(t),
	})
	if err != nil {
		t.Fatalf("enroll through first store: %v", err)
	}
	if _, err := second.Authenticate(integrationCertificate(t, identity.CertificatePEM)); err != nil {
		t.Fatalf("authenticate through second store authority: %v", err)
	}
	_, err = second.EnrollContext(ctx, enrollment.Request{
		BootstrapToken: token, Name: "pg-agent-02", OperatingSystem: "linux", CSRPEM: integrationCSR(t),
	})
	if !errors.Is(err, enrollment.ErrTokenConsumed) {
		t.Fatalf("expected shared token consumption, got %v", err)
	}

	staleToken := token + "-stale"
	rotatedToken := token + "-rotated"
	staleHash := sha256.Sum256([]byte(staleToken))
	rotatedHash := sha256.Sum256([]byte(rotatedToken))
	defer func() {
		_, _ = firstStore.pool.Exec(context.Background(), "DELETE FROM enrollment_tokens WHERE token_hash = ANY($1)", [][]byte{staleHash[:], rotatedHash[:]})
	}()
	staleAuthority, err := enrollment.NewPersistentAuthority(ctx, staleToken, firstStore)
	if err != nil {
		t.Fatalf("register stale token: %v", err)
	}
	if _, err := enrollment.NewPersistentAuthority(ctx, rotatedToken, secondStore); err != nil {
		t.Fatalf("register rotated token: %v", err)
	}
	_, err = staleAuthority.EnrollContext(ctx, enrollment.Request{
		BootstrapToken: staleToken, Name: "pg-agent-stale", OperatingSystem: "linux", CSRPEM: integrationCSR(t),
	})
	if !errors.Is(err, enrollment.ErrInvalidToken) {
		t.Fatalf("expected previous unused token to be revoked, got %v", err)
	}
}

func integrationCSR(t *testing.T) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "integration-agent"}}, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

func integrationCertificate(t *testing.T, value string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		t.Fatal("decode integration certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

package enrollment_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"sync"
	"testing"

	"github.com/gokayybaz/bazusop/internal/enrollment"
)

func TestPersistentAuthoritySharesCAAndTokenConsumption(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &fakeStateStore{tokens: make(map[[32]byte]tokenState)}
	first, err := enrollment.NewPersistentAuthority(ctx, "shared-token", store)
	if err != nil {
		t.Fatalf("create first authority: %v", err)
	}
	second, err := enrollment.NewPersistentAuthority(ctx, "shared-token", store)
	if err != nil {
		t.Fatalf("create second authority: %v", err)
	}
	if !bytes.Equal(first.JobSigningKey().Public().(ed25519.PublicKey), second.JobSigningKey().Public().(ed25519.PublicKey)) {
		t.Fatal("persistent authorities did not share the job signing trust root")
	}
	identity, err := first.EnrollContext(ctx, enrollment.Request{
		BootstrapToken: "shared-token", Name: "edge-01", OperatingSystem: "linux", CSRPEM: newCSR(t, "edge-01"),
	})
	if err != nil {
		t.Fatalf("enroll through first authority: %v", err)
	}
	if _, err := second.Authenticate(parseCertificate(t, identity.CertificatePEM)); err != nil {
		t.Fatalf("second authority did not trust shared CA identity: %v", err)
	}
	_, err = second.EnrollContext(ctx, enrollment.Request{
		BootstrapToken: "shared-token", Name: "edge-02", OperatingSystem: "windows", CSRPEM: newCSR(t, "edge-02"),
	})
	if !errors.Is(err, enrollment.ErrTokenConsumed) {
		t.Fatalf("expected token consumption to be shared, got %v", err)
	}

	rotated, err := enrollment.NewPersistentAuthority(ctx, "rotated-token", store)
	if err != nil {
		t.Fatalf("create rotated authority: %v", err)
	}
	if _, err := rotated.EnrollContext(ctx, enrollment.Request{
		BootstrapToken: "rotated-token", Name: "edge-03", OperatingSystem: "linux", CSRPEM: newCSR(t, "edge-03"),
	}); err != nil {
		t.Fatalf("enroll with rotated token: %v", err)
	}
}

func TestRegisteringRotatedTokenRevokesUnusedPreviousToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &fakeStateStore{tokens: make(map[[32]byte]tokenState)}
	oldAuthority, err := enrollment.NewPersistentAuthority(ctx, "old-token", store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enrollment.NewPersistentAuthority(ctx, "new-token", store); err != nil {
		t.Fatal(err)
	}
	_, err = oldAuthority.EnrollContext(ctx, enrollment.Request{
		BootstrapToken: "old-token", Name: "edge-old", OperatingSystem: "linux", CSRPEM: newCSR(t, "edge-old"),
	})
	if !errors.Is(err, enrollment.ErrInvalidToken) {
		t.Fatalf("expected rotated old token to be revoked, got %v", err)
	}
}

type tokenState struct {
	consumed bool
	revoked  bool
}

type fakeStateStore struct {
	mu     sync.Mutex
	state  enrollment.AuthorityState
	tokens map[[32]byte]tokenState
}

func (store *fakeStateStore) LoadOrCreateEnrollmentAuthority(_ context.Context, candidate enrollment.AuthorityState) (enrollment.AuthorityState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.state.CertificatePEM == "" {
		store.state = candidate
	}
	return store.state, nil
}

func (store *fakeStateStore) RegisterEnrollmentToken(_ context.Context, tokenHash [32]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.tokens[tokenHash]; exists {
		return nil
	}
	for hash, state := range store.tokens {
		if !state.consumed {
			state.revoked = true
			store.tokens[hash] = state
		}
	}
	store.tokens[tokenHash] = tokenState{}
	return nil
}

func (store *fakeStateStore) ConsumeEnrollmentToken(_ context.Context, tokenHash [32]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	state, exists := store.tokens[tokenHash]
	if !exists || state.revoked {
		return enrollment.ErrInvalidToken
	}
	if state.consumed {
		return enrollment.ErrTokenConsumed
	}
	state.consumed = true
	store.tokens[tokenHash] = state
	return nil
}

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

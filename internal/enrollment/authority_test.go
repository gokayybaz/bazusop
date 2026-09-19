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
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPersistentAuthoritySharesCAAndTokenConsumption(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newFakeStateStore(tenancy.DefaultScope())
	first, err := enrollment.NewPersistentAuthority(ctx, "shared-token", tenancy.DefaultScope(), store)
	if err != nil {
		t.Fatalf("create first authority: %v", err)
	}
	second, err := enrollment.NewPersistentAuthority(ctx, "shared-token", tenancy.DefaultScope(), store)
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

	rotated, err := enrollment.NewPersistentAuthority(ctx, "rotated-token", tenancy.DefaultScope(), store)
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
	store := newFakeStateStore(tenancy.DefaultScope())
	oldAuthority, err := enrollment.NewPersistentAuthority(ctx, "old-token", tenancy.DefaultScope(), store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enrollment.NewPersistentAuthority(ctx, "new-token", tenancy.DefaultScope(), store); err != nil {
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
	scope    tenancy.Scope
}

type fakeStateStore struct {
	mu         sync.Mutex
	state      enrollment.AuthorityState
	tokens     map[[32]byte]tokenState
	agents     map[string]tenancy.Agent
	resolveErr error
}

func newFakeStateStore(_ tenancy.Scope) *fakeStateStore {
	return &fakeStateStore{tokens: make(map[[32]byte]tokenState), agents: make(map[string]tenancy.Agent)}
}

func (store *fakeStateStore) deleteAgent(id string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.agents, id)
}

func (store *fakeStateStore) ResolveAgent(ctx context.Context, id string) (tenancy.Agent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return tenancy.Agent{}, err
	}
	if store.resolveErr != nil {
		return tenancy.Agent{}, store.resolveErr
	}
	agent, ok := store.agents[id]
	if !ok {
		return tenancy.Agent{}, tenancy.ErrNotFound
	}
	return agent, nil
}

func (store *fakeStateStore) LoadOrCreateEnrollmentAuthority(_ context.Context, candidate enrollment.AuthorityState) (enrollment.AuthorityState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.state.CertificatePEM == "" {
		store.state = candidate
	}
	return store.state, nil
}

func (store *fakeStateStore) RegisterEnrollmentToken(_ context.Context, tokenHash [32]byte, scope tenancy.Scope) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.tokens[tokenHash]; exists {
		return nil
	}
	for hash, state := range store.tokens {
		if !state.consumed && state.scope == scope {
			state.revoked = true
			store.tokens[hash] = state
		}
	}
	store.tokens[tokenHash] = tokenState{scope: scope}
	return nil
}

func (store *fakeStateStore) ConsumeEnrollmentToken(_ context.Context, tokenHash [32]byte, agentID string) (tenancy.Agent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	state, exists := store.tokens[tokenHash]
	if !exists || state.revoked {
		return tenancy.Agent{}, enrollment.ErrInvalidToken
	}
	if state.consumed {
		return tenancy.Agent{}, enrollment.ErrTokenConsumed
	}
	state.consumed = true
	store.tokens[tokenHash] = state
	agent := tenancy.Agent{ID: agentID, OrganizationID: state.scope.OrganizationID, SiteID: state.scope.SiteID}
	store.agents[agentID] = agent
	return agent, nil
}

func validRequest(t *testing.T, token string) enrollment.Request {
	t.Helper()
	return enrollment.Request{BootstrapToken: token, Name: "edge-01", OperatingSystem: "linux", CSRPEM: newCSR(t, "edge-01")}
}

func TestEnrollmentBindsAgentToTokenScope(t *testing.T) {
	scope := tenancy.Scope{OrganizationID: "org_default", SiteID: "site_istanbul"}
	store := newFakeStateStore(scope)
	authority, err := enrollment.NewPersistentAuthority(t.Context(), "bootstrap-secret", scope, store)
	if err != nil {
		t.Fatal(err)
	}
	// Another authority's configuration must not override the presented token's site.
	other, err := enrollment.NewPersistentAuthority(t.Context(), "other-secret", tenancy.DefaultScope(), store)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := other.EnrollContext(t.Context(), validRequest(t, "bootstrap-secret"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := authority.AuthenticateContext(t.Context(), parseCertificate(t, identity.CertificatePEM))
	if err != nil {
		t.Fatal(err)
	}
	if agent.ID != identity.AgentID || agent.Scope() != scope {
		t.Fatalf("unexpected scoped agent: %#v", agent)
	}
	renewed, err := other.RenewContext(t.Context(), parseCertificate(t, identity.CertificatePEM), newCSR(t, "renewed"))
	if err != nil {
		t.Fatal(err)
	}
	renewedAgent, err := authority.AuthenticateContext(t.Context(), parseCertificate(t, renewed.CertificatePEM))
	if err != nil || renewedAgent != agent {
		t.Fatalf("renewal changed scoped identity: %#v, %v", renewedAgent, err)
	}
}

func TestAuthenticationRejectsInvalidPersistentAgent(t *testing.T) {
	for _, scenario := range []string{"missing", "invalid scope", "different ID", "storage error", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			scope := tenancy.DefaultScope()
			store := newFakeStateStore(scope)
			authority, err := enrollment.NewPersistentAuthority(t.Context(), "bootstrap-secret", scope, store)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := authority.EnrollContext(t.Context(), validRequest(t, "bootstrap-secret"))
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			switch scenario {
			case "missing":
				store.deleteAgent(identity.AgentID)
			case "invalid scope":
				store.agents[identity.AgentID] = tenancy.Agent{ID: identity.AgentID}
			case "different ID":
				store.agents[identity.AgentID] = tenancy.Agent{ID: "other-agent", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}
			case "storage error":
				store.resolveErr = errors.New("private storage detail")
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			peer := parseCertificate(t, identity.CertificatePEM)
			if _, err := authority.AuthenticateContext(ctx, peer); err != enrollment.ErrInvalidIdentity {
				t.Fatalf("expected invalid identity only, got %v", err)
			}
			if _, err := authority.RenewContext(ctx, peer, newCSR(t, "renew")); err != enrollment.ErrInvalidIdentity {
				t.Fatalf("expected renewal rejection, got %v", err)
			}
			if _, err := authority.Authenticate(peer); err != nil {
				t.Fatalf("legacy certificate verification failed: %v", err)
			}
		})
	}
}

func TestPersistentAuthorityRejectsInvalidScopeBeforeStoringState(t *testing.T) {
	store := newFakeStateStore(tenancy.DefaultScope())
	if _, err := enrollment.NewPersistentAuthority(t.Context(), "bootstrap-secret", tenancy.Scope{}, store); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("expected invalid scope, got %v", err)
	}
	if store.state.CertificatePEM != "" || len(store.tokens) != 0 {
		t.Fatal("invalid scope changed persistent state")
	}
}

func TestMemoryAuthorityResolvesDefaultScope(t *testing.T) {
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := authority.Enroll(validRequest(t, "bootstrap-secret"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := authority.AuthenticateContext(t.Context(), parseCertificate(t, identity.CertificatePEM))
	if err != nil || agent.ID != identity.AgentID || agent.Scope() != tenancy.DefaultScope() {
		t.Fatalf("unexpected memory identity: %#v, %v", agent, err)
	}
	if _, err := authority.AuthenticateContext(t.Context(), nil); err != enrollment.ErrInvalidIdentity {
		t.Fatalf("nil peer: %v", err)
	}
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

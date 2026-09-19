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
	"sync"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresSharesEnrollmentAuthorityAndConsumesTokenOnce(t *testing.T) {
	database := newIsolatedPostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	firstStore := database.Open(t)
	secondStore := database.Open(t)

	token := "integration-" + time.Now().UTC().Format("20060102150405.000000000")
	first, err := enrollment.NewPersistentAuthority(ctx, token, tenancy.DefaultScope(), firstStore)
	if err != nil {
		t.Fatalf("create first persistent authority: %v", err)
	}
	second, err := enrollment.NewPersistentAuthority(ctx, token, tenancy.DefaultScope(), secondStore)
	if err != nil {
		t.Fatalf("create second persistent authority: %v", err)
	}
	identity, err := first.EnrollContext(ctx, enrollment.Request{
		BootstrapToken: token, Name: "pg-agent-01", OperatingSystem: "linux", CSRPEM: integrationCSR(t),
	})
	if err != nil {
		t.Fatalf("enroll through first store: %v", err)
	}
	if agent, err := second.AuthenticateContext(ctx, integrationCertificate(t, identity.CertificatePEM)); err != nil || agent.ID != identity.AgentID || agent.Scope() != tenancy.DefaultScope() {
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
	staleAuthority, err := enrollment.NewPersistentAuthority(ctx, staleToken, tenancy.DefaultScope(), firstStore)
	if err != nil {
		t.Fatalf("register stale token: %v", err)
	}
	if _, err := enrollment.NewPersistentAuthority(ctx, rotatedToken, tenancy.DefaultScope(), secondStore); err != nil {
		t.Fatalf("register rotated token: %v", err)
	}
	_, err = staleAuthority.EnrollContext(ctx, enrollment.Request{
		BootstrapToken: staleToken, Name: "pg-agent-stale", OperatingSystem: "linux", CSRPEM: integrationCSR(t),
	})
	if !errors.Is(err, enrollment.ErrInvalidToken) {
		t.Fatalf("expected previous unused token to be revoked, got %v", err)
	}
}

func TestPostgresEnrollmentSiteIsolationAndAtomicConsumption(t *testing.T) {
	database := newIsolatedPostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store := database.Open(t)
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	org := "enrollment-org-" + suffix
	scopeA := tenancy.Scope{OrganizationID: org, SiteID: "site-a-" + suffix}
	scopeB := tenancy.Scope{OrganizationID: org, SiteID: "site-b-" + suffix}
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Enrollment test')`, org); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []tenancy.Scope{scopeA, scopeB} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1, $2, $1, $1)`, scope.SiteID, org); err != nil {
			t.Fatal(err)
		}
	}
	oldA := sha256.Sum256([]byte("old-a-" + suffix))
	newA := sha256.Sum256([]byte("new-a-" + suffix))
	tokenB := sha256.Sum256([]byte("token-b-" + suffix))
	for _, token := range []struct {
		hash  [32]byte
		scope tenancy.Scope
	}{{oldA, scopeA}, {tokenB, scopeB}, {newA, scopeA}} {
		if err := store.RegisterEnrollmentToken(ctx, token.hash, token.scope); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.ConsumeEnrollmentToken(ctx, oldA, "revoked-"+suffix); !errors.Is(err, enrollment.ErrInvalidToken) {
		t.Fatalf("old site A token was not revoked: %v", err)
	}
	agentB, err := store.ConsumeEnrollmentToken(ctx, tokenB, "agent-b-"+suffix)
	if err != nil || agentB.Scope() != scopeB {
		t.Fatalf("rotation revoked another site's token: %#v, %v", agentB, err)
	}
	// A duplicate agent insertion must leave the token available for retry.
	if _, err := store.ConsumeEnrollmentToken(ctx, newA, agentB.ID); err == nil {
		t.Fatal("expected duplicate agent insertion failure")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"agent-a1-" + suffix, "agent-a2-" + suffix} {
		wg.Go(func() { _, err := store.ConsumeEnrollmentToken(ctx, newA, id); results <- err })
	}
	wg.Wait()
	close(results)
	successes, consumed := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, enrollment.ErrTokenConsumed):
			consumed++
		default:
			t.Fatalf("unexpected consume error: %v", err)
		}
	}
	if successes != 1 || consumed != 1 {
		t.Fatalf("success=%d consumed=%d", successes, consumed)
	}
	var count int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM agents WHERE organization_id=$1 AND site_id=$2`, org, scopeA.SiteID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("agent count=%d, err=%v", count, err)
	}
	var boundID string
	if err := store.pool.QueryRow(ctx, `SELECT consumed_by_agent_id FROM enrollment_tokens WHERE token_hash=$1`, newA[:]).Scan(&boundID); err != nil {
		t.Fatal(err)
	}
	if agent, err := store.ResolveAgent(ctx, boundID); err != nil || agent.Scope() != scopeA {
		t.Fatalf("token's agent mapping=%#v, err=%v", agent, err)
	}
	if _, err := store.ResolveAgent(ctx, "missing-"+suffix); !errors.Is(err, tenancy.ErrNotFound) {
		t.Fatalf("missing agent: %v", err)
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

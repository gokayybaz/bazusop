package oidc_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/gokayybaz/bazusop/internal/oidc"
)

// mockIdP is a minimal OIDC provider for tests: it serves discovery, JWKS
// and token endpoints, and hand-signs a real RS256 ID token so
// internal/oidc's signature/issuer/audience/expiry/nonce verification runs
// against real cryptography rather than a stub. It never serves the
// authorization endpoint — tests call Provider.AuthCodeURL directly and
// extract state/nonce/code_challenge from the returned URL string, since
// nothing in this package or its caller ever needs the browser to actually
// navigate there.
type mockIdP struct {
	server            *httptest.Server
	key               *rsa.PrivateKey
	audience          string
	subject           string
	email             string
	emailVerified     bool
	expectedNonce     string
	expectedChallenge string
	issueExpiredToken bool
}

func newMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	idp := &mockIdP{key: key, emailVerified: true}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"issuer":                                idp.server.URL,
			"authorization_endpoint":                idp.server.URL + "/authorize",
			"token_endpoint":                        idp.server.URL + "/token",
			"jwks_uri":                              idp.server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &idp.key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(response http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		if idp.expectedChallenge != "" {
			sum := sha256.Sum256([]byte(request.FormValue("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != idp.expectedChallenge {
				http.Error(response, "pkce verifier does not match the original challenge", http.StatusBadRequest)
				return
			}
		}
		expiry := time.Now().Add(time.Hour)
		if idp.issueExpiredToken {
			expiry = time.Now().Add(-time.Hour)
		}
		claims := map[string]any{
			"iss": idp.server.URL, "sub": idp.subject, "aud": idp.audience,
			"exp": expiry.Unix(), "iat": time.Now().Unix(), "nonce": idp.expectedNonce,
			"email": idp.email, "email_verified": idp.emailVerified,
		}
		payload, err := json.Marshal(claims)
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		signed, err := signer.Sign(payload)
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		compact, err := signed.CompactSerialize()
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"access_token": "test-access-token", "token_type": "Bearer", "id_token": compact,
		})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func newTestProvider(t *testing.T, idp *mockIdP) *oidc.Provider {
	t.Helper()
	provider, err := oidc.NewProvider(t.Context(), oidc.Configuration{
		DiscoveryURL: idp.server.URL + "/.well-known/openid-configuration",
		Issuer:       idp.server.URL, ClientID: idp.audience, ClientSecret: "test-secret",
		RedirectURL: "https://hub.example.com/api/v1/oidc/callback",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	return provider
}

func primeIdPFromAuthCodeURL(t *testing.T, idp *mockIdP, authCodeURL string) {
	t.Helper()
	parsed, err := url.Parse(authCodeURL)
	if err != nil {
		t.Fatalf("parse auth code url: %v", err)
	}
	if parsed.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("expected an S256 PKCE challenge method, got %q", parsed.Query().Get("code_challenge_method"))
	}
	idp.expectedChallenge = parsed.Query().Get("code_challenge")
}

func TestExchangeVerifiesSignatureIssuerAudienceAndNonce(t *testing.T) {
	t.Parallel()
	idp := newMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-123", "user@example.com"
	provider := newTestProvider(t, idp)

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}
	primeIdPFromAuthCodeURL(t, idp, provider.AuthCodeURL(flow))
	idp.expectedNonce = flow.Nonce

	claims, err := provider.Exchange(t.Context(), "test-code", flow)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if claims.Subject != "subject-123" || claims.Email != "user@example.com" || !claims.EmailVerified {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestExchangeRejectsAMismatchedNonce(t *testing.T) {
	t.Parallel()
	idp := newMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-1", "a@example.com"
	provider := newTestProvider(t, idp)

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatal(err)
	}
	primeIdPFromAuthCodeURL(t, idp, provider.AuthCodeURL(flow))
	idp.expectedNonce = "a-different-nonce" // simulates a forged/replayed token

	if _, err := provider.Exchange(t.Context(), "test-code", flow); !errors.Is(err, oidc.ErrNonceMismatch) {
		t.Fatalf("expected ErrNonceMismatch, got %v", err)
	}
}

func TestExchangeRejectsAnExpiredIDToken(t *testing.T) {
	t.Parallel()
	idp := newMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-1", "a@example.com"
	idp.issueExpiredToken = true
	provider := newTestProvider(t, idp)

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatal(err)
	}
	primeIdPFromAuthCodeURL(t, idp, provider.AuthCodeURL(flow))
	idp.expectedNonce = flow.Nonce

	if _, err := provider.Exchange(t.Context(), "test-code", flow); err == nil {
		t.Fatal("expected an error for an expired id token")
	}
}

func TestExchangeRejectsAWrongPKCEVerifier(t *testing.T) {
	t.Parallel()
	idp := newMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-1", "a@example.com"
	provider := newTestProvider(t, idp)

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatal(err)
	}
	primeIdPFromAuthCodeURL(t, idp, provider.AuthCodeURL(flow))
	idp.expectedNonce = flow.Nonce
	tampered := flow
	tampered.CodeVerifier = "a-different-verifier-that-does-not-match-the-challenge"

	if _, err := provider.Exchange(t.Context(), "test-code", tampered); err == nil {
		t.Fatal("expected an error for a mismatched PKCE verifier")
	}
}

func TestNewProviderRejectsAnIssuerMismatch(t *testing.T) {
	t.Parallel()
	idp := newMockIdP(t)
	_, err := oidc.NewProvider(t.Context(), oidc.Configuration{
		DiscoveryURL: idp.server.URL + "/.well-known/openid-configuration",
		Issuer:       "https://not-the-real-issuer.example.com", ClientID: "test-client", ClientSecret: "test-secret",
		RedirectURL: "https://hub.example.com/api/v1/oidc/callback",
	})
	if !errors.Is(err, oidc.ErrIssuerMismatch) {
		t.Fatalf("expected ErrIssuerMismatch, got %v", err)
	}
}

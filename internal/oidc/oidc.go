// Package oidc wraps golang.org/x/oauth2 and github.com/coreos/go-oidc/v3
// into the Authorization Code + PKCE flow spike 11.9 needs: nothing in
// this package persists anything or knows about bazUSOP's user/invite
// model — internal/server passes in an organization's stored configuration
// and gets back verified claims (subject, email, acr, amr) to hand to
// internal/identity.
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	oidclib "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ErrIssuerMismatch = errors.New("discovered issuer does not match the configured allowed issuer")
	ErrNonceMismatch  = errors.New("id token nonce does not match the login flow")
)

// Configuration is one organization's OIDC settings, already decrypted —
// internal/identity is the source of truth and the only thing that
// touches the client secret at rest.
type Configuration struct {
	DiscoveryURL string
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Flow carries the per-attempt state a caller must round-trip through the
// browser (in a short-lived cookie, see internal/server) between
// AuthCodeURL and Exchange: state (CSRF/replay), nonce (ID token replay)
// and the PKCE code_verifier (authorization code interception).
type Flow struct {
	State        string
	Nonce        string
	CodeVerifier string
}

func NewFlow() (Flow, error) {
	state, err := randomString()
	if err != nil {
		return Flow{}, err
	}
	nonce, err := randomString()
	if err != nil {
		return Flow{}, err
	}
	return Flow{State: state, Nonce: nonce, CodeVerifier: oauth2.GenerateVerifier()}, nil
}

func randomString() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return hex.EncodeToString(value), nil
}

// Claims is the subset of a verified ID token spike 11.9 needs: Subject
// (the permanent issuer+subject identity), Email (used only for the
// one-time invite lookup on first login) and, if the IdP included them,
// acr/amr (see the design spec's "OIDC kullanıcı" section — not otherwise
// interpreted in this spike).
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	ACR           string
	AMR           []string
}

type rawClaims struct {
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	ACR           string   `json:"acr"`
	AMR           []string `json:"amr"`
}

// Provider wraps one organization's OIDC configuration into ready-to-use
// OAuth2 + ID-token-verification machinery.
type Provider struct {
	oauth2Config oauth2.Config
	verifier     *oidclib.IDTokenVerifier
}

// NewProvider fetches configuration.DiscoveryURL (the platform
// administrator's explicitly configured discovery document — not
// necessarily the same host path go-oidc would derive on its own) and
// confirms its issuer matches the separately configured allowed issuer;
// the design spec requires these to be validated as two distinct admin
// inputs. go-oidc's own NewProvider takes an issuer, not a discovery URL —
// it derives "{issuer}/.well-known/openid-configuration" itself and
// re-validates the same issuer match internally — so the standard
// discovery + JWKS/OAuth2 wiring below is built from configuration.Issuer,
// once the explicit discovery-URL check above has already passed.
func NewProvider(ctx context.Context, configuration Configuration) (*Provider, error) {
	if err := verifyDiscoveredIssuer(ctx, configuration.DiscoveryURL, configuration.Issuer); err != nil {
		return nil, err
	}
	provider, err := oidclib.NewProvider(ctx, configuration.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider: %w", err)
	}
	return &Provider{
		oauth2Config: oauth2.Config{
			ClientID: configuration.ClientID, ClientSecret: configuration.ClientSecret,
			Endpoint: provider.Endpoint(), RedirectURL: configuration.RedirectURL,
			Scopes: []string{oidclib.ScopeOpenID, "email"},
		},
		verifier: provider.Verifier(&oidclib.Config{ClientID: configuration.ClientID}),
	}, nil
}

func verifyDiscoveredIssuer(ctx context.Context, discoveryURL, expectedIssuer string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return fmt.Errorf("build discovery request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("fetch discovery document: %w", err)
	}
	defer response.Body.Close()
	var discovered struct {
		Issuer string `json:"issuer"`
	}
	if err := json.NewDecoder(response.Body).Decode(&discovered); err != nil {
		return fmt.Errorf("decode discovery document: %w", err)
	}
	if discovered.Issuer != expectedIssuer {
		return ErrIssuerMismatch
	}
	return nil
}

// AuthCodeURL builds the IdP redirect target for flow, binding state,
// nonce and a PKCE S256 challenge to this one login attempt.
func (provider *Provider) AuthCodeURL(flow Flow) string {
	return provider.oauth2Config.AuthCodeURL(flow.State, oidclib.Nonce(flow.Nonce), oauth2.S256ChallengeOption(flow.CodeVerifier))
}

// Exchange completes the callback half of the flow: exchanges code for
// tokens (presenting the PKCE verifier), then verifies the returned ID
// token's signature, issuer, audience and expiry (via the JWKS-backed
// verifier from NewProvider) and its nonce against flow.Nonce.
func (provider *Provider) Exchange(ctx context.Context, code string, flow Flow) (Claims, error) {
	token, err := provider.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(flow.CodeVerifier))
	if err != nil {
		return Claims{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Claims{}, fmt.Errorf("token response did not include an id_token")
	}
	idToken, err := provider.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Claims{}, fmt.Errorf("verify id token: %w", err)
	}
	if idToken.Nonce != flow.Nonce {
		return Claims{}, ErrNonceMismatch
	}
	var extra rawClaims
	if err := idToken.Claims(&extra); err != nil {
		return Claims{}, fmt.Errorf("decode id token claims: %w", err)
	}
	return Claims{
		Subject: idToken.Subject, Email: extra.Email, EmailVerified: extra.EmailVerified,
		ACR: extra.ACR, AMR: extra.AMR,
	}, nil
}

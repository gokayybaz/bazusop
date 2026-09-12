package enrollment

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidToken    = errors.New("invalid bootstrap token")
	ErrTokenConsumed   = errors.New("bootstrap token already consumed")
	ErrInvalidRequest  = errors.New("invalid enrollment request")
	ErrInvalidIdentity = errors.New("invalid agent identity")
)

const certificateLifetime = 24 * time.Hour

type Request struct {
	BootstrapToken  string `json:"bootstrap_token"`
	Name            string `json:"name"`
	OperatingSystem string `json:"operating_system"`
	CSRPEM          string `json:"csr"`
}

type Identity struct {
	AgentID          string    `json:"agent_id"`
	CertificatePEM   string    `json:"certificate"`
	CACertificatePEM string    `json:"ca_certificate"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type AuthorityState struct {
	CertificatePEM string
	PrivateKeyPEM  string
}

type StateStore interface {
	LoadOrCreateEnrollmentAuthority(context.Context, AuthorityState) (AuthorityState, error)
	RegisterEnrollmentToken(context.Context, [sha256.Size]byte) error
	ConsumeEnrollmentToken(context.Context, [sha256.Size]byte) error
}

type tokenConsumer interface {
	ConsumeEnrollmentToken(context.Context, [sha256.Size]byte) error
}

type Authority struct {
	caCertificate *x509.Certificate
	caPrivateKey  ed25519.PrivateKey
	caPEM         string
	tokens        tokenConsumer
	now           func() time.Time
}

func NewAuthority(bootstrapToken string) (*Authority, error) {
	if err := validateBootstrapToken(bootstrapToken); err != nil {
		return nil, err
	}
	state, err := generateAuthorityState()
	if err != nil {
		return nil, err
	}
	tokens := &memoryTokenStore{bootstrapHash: sha256.Sum256([]byte(bootstrapToken))}
	return authorityFromState(state, tokens)
}

func NewPersistentAuthority(ctx context.Context, bootstrapToken string, store StateStore) (*Authority, error) {
	if err := validateBootstrapToken(bootstrapToken); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("enrollment state store is required")
	}
	candidate, err := generateAuthorityState()
	if err != nil {
		return nil, err
	}
	state, err := store.LoadOrCreateEnrollmentAuthority(ctx, candidate)
	if err != nil {
		return nil, fmt.Errorf("load enrollment authority: %w", err)
	}
	tokenHash := sha256.Sum256([]byte(bootstrapToken))
	if err := store.RegisterEnrollmentToken(ctx, tokenHash); err != nil {
		return nil, fmt.Errorf("register enrollment token: %w", err)
	}
	return authorityFromState(state, store)
}

func GenerateBootstrapToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate bootstrap token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (authority *Authority) ClientCAPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(authority.caCertificate)
	return pool
}

func (authority *Authority) Enroll(request Request) (Identity, error) {
	return authority.EnrollContext(context.Background(), request)
}

func (authority *Authority) EnrollContext(ctx context.Context, request Request) (Identity, error) {
	csr, err := parseCSR(request.CSRPEM)
	if err != nil || strings.TrimSpace(request.Name) == "" || !supportedOS(request.OperatingSystem) {
		return Identity{}, ErrInvalidRequest
	}
	agentID, err := randomAgentID()
	if err != nil {
		return Identity{}, fmt.Errorf("generate agent ID: %w", err)
	}
	identity, err := authority.issue(agentID, strings.ToLower(request.OperatingSystem), csr)
	if err != nil {
		return Identity{}, err
	}
	providedHash := sha256.Sum256([]byte(request.BootstrapToken))
	if err := authority.tokens.ConsumeEnrollmentToken(ctx, providedHash); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (authority *Authority) Renew(peer *x509.Certificate, csrPEM string) (Identity, error) {
	csr, err := parseCSR(csrPEM)
	if err != nil || peer == nil {
		return Identity{}, ErrInvalidIdentity
	}
	agentID, err := authority.Authenticate(peer)
	if err != nil {
		return Identity{}, err
	}
	operatingSystem := "unknown"
	if len(peer.Subject.OrganizationalUnit) > 0 {
		operatingSystem = peer.Subject.OrganizationalUnit[0]
	}

	return authority.issue(agentID, operatingSystem, csr)
}

type memoryTokenStore struct {
	mu            sync.Mutex
	bootstrapHash [sha256.Size]byte
	consumed      bool
}

func (store *memoryTokenStore) ConsumeEnrollmentToken(_ context.Context, providedHash [sha256.Size]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if subtle.ConstantTimeCompare(providedHash[:], store.bootstrapHash[:]) != 1 {
		return ErrInvalidToken
	}
	if store.consumed {
		return ErrTokenConsumed
	}
	store.consumed = true
	return nil
}

func validateBootstrapToken(token string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("%w: token is required", ErrInvalidToken)
	}
	return nil
}

func generateAuthorityState() (AuthorityState, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return AuthorityState{}, fmt.Errorf("generate CA key: %w", err)
	}
	now := time.Now().UTC()
	serial, err := randomSerial()
	if err != nil {
		return AuthorityState{}, fmt.Errorf("generate CA serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "bazUSOP Agent CA", Organization: []string{"bazUSOP"}},
		NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return AuthorityState{}, fmt.Errorf("create CA certificate: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return AuthorityState{}, fmt.Errorf("encode CA private key: %w", err)
	}
	return AuthorityState{
		CertificatePEM: encodeCertificate(der),
		PrivateKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
	}, nil
}

func authorityFromState(state AuthorityState, tokens tokenConsumer) (*Authority, error) {
	certificateBlock, _ := pem.Decode([]byte(state.CertificatePEM))
	privateKeyBlock, _ := pem.Decode([]byte(state.PrivateKeyPEM))
	if certificateBlock == nil || certificateBlock.Type != "CERTIFICATE" || privateKeyBlock == nil || privateKeyBlock.Type != "PRIVATE KEY" {
		return nil, errors.New("stored enrollment authority is invalid")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil || !certificate.IsCA {
		return nil, errors.New("stored enrollment CA certificate is invalid")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(privateKeyBlock.Bytes)
	if err != nil {
		return nil, errors.New("stored enrollment CA private key is invalid")
	}
	privateKey, ok := parsedKey.(ed25519.PrivateKey)
	if !ok || !bytes.Equal(certificate.RawSubjectPublicKeyInfo, mustMarshalPublicKey(privateKey.Public())) {
		return nil, errors.New("stored enrollment CA key does not match certificate")
	}
	return &Authority{caCertificate: certificate, caPrivateKey: privateKey, caPEM: state.CertificatePEM, tokens: tokens, now: time.Now}, nil
}

func mustMarshalPublicKey(publicKey any) []byte {
	der, _ := x509.MarshalPKIXPublicKey(publicKey)
	return der
}

func (authority *Authority) Authenticate(peer *x509.Certificate) (string, error) {
	if peer == nil {
		return "", ErrInvalidIdentity
	}
	roots := x509.NewCertPool()
	roots.AddCert(authority.caCertificate)
	if _, err := peer.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: authority.now().UTC(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return "", ErrInvalidIdentity
	}

	agentID := agentIDFromCertificate(peer)
	if agentID == "" {
		return "", ErrInvalidIdentity
	}
	return agentID, nil
}

func (authority *Authority) issue(agentID, operatingSystem string, csr *x509.CertificateRequest) (Identity, error) {
	now := authority.now().UTC()
	serial, err := randomSerial()
	if err != nil {
		return Identity{}, fmt.Errorf("generate certificate serial: %w", err)
	}
	identityURL, err := url.Parse("spiffe://bazusop/agent/" + agentID)
	if err != nil {
		return Identity{}, fmt.Errorf("build agent identity: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         "bazusop-agent-" + agentID,
			Organization:       []string{"bazUSOP Agents"},
			OrganizationalUnit: []string{operatingSystem},
		},
		URIs:        []*url.URL{identityURL},
		NotBefore:   now.Add(-time.Minute),
		NotAfter:    now.Add(certificateLifetime),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, authority.caCertificate, csr.PublicKey, authority.caPrivateKey)
	if err != nil {
		return Identity{}, fmt.Errorf("issue agent certificate: %w", err)
	}
	return Identity{
		AgentID:          agentID,
		CertificatePEM:   encodeCertificate(der),
		CACertificatePEM: authority.caPEM,
		ExpiresAt:        template.NotAfter,
	}, nil
}

func parseCSR(csrPEM string) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, ErrInvalidRequest
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return nil, ErrInvalidRequest
	}
	return csr, nil
}

func supportedOS(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "linux", "windows":
		return true
	default:
		return false
	}
}

func agentIDFromCertificate(certificate *x509.Certificate) string {
	for _, identityURL := range certificate.URIs {
		if identityURL.Scheme == "spiffe" && identityURL.Host == "bazusop" {
			parts := strings.Split(strings.Trim(identityURL.Path, "/"), "/")
			if len(parts) == 2 && parts[0] == "agent" && parts[1] != "" {
				return parts[1]
			}
		}
	}
	return ""
}

func randomAgentID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func encodeCertificate(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

package serviceaccounts

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gokayybaz/bazusop/internal/authorization"
)

const TokenPrefix = "bazusop_sat_"

const (
	defaultExpiryDays = 90
	maxExpiryDays     = 365
	pepperKeyVersion  = 0x01
)

type ServiceAccount struct {
	ID             string
	OrganizationID string
	SiteID         string
	Name           string
	Role           authorization.SiteRole
	CreatedAt      time.Time
	DisabledAt     *time.Time
}

type Token struct {
	ID               string
	ServiceAccountID string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	LastUsedAt       *time.Time
	LastUsedIP       string
	RevokedAt        *time.Time
}

var (
	ErrInvalidRole     = errors.New("service accounts may only hold site-admin, operator, or viewer roles")
	ErrInvalidExpiry   = errors.New("token expiry must be between 1 and 365 days")
	ErrAccountNotFound = errors.New("service account not found")
	ErrAccountDisabled = errors.New("service account is disabled")
	ErrTokenNotFound   = errors.New("service account token not found")
	ErrTokenRevoked    = errors.New("service account token revoked")
	ErrTokenExpired    = errors.New("service account token expired")
)

// ParseToken splits a presented bearer value into its non-secret tokenID
// (safe to log — internal/server/middleware.go's deriveActor uses this
// directly, with no DB round trip or pepper needed) and its high-entropy
// secret (never logged; only ever compared via Service.Validate).
func ParseToken(token string) (tokenID, secret string, ok bool) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(token, TokenPrefix)
	parts := strings.SplitN(rest, "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

type Store interface {
	CreateAccount(ctx context.Context, account ServiceAccount) error
	AccountByID(ctx context.Context, id string) (ServiceAccount, error)
	DisableAccount(ctx context.Context, id string, at time.Time) error
	AccountsForSite(ctx context.Context, siteID string) ([]ServiceAccount, error)
	CreateToken(ctx context.Context, token Token, secretHash string) error
	TokenByID(ctx context.Context, id string) (Token, string, error)
	RevokeToken(ctx context.Context, id string, at time.Time) error
	RevokeActiveTokensForAccount(ctx context.Context, accountID string, at time.Time) error
	Touch(ctx context.Context, tokenID string, at time.Time, sourceIP string) error
}

type Service struct {
	store  Store
	pepper string
	now    func() time.Time
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option { return func(service *Service) { service.now = clock } }

func NewService(store Store, pepper string, options ...Option) (*Service, error) {
	if pepper == "" {
		return nil, errors.New("service account token pepper is required")
	}
	service := &Service{store: store, pepper: pepper, now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		option(service)
	}
	return service, nil
}

func (service *Service) CreateAccount(ctx context.Context, organizationID, siteID, name string, role authorization.SiteRole, expiryDays int) (ServiceAccount, string, error) {
	if role != authorization.SiteRoleAdmin && role != authorization.SiteRoleOperator && role != authorization.SiteRoleViewer {
		return ServiceAccount{}, "", ErrInvalidRole
	}
	id, err := newOpaqueHex(16)
	if err != nil {
		return ServiceAccount{}, "", err
	}
	account := ServiceAccount{ID: id, OrganizationID: organizationID, SiteID: siteID, Name: name, Role: role, CreatedAt: service.now()}
	if err := service.store.CreateAccount(ctx, account); err != nil {
		return ServiceAccount{}, "", err
	}
	token, err := service.issueToken(ctx, id, expiryDays)
	if err != nil {
		return ServiceAccount{}, "", err
	}
	return account, token, nil
}

func (service *Service) RotateToken(ctx context.Context, accountID string, expiryDays int) (string, error) {
	if err := service.store.RevokeActiveTokensForAccount(ctx, accountID, service.now()); err != nil {
		return "", err
	}
	return service.issueToken(ctx, accountID, expiryDays)
}

func (service *Service) RevokeToken(ctx context.Context, tokenID string) error {
	return service.store.RevokeToken(ctx, tokenID, service.now())
}

func (service *Service) DisableAccount(ctx context.Context, accountID string) error {
	if err := service.store.RevokeActiveTokensForAccount(ctx, accountID, service.now()); err != nil {
		return err
	}
	return service.store.DisableAccount(ctx, accountID, service.now())
}

func (service *Service) ListForSite(ctx context.Context, siteID string) ([]ServiceAccount, error) {
	return service.store.AccountsForSite(ctx, siteID)
}

// Validate looks the token up by its non-secret tokenID (O(1), indexed),
// then compares the presented secret's HMAC against the stored hash in
// constant time. A wrong secret for a real tokenID is reported identically
// to an unknown tokenID (ErrTokenNotFound) — never distinguished, so a
// caller can't use response differences to enumerate valid token IDs.
func (service *Service) Validate(ctx context.Context, presented, sourceIP string) (ServiceAccount, error) {
	tokenID, secret, ok := ParseToken(presented)
	if !ok {
		return ServiceAccount{}, ErrTokenNotFound
	}
	token, storedHash, err := service.store.TokenByID(ctx, tokenID)
	if errors.Is(err, ErrTokenNotFound) {
		return ServiceAccount{}, ErrTokenNotFound
	}
	if err != nil {
		return ServiceAccount{}, err
	}
	if subtle.ConstantTimeCompare([]byte(hashSecret(secret, service.pepper)), []byte(storedHash)) != 1 {
		return ServiceAccount{}, ErrTokenNotFound
	}
	if token.RevokedAt != nil {
		return ServiceAccount{}, ErrTokenRevoked
	}
	if service.now().After(token.ExpiresAt) {
		return ServiceAccount{}, ErrTokenExpired
	}
	account, err := service.store.AccountByID(ctx, token.ServiceAccountID)
	if err != nil {
		return ServiceAccount{}, err
	}
	if account.DisabledAt != nil {
		return ServiceAccount{}, ErrAccountDisabled
	}
	if err := service.store.Touch(ctx, token.ID, service.now(), sourceIP); err != nil {
		return ServiceAccount{}, err
	}
	return account, nil
}

func (service *Service) issueToken(ctx context.Context, accountID string, expiryDays int) (string, error) {
	if expiryDays == 0 {
		expiryDays = defaultExpiryDays
	}
	if expiryDays < 1 || expiryDays > maxExpiryDays {
		return "", ErrInvalidExpiry
	}
	tokenID, err := newOpaqueHex(8)
	if err != nil {
		return "", err
	}
	secret, err := newOpaqueHex(32)
	if err != nil {
		return "", err
	}
	now := service.now()
	token := Token{ID: tokenID, ServiceAccountID: accountID, CreatedAt: now, ExpiresAt: now.AddDate(0, 0, expiryDays)}
	if err := service.store.CreateToken(ctx, token, hashSecret(secret, service.pepper)); err != nil {
		return "", err
	}
	return TokenPrefix + tokenID + "_" + secret, nil
}

// hashSecret is HMAC-SHA-256 keyed by an operator-supplied pepper kept
// outside the database (BAZUSOP_SERVICE_ACCOUNT_PEPPER), not plain SHA-256
// — a deliberate step up from the plain-hash pattern used for session and
// invite tokens elsewhere in this codebase, since a leaked database alone
// is not enough to brute-force these longer-lived (90-365 day) credentials;
// the pepper must also leak. The one-byte version prefix mirrors
// internal/identity's EncryptTOTPSecret pattern for future pepper rotation.
func hashSecret(secret, pepper string) string {
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte(secret))
	versioned := append([]byte{pepperKeyVersion}, mac.Sum(nil)...)
	return hex.EncodeToString(versioned)
}

func newOpaqueHex(byteLen int) (string, error) {
	value := make([]byte, byteLen)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate token component: %w", err)
	}
	return hex.EncodeToString(value), nil
}

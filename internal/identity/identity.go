package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type Role string

const RolePlatformAdmin Role = "platform-admin"

type User struct {
	ID                  string
	OrganizationID      string
	Email               string
	Role                Role
	PasswordHash        string
	TOTPSecretEncrypted []byte
	TOTPConfirmedAt     *time.Time
	OIDCIssuer          string
	OIDCSubject         string
	CreatedAt           time.Time
	DisabledAt          *time.Time
}

type IdentityType string

const (
	IdentityTypeLocal IdentityType = "local"
	IdentityTypeOIDC  IdentityType = "oidc"
)

type Invite struct {
	ID             string
	OrganizationID string
	Email          string
	Role           Role
	SiteRoleGrants []SiteRoleGrant
	IdentityType   IdentityType
	CreatedBy      string
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

// OIDCConfiguration is one organization's OIDC settings. ClientSecretEncrypted
// is only ever populated by Service.OIDCConfigurationWithSecret (internal
// use, login/callback handlers); Service.OIDCConfiguration always zeroes it
// — the design spec requires the client secret never be returned by any
// read API.
type OIDCConfiguration struct {
	OrganizationID        string
	DiscoveryURL          string
	Issuer                string
	ClientID              string
	ClientSecretEncrypted []byte
	RedirectURL           string
	UpdatedAt             time.Time
	UpdatedBy             string
}

type SiteRoleGrant struct {
	SiteID string
	Role   string
}

// SiteRoleGrantor completes site-role assignment after ConsumeInvite
// creates a new non-platform-admin user. internal/authorization provides
// the production implementation; identity stays decoupled from its
// concrete SiteRole type by passing the role as a plain string.
type SiteRoleGrantor func(ctx context.Context, userID string, grants []SiteRoleGrant) error

// TOTPEnrollment is returned once, at bootstrap or invite-consumption time
// (provisioning URI) and once more at TOTP confirmation time (recovery
// codes). It deliberately never carries the raw TOTP secret — only what a
// UI needs to render a QR code and what a user needs to save for account
// recovery.
type TOTPEnrollment struct {
	ProvisioningURI string
	RecoveryCodes   []string
}

var (
	ErrAlreadyBootstrapped     = errors.New("hub is already bootstrapped")
	ErrInvalidBootstrapSecret  = errors.New("invalid bootstrap secret")
	ErrInviteNotFound          = errors.New("invite not found")
	ErrInviteExpired           = errors.New("invite expired or already consumed")
	ErrInvalidCredentials      = errors.New("invalid credentials")
	ErrTOTPAlreadyConfirmed    = errors.New("TOTP already confirmed")
	ErrInvalidTOTPCode         = errors.New("invalid TOTP code")
	ErrInvalidInviteRole       = errors.New("invalid invite role/site-grant combination")
	ErrOIDCNotConfigured       = errors.New("organization has no OIDC configuration")
	ErrInviteWrongIdentityType = errors.New("invite identity type does not match this login method")
)

type Store interface {
	IsBootstrapped(ctx context.Context) (bool, error)
	CompleteBootstrap(ctx context.Context, user User) error
	CreateUser(ctx context.Context, user User) error
	UserByEmail(ctx context.Context, organizationID, email string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)
	UserByOIDCIdentity(ctx context.Context, organizationID, issuer, subject string) (User, error)
	UpdateUser(ctx context.Context, user User) error
	SaveInvite(ctx context.Context, invite Invite, tokenHash string) error
	InviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error)
	InviteByEmail(ctx context.Context, organizationID, email string) (Invite, error)
	ConsumeInvite(ctx context.Context, tokenHash string, consumedByUserID string, at time.Time) error
	ConsumeInviteByID(ctx context.Context, inviteID string, consumedByUserID string, at time.Time) error
	SaveRecoveryCodes(ctx context.Context, userID string, codeHashes []string) error
	ConsumeRecoveryCode(ctx context.Context, userID, codeHash string) (bool, error)
	SaveOIDCConfiguration(ctx context.Context, config OIDCConfiguration) error
	OIDCConfigurationByOrganization(ctx context.Context, organizationID string) (OIDCConfiguration, error)
}

type Service struct {
	store             Store
	totpEncryptionKey string
	now               func() time.Time
	siteRoleGrantor   SiteRoleGrantor
}

type Option func(*Service)

func WithSiteRoleGrantor(grantor SiteRoleGrantor) Option {
	return func(service *Service) { service.siteRoleGrantor = grantor }
}

func NewService(store Store, totpEncryptionKey string, options ...Option) *Service {
	service := &Service{store: store, totpEncryptionKey: totpEncryptionKey, now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		option(service)
	}
	return service
}

// Bootstrap creates the hub's first platform-admin user. The bootstrap
// secret itself is compared by the caller (the HTTP handler, which holds
// the configured BAZUSOP_BOOTSTRAP_SECRET) before this method is ever
// invoked — Bootstrap is secret-agnostic so it stays independently
// testable.
func (service *Service) Bootstrap(ctx context.Context, organizationID, email, password string) (User, TOTPEnrollment, error) {
	bootstrapped, err := service.store.IsBootstrapped(ctx)
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	if bootstrapped {
		return User{}, TOTPEnrollment{}, ErrAlreadyBootstrapped
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	totpSecret, err := GenerateTOTPSecret()
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	encryptedSecret, err := EncryptTOTPSecret(totpSecret, service.totpEncryptionKey)
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	id, err := newID()
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	confirmedAt := service.now()
	user := User{
		ID: id, OrganizationID: organizationID, Email: email, Role: RolePlatformAdmin,
		PasswordHash: passwordHash, TOTPSecretEncrypted: encryptedSecret, TOTPConfirmedAt: &confirmedAt,
		CreatedAt: confirmedAt,
	}
	if err := service.store.CompleteBootstrap(ctx, user); err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	// The bootstrap admin is the one caller inherently trusted by holding
	// BAZUSOP_BOOTSTRAP_SECRET, so — unlike invited users, who must submit a
	// valid TOTP code via ConfirmTOTP before receiving recovery codes —
	// bootstrap issues the recovery codes immediately in the same response.
	codes, err := GenerateRecoveryCodes(10)
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	hashes := make([]string, len(codes))
	for index, code := range codes {
		hashes[index] = HashRecoveryCode(code)
	}
	if err := service.store.SaveRecoveryCodes(ctx, id, hashes); err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	return user, TOTPEnrollment{ProvisioningURI: TOTPProvisioningURI("bazUSOP", email, totpSecret), RecoveryCodes: codes}, nil
}

func (service *Service) CreateInvite(ctx context.Context, createdBy, organizationID, email string, role Role, siteRoleGrants []SiteRoleGrant, identityType IdentityType) (Invite, string, error) {
	if role == RolePlatformAdmin && len(siteRoleGrants) > 0 {
		return Invite{}, "", ErrInvalidInviteRole
	}
	if role != RolePlatformAdmin && len(siteRoleGrants) == 0 {
		return Invite{}, "", ErrInvalidInviteRole
	}
	if identityType == "" {
		identityType = IdentityTypeLocal
	}
	token, err := newID()
	if err != nil {
		return Invite{}, "", err
	}
	id, err := newID()
	if err != nil {
		return Invite{}, "", err
	}
	invite := Invite{
		ID: id, OrganizationID: organizationID, Email: email, Role: role, SiteRoleGrants: siteRoleGrants,
		IdentityType: identityType, CreatedBy: createdBy, ExpiresAt: service.now().Add(24 * time.Hour), CreatedAt: service.now(),
	}
	if err := service.store.SaveInvite(ctx, invite, hashToken(token)); err != nil {
		return Invite{}, "", err
	}
	return invite, token, nil
}

func (service *Service) ConsumeInvite(ctx context.Context, token, password string) (User, error) {
	tokenHash := hashToken(token)
	invite, err := service.store.InviteByTokenHash(ctx, tokenHash)
	if err != nil {
		return User{}, err
	}
	if invite.ConsumedAt != nil || invite.RevokedAt != nil || service.now().After(invite.ExpiresAt) {
		return User{}, ErrInviteExpired
	}
	if invite.IdentityType == IdentityTypeOIDC {
		return User{}, ErrInviteWrongIdentityType
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	id, err := newID()
	if err != nil {
		return User{}, err
	}
	user := User{
		ID: id, OrganizationID: invite.OrganizationID, Email: invite.Email, Role: invite.Role,
		PasswordHash: passwordHash, CreatedAt: service.now(),
	}
	if err := service.store.CreateUser(ctx, user); err != nil {
		return User{}, err
	}
	if len(invite.SiteRoleGrants) > 0 && service.siteRoleGrantor != nil {
		if err := service.siteRoleGrantor(ctx, user.ID, invite.SiteRoleGrants); err != nil {
			return User{}, err
		}
	}
	if err := service.store.ConsumeInvite(ctx, tokenHash, user.ID, service.now()); err != nil {
		return User{}, err
	}
	return user, nil
}

// ConfirmTOTP completes TOTP enrollment. codeFromSecret is a seam so tests
// can compute a valid code without VerifyTOTPCode's caller needing to
// decrypt the stored secret through package-external means; production
// callers (the HTTP handler) pass the user-submitted 6-digit code wrapped
// as `func([]byte) string { return submittedCode }`.
func (service *Service) ConfirmTOTP(ctx context.Context, userID string, codeFromSecret func(secret []byte) string, at time.Time) (TOTPEnrollment, error) {
	user, err := service.store.UserByID(ctx, userID)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	if user.TOTPConfirmedAt != nil {
		return TOTPEnrollment{}, ErrTOTPAlreadyConfirmed
	}
	var secret []byte
	if hasSecret(user) {
		secret, err = DecryptTOTPSecret(user.TOTPSecretEncrypted, service.totpEncryptionKey)
		if err != nil {
			return TOTPEnrollment{}, err
		}
	} else {
		newSecret, err := GenerateTOTPSecret()
		if err != nil {
			return TOTPEnrollment{}, err
		}
		secret = newSecret
		encrypted, err := EncryptTOTPSecret(secret, service.totpEncryptionKey)
		if err != nil {
			return TOTPEnrollment{}, err
		}
		user.TOTPSecretEncrypted = encrypted
	}
	submitted := codeFromSecret(secret)
	if !VerifyTOTPCode(secret, submitted, at) {
		return TOTPEnrollment{}, ErrInvalidTOTPCode
	}
	confirmedAt := at
	user.TOTPConfirmedAt = &confirmedAt
	if err := service.store.UpdateUser(ctx, user); err != nil {
		return TOTPEnrollment{}, err
	}
	codes, err := GenerateRecoveryCodes(10)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	hashes := make([]string, len(codes))
	for index, code := range codes {
		hashes[index] = HashRecoveryCode(code)
	}
	if err := service.store.SaveRecoveryCodes(ctx, userID, hashes); err != nil {
		return TOTPEnrollment{}, err
	}
	return TOTPEnrollment{RecoveryCodes: codes}, nil
}

func hasSecret(user User) bool { return len(user.TOTPSecretEncrypted) > 0 }

func (service *Service) VerifyCredentials(ctx context.Context, organizationID, email, password, totpCode string) (User, error) {
	user, err := service.store.UserByEmail(ctx, organizationID, email)
	if err != nil {
		return User{}, ErrInvalidCredentials
	}
	ok, err := VerifyPassword(user.PasswordHash, password)
	if err != nil || !ok {
		return User{}, ErrInvalidCredentials
	}
	secret, err := DecryptTOTPSecret(user.TOTPSecretEncrypted, service.totpEncryptionKey)
	if err != nil {
		return User{}, ErrInvalidCredentials
	}
	if !VerifyTOTPCode(secret, totpCode, service.now()) {
		return User{}, ErrInvalidTOTPCode
	}
	return user, nil
}

func (service *Service) VerifyCredentialsWithRecoveryCode(ctx context.Context, organizationID, email, password, recoveryCode string) (User, error) {
	user, err := service.store.UserByEmail(ctx, organizationID, email)
	if err != nil {
		return User{}, ErrInvalidCredentials
	}
	ok, err := VerifyPassword(user.PasswordHash, password)
	if err != nil || !ok {
		return User{}, ErrInvalidCredentials
	}
	consumed, err := service.store.ConsumeRecoveryCode(ctx, user.ID, HashRecoveryCode(recoveryCode))
	if err != nil || !consumed {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (service *Service) UserByID(ctx context.Context, id string) (User, error) {
	return service.store.UserByID(ctx, id)
}

// IsUserActive reports whether id exists and is not disabled. An unknown
// user is reported inactive (not an error) — this lets sessions.Service
// treat "user vanished" and "user disabled" identically: no valid session.
func (service *Service) IsUserActive(ctx context.Context, id string) (bool, error) {
	user, err := service.store.UserByID(ctx, id)
	if errors.Is(err, ErrInvalidCredentials) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return user.DisabledAt == nil, nil
}

// IsPlatformAdmin reports whether id holds the org-scoped platform-admin
// role. An unknown user is reported false (not an error), matching
// IsUserActive's convention — this is what authorization.PlatformAdminChecker
// is wired to in production.
func (service *Service) IsPlatformAdmin(ctx context.Context, id string) (bool, error) {
	user, err := service.store.UserByID(ctx, id)
	if errors.Is(err, ErrInvalidCredentials) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return user.Role == RolePlatformAdmin, nil
}

// LoginOrLinkOIDCUser resolves a verified OIDC identity (issuer+subject,
// already authenticated by internal/oidc) to a user. A returning user
// (already linked to this issuer+subject) logs in directly; a first-time
// subject requires a pending, unconsumed, unexpired oidc-type invite whose
// email matches — the same "no account without an invite" rule local users
// follow. Once linked, the account is permanently recognized by
// issuer+subject; a later email change at the IdP does not move the link
// (email is only ever consulted for this one-time invite lookup).
func (service *Service) LoginOrLinkOIDCUser(ctx context.Context, organizationID, issuer, subject, email string) (User, error) {
	existing, err := service.store.UserByOIDCIdentity(ctx, organizationID, issuer, subject)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		return User{}, err
	}
	invite, err := service.store.InviteByEmail(ctx, organizationID, email)
	if err != nil {
		return User{}, ErrInviteNotFound
	}
	if invite.ConsumedAt != nil || invite.RevokedAt != nil || service.now().After(invite.ExpiresAt) {
		return User{}, ErrInviteExpired
	}
	if invite.IdentityType != IdentityTypeOIDC {
		return User{}, ErrInviteWrongIdentityType
	}
	id, err := newID()
	if err != nil {
		return User{}, err
	}
	user := User{
		ID: id, OrganizationID: invite.OrganizationID, Email: invite.Email, Role: invite.Role,
		OIDCIssuer: issuer, OIDCSubject: subject, CreatedAt: service.now(),
	}
	if err := service.store.CreateUser(ctx, user); err != nil {
		return User{}, err
	}
	if len(invite.SiteRoleGrants) > 0 && service.siteRoleGrantor != nil {
		if err := service.siteRoleGrantor(ctx, user.ID, invite.SiteRoleGrants); err != nil {
			return User{}, err
		}
	}
	if err := service.store.ConsumeInviteByID(ctx, invite.ID, user.ID, service.now()); err != nil {
		return User{}, err
	}
	return user, nil
}

// SetOIDCConfiguration persists organization-wide OIDC settings. clientSecret
// is encrypted at rest using the same AES-256-GCM helper (and runtime key)
// TOTP secrets use — both are "encrypt this operator-supplied secret with a
// required runtime key" concerns, so spike 11.9 reuses EncryptTOTPSecret
// rather than introducing a second encryption key operators must configure.
func (service *Service) SetOIDCConfiguration(ctx context.Context, organizationID, discoveryURL, issuer, clientID, clientSecret, redirectURL, updatedBy string) (OIDCConfiguration, error) {
	encrypted, err := EncryptTOTPSecret([]byte(clientSecret), service.totpEncryptionKey)
	if err != nil {
		return OIDCConfiguration{}, err
	}
	config := OIDCConfiguration{
		OrganizationID: organizationID, DiscoveryURL: discoveryURL, Issuer: issuer, ClientID: clientID,
		ClientSecretEncrypted: encrypted, RedirectURL: redirectURL, UpdatedAt: service.now(), UpdatedBy: updatedBy,
	}
	if err := service.store.SaveOIDCConfiguration(ctx, config); err != nil {
		return OIDCConfiguration{}, err
	}
	return config, nil
}

// OIDCConfiguration returns the organization's OIDC settings WITHOUT the
// client secret — the shape safe to return from a read API.
func (service *Service) OIDCConfiguration(ctx context.Context, organizationID string) (OIDCConfiguration, error) {
	config, err := service.store.OIDCConfigurationByOrganization(ctx, organizationID)
	if err != nil {
		return OIDCConfiguration{}, err
	}
	config.ClientSecretEncrypted = nil
	return config, nil
}

// OIDCConfigurationWithSecret returns the organization's OIDC settings with
// the client secret decrypted — for internal/server's login/callback
// handlers only, which must present it to the IdP's token endpoint. Never
// exposed through an HTTP response.
func (service *Service) OIDCConfigurationWithSecret(ctx context.Context, organizationID string) (OIDCConfiguration, string, error) {
	config, err := service.store.OIDCConfigurationByOrganization(ctx, organizationID)
	if err != nil {
		return OIDCConfiguration{}, "", err
	}
	secret, err := DecryptTOTPSecret(config.ClientSecretEncrypted, service.totpEncryptionKey)
	if err != nil {
		return OIDCConfiguration{}, "", err
	}
	config.ClientSecretEncrypted = nil
	return config, string(secret), nil
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func hashToken(token string) string { return HashRecoveryCode(token) }

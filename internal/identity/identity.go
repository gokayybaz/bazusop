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
	CreatedAt           time.Time
	DisabledAt          *time.Time
}

type Invite struct {
	ID             string
	OrganizationID string
	Email          string
	Role           Role
	CreatedBy      string
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

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
	ErrAlreadyBootstrapped    = errors.New("hub is already bootstrapped")
	ErrInvalidBootstrapSecret = errors.New("invalid bootstrap secret")
	ErrInviteNotFound         = errors.New("invite not found")
	ErrInviteExpired          = errors.New("invite expired or already consumed")
	ErrInvalidCredentials     = errors.New("invalid credentials")
	ErrTOTPAlreadyConfirmed   = errors.New("TOTP already confirmed")
	ErrInvalidTOTPCode        = errors.New("invalid TOTP code")
)

type Store interface {
	IsBootstrapped(ctx context.Context) (bool, error)
	CompleteBootstrap(ctx context.Context, user User) error
	CreateUser(ctx context.Context, user User) error
	UserByEmail(ctx context.Context, organizationID, email string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)
	UpdateUser(ctx context.Context, user User) error
	SaveInvite(ctx context.Context, invite Invite, tokenHash string) error
	InviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error)
	ConsumeInvite(ctx context.Context, tokenHash string, consumedByUserID string, at time.Time) error
	SaveRecoveryCodes(ctx context.Context, userID string, codeHashes []string) error
	ConsumeRecoveryCode(ctx context.Context, userID, codeHash string) (bool, error)
}

type Service struct {
	store             Store
	totpEncryptionKey string
	now               func() time.Time
}

func NewService(store Store, totpEncryptionKey string) *Service {
	return &Service{store: store, totpEncryptionKey: totpEncryptionKey, now: func() time.Time { return time.Now().UTC() }}
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

func (service *Service) CreateInvite(ctx context.Context, createdBy, organizationID, email string) (Invite, string, error) {
	token, err := newID()
	if err != nil {
		return Invite{}, "", err
	}
	id, err := newID()
	if err != nil {
		return Invite{}, "", err
	}
	invite := Invite{
		ID: id, OrganizationID: organizationID, Email: email, Role: RolePlatformAdmin,
		CreatedBy: createdBy, ExpiresAt: service.now().Add(24 * time.Hour), CreatedAt: service.now(),
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

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func hashToken(token string) string { return HashRecoveryCode(token) }

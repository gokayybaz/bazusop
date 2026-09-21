package sessions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type Session struct {
	ID                string
	UserID            string
	OrganizationID    string
	CSRFToken         string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
}

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionRevoked  = errors.New("session revoked")
	ErrSessionExpired  = errors.New("session expired")
	ErrSessionIdle     = errors.New("session idle timeout")
	ErrUserInactive    = errors.New("user is disabled")
)

// UserActiveChecker reports whether userID may still hold a valid session —
// false once a user is disabled. internal/identity provides the production
// implementation (Service.IsUserActive); this package stays decoupled from
// identity's concrete types, matching the jobs.HostScopeChecker convention
// already used elsewhere in this codebase.
type UserActiveChecker func(context.Context, string) (bool, error)

type Store interface {
	CreateSession(ctx context.Context, session Session, tokenHash string) error
	SessionByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	Touch(ctx context.Context, sessionID string, lastSeenAt time.Time) error
	RevokeSession(ctx context.Context, sessionID string, at time.Time) error
	RevokeAllForUser(ctx context.Context, userID string, at time.Time) error
	RevokeAllForOrganization(ctx context.Context, organizationID string, at time.Time) error
}

type Service struct {
	store           Store
	userActive      UserActiveChecker
	now             func() time.Time
	idleTimeout     time.Duration
	absoluteTimeout time.Duration
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option { return func(service *Service) { service.now = clock } }
func WithIdleTimeout(duration time.Duration) Option {
	return func(service *Service) { service.idleTimeout = duration }
}
func WithAbsoluteTimeout(duration time.Duration) Option {
	return func(service *Service) { service.absoluteTimeout = duration }
}

// NewService requires a non-nil userActive so that disabling a user always
// has a real, already-tested path to invalidating their sessions, even
// before any HTTP endpoint sets DisabledAt.
func NewService(store Store, userActive UserActiveChecker, options ...Option) (*Service, error) {
	service := &Service{
		store: store, userActive: userActive, now: func() time.Time { return time.Now().UTC() },
		idleTimeout: 30 * time.Minute, absoluteTimeout: 8 * time.Hour,
	}
	for _, option := range options {
		option(service)
	}
	if service.userActive == nil {
		return nil, errors.New("session user-active checker is required")
	}
	return service, nil
}

func (service *Service) Create(ctx context.Context, userID, organizationID string) (Session, string, string, error) {
	id, err := newOpaqueID()
	if err != nil {
		return Session{}, "", "", err
	}
	token, err := newOpaqueID()
	if err != nil {
		return Session{}, "", "", err
	}
	csrfToken, err := newOpaqueID()
	if err != nil {
		return Session{}, "", "", err
	}
	now := service.now()
	session := Session{
		ID: id, UserID: userID, OrganizationID: organizationID, CSRFToken: csrfToken,
		CreatedAt: now, LastSeenAt: now, AbsoluteExpiresAt: now.Add(service.absoluteTimeout),
	}
	if err := service.store.CreateSession(ctx, session, hashToken(token)); err != nil {
		return Session{}, "", "", err
	}
	return session, token, csrfToken, nil
}

func (service *Service) Validate(ctx context.Context, token string) (Session, error) {
	session, err := service.store.SessionByTokenHash(ctx, hashToken(token))
	if err != nil {
		return Session{}, err
	}
	if session.RevokedAt != nil {
		return Session{}, ErrSessionRevoked
	}
	now := service.now()
	if now.After(session.AbsoluteExpiresAt) {
		return Session{}, ErrSessionExpired
	}
	if now.Sub(session.LastSeenAt) > service.idleTimeout {
		return Session{}, ErrSessionIdle
	}
	active, err := service.userActive(ctx, session.UserID)
	if err != nil {
		return Session{}, err
	}
	if !active {
		return Session{}, ErrUserInactive
	}
	if err := service.store.Touch(ctx, session.ID, now); err != nil {
		return Session{}, err
	}
	session.LastSeenAt = now
	return session, nil
}

// Rotate validates token, then issues a brand-new session for the same
// user/organization and revokes the old one. Spike 11.5 will call this
// whenever a user's role or site membership changes; this spike calls it
// from no HTTP handler yet, but it is fully exercised at the service layer.
func (service *Service) Rotate(ctx context.Context, token string) (Session, string, string, error) {
	session, err := service.Validate(ctx, token)
	if err != nil {
		return Session{}, "", "", err
	}
	newSession, newToken, newCSRFToken, err := service.Create(ctx, session.UserID, session.OrganizationID)
	if err != nil {
		return Session{}, "", "", err
	}
	if err := service.store.RevokeSession(ctx, session.ID, service.now()); err != nil {
		return Session{}, "", "", err
	}
	return newSession, newToken, newCSRFToken, nil
}

func (service *Service) Revoke(ctx context.Context, token string) error {
	session, err := service.store.SessionByTokenHash(ctx, hashToken(token))
	if err != nil {
		return err
	}
	return service.store.RevokeSession(ctx, session.ID, service.now())
}

func (service *Service) RevokeAllForUser(ctx context.Context, userID string) error {
	return service.store.RevokeAllForUser(ctx, userID, service.now())
}

func (service *Service) RevokeAllForOrganization(ctx context.Context, organizationID string) error {
	return service.store.RevokeAllForOrganization(ctx, organizationID, service.now())
}

func newOpaqueID() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

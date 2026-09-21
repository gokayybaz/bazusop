package sessions_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/sessions"
)

func alwaysActive(context.Context, string) (bool, error) { return true, nil }

func newTestService(t *testing.T, now func() time.Time, options ...sessions.Option) *sessions.Service {
	t.Helper()
	allOptions := append([]sessions.Option{sessions.WithClock(now)}, options...)
	service, err := sessions.NewService(sessions.NewMemoryStore(), alwaysActive, allOptions...)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestCreateProducesAValidatableSession(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	session, token, csrfToken, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if session.UserID != "user-1" || session.OrganizationID != "org_default" || token == "" || csrfToken == "" {
		t.Fatalf("unexpected session: %#v (token=%q csrf=%q)", session, token, csrfToken)
	}
	if session.CSRFToken != csrfToken {
		t.Fatalf("expected the returned CSRF token to match the session's stored CSRF token")
	}

	validated, err := service.Validate(t.Context(), token)
	if err != nil || validated.ID != session.ID {
		t.Fatalf("expected the fresh token to validate, got %#v %v", validated, err)
	}
}

func TestValidateRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	if _, err := service.Validate(t.Context(), "does-not-exist"); !errors.Is(err, sessions.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestValidateRejectsARevokedSession(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	_, token, _, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Revoke(t.Context(), token); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := service.Validate(t.Context(), token); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked, got %v", err)
	}
}

func TestValidateRejectsAfterAbsoluteTimeout(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }
	service := newTestService(t, clock, sessions.WithAbsoluteTimeout(time.Hour))
	_, token, _, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	current = current.Add(61 * time.Minute)
	if _, err := service.Validate(t.Context(), token); !errors.Is(err, sessions.ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got %v", err)
	}
}

func TestValidateRejectsAfterIdleTimeoutAndSlidesTheWindowForwardOtherwise(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }
	service := newTestService(t, clock, sessions.WithIdleTimeout(10*time.Minute), sessions.WithAbsoluteTimeout(8*time.Hour))
	_, token, _, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatal(err)
	}

	current = current.Add(5 * time.Minute)
	if _, err := service.Validate(t.Context(), token); err != nil {
		t.Fatalf("expected a well-within-idle-window validation to succeed: %v", err)
	}

	// The prior Validate call above slid LastSeenAt forward to `current`, so
	// another 5 minutes here is still within the 10-minute idle window
	// measured from the *slid* timestamp, proving the window moved.
	current = current.Add(5 * time.Minute)
	if _, err := service.Validate(t.Context(), token); err != nil {
		t.Fatalf("expected the idle window to have slid forward: %v", err)
	}

	current = current.Add(11 * time.Minute)
	if _, err := service.Validate(t.Context(), token); !errors.Is(err, sessions.ErrSessionIdle) {
		t.Fatalf("expected ErrSessionIdle, got %v", err)
	}
}

func TestValidateRejectsWhenTheUserIsNoLongerActive(t *testing.T) {
	t.Parallel()
	inactive := func(context.Context, string) (bool, error) { return false, nil }
	service, err := sessions.NewService(sessions.NewMemoryStore(), inactive)
	if err != nil {
		t.Fatal(err)
	}
	_, token, _, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Validate(t.Context(), token); !errors.Is(err, sessions.ErrUserInactive) {
		t.Fatalf("expected ErrUserInactive, got %v", err)
	}
}

func TestRotateIssuesAFreshSessionAndRevokesTheOld(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	session, token, csrfToken, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	newSession, newToken, newCSRFToken, err := service.Rotate(t.Context(), token)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newSession.ID == session.ID || newToken == token || newCSRFToken == csrfToken {
		t.Fatalf("expected rotation to produce a fresh session id, token, and CSRF token")
	}
	if newSession.UserID != session.UserID || newSession.OrganizationID != session.OrganizationID {
		t.Fatalf("expected rotation to preserve the same user and organization")
	}
	if _, err := service.Validate(t.Context(), token); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected the old token to be revoked after rotation, got %v", err)
	}
	if _, err := service.Validate(t.Context(), newToken); err != nil {
		t.Fatalf("expected the new token to validate: %v", err)
	}
}

func TestRevokeAllForUserInvalidatesEveryOneOfTheirSessionsButNotOthers(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	_, tokenA1, _, err := service.Create(t.Context(), "user-a", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	_, tokenA2, _, err := service.Create(t.Context(), "user-a", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	_, tokenB, _, err := service.Create(t.Context(), "user-b", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeAllForUser(t.Context(), "user-a"); err != nil {
		t.Fatalf("revoke all for user: %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenA1); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected user-a's first session revoked, got %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenA2); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected user-a's second session revoked, got %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenB); err != nil {
		t.Fatalf("expected user-b's session to remain valid, got %v", err)
	}
}

func TestRevokeAllForOrganizationInvalidatesEverySessionInThatOrganization(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	_, tokenA, _, err := service.Create(t.Context(), "user-a", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	_, tokenB, _, err := service.Create(t.Context(), "user-b", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	_, tokenOther, _, err := service.Create(t.Context(), "user-c", "org_other")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeAllForOrganization(t.Context(), "org_default"); err != nil {
		t.Fatalf("revoke all for organization: %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenA); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected org_default session A revoked, got %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenB); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected org_default session B revoked, got %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenOther); err != nil {
		t.Fatalf("expected the other organization's session to remain valid, got %v", err)
	}
}

func TestNewServiceRequiresAUserActiveChecker(t *testing.T) {
	t.Parallel()
	if _, err := sessions.NewService(sessions.NewMemoryStore(), nil); err == nil {
		t.Fatal("expected NewService to reject a nil UserActiveChecker")
	}
}

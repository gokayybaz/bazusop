package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/sessions"
)

func (store *Store) CreateSession(ctx context.Context, session sessions.Session, tokenHash string) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO sessions (id, token_hash, user_id, organization_id, csrf_token, created_at, last_seen_at, absolute_expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		session.ID, tokenHash, session.UserID, session.OrganizationID, session.CSRFToken,
		session.CreatedAt, session.LastSeenAt, session.AbsoluteExpiresAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (store *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (sessions.Session, error) {
	var session sessions.Session
	err := store.pool.QueryRow(ctx, `
		SELECT id, user_id, organization_id, csrf_token, created_at, last_seen_at, absolute_expires_at, revoked_at
		FROM sessions WHERE token_hash=$1`, tokenHash).Scan(
		&session.ID, &session.UserID, &session.OrganizationID, &session.CSRFToken,
		&session.CreatedAt, &session.LastSeenAt, &session.AbsoluteExpiresAt, &session.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessions.Session{}, sessions.ErrSessionNotFound
	}
	if err != nil {
		return sessions.Session{}, fmt.Errorf("query session: %w", err)
	}
	return session, nil
}

func (store *Store) Touch(ctx context.Context, sessionID string, lastSeenAt time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE sessions SET last_seen_at=$2 WHERE id=$1`, sessionID, lastSeenAt)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (store *Store) RevokeSession(ctx context.Context, sessionID string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL`, sessionID, at)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (store *Store) RevokeAllForUser(ctx context.Context, userID string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, userID, at)
	if err != nil {
		return fmt.Errorf("revoke sessions for user: %w", err)
	}
	return nil
}

func (store *Store) RevokeAllForOrganization(ctx context.Context, organizationID string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=$2 WHERE organization_id=$1 AND revoked_at IS NULL`, organizationID, at)
	if err != nil {
		return fmt.Errorf("revoke sessions for organization: %w", err)
	}
	return nil
}

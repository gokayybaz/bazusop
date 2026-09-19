package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func (store *Store) LoadOrCreateEnrollmentAuthority(ctx context.Context, candidate enrollment.AuthorityState) (enrollment.AuthorityState, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return enrollment.AuthorityState{}, fmt.Errorf("begin enrollment authority transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `
		INSERT INTO enrollment_authority (singleton, certificate_pem, private_key_pem)
		VALUES (TRUE, $1, $2)
		ON CONFLICT (singleton) DO NOTHING`, candidate.CertificatePEM, candidate.PrivateKeyPEM); err != nil {
		return enrollment.AuthorityState{}, fmt.Errorf("create enrollment authority: %w", err)
	}
	var state enrollment.AuthorityState
	if err := transaction.QueryRow(ctx, `
		SELECT certificate_pem, private_key_pem
		FROM enrollment_authority
		WHERE singleton = TRUE`).Scan(&state.CertificatePEM, &state.PrivateKeyPEM); err != nil {
		return enrollment.AuthorityState{}, fmt.Errorf("load enrollment authority: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return enrollment.AuthorityState{}, fmt.Errorf("commit enrollment authority: %w", err)
	}
	return state, nil
}

type enrollmentDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (store *Store) RegisterEnrollmentToken(ctx context.Context, tokenHash [32]byte, scope tenancy.Scope) error {
	return registerEnrollmentToken(ctx, store.pool, tokenHash, scope)
}

func registerEnrollmentToken(ctx context.Context, database enrollmentDatabase, tokenHash [32]byte, scope tenancy.Scope) error {
	transaction, err := database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin enrollment token registration: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var registeredHash []byte
	err = transaction.QueryRow(ctx, `
		INSERT INTO enrollment_tokens (token_hash, organization_id, site_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (token_hash) DO NOTHING
		RETURNING token_hash`, tokenHash[:], scope.OrganizationID, scope.SiteID).Scan(&registeredHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("register enrollment token: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE enrollment_tokens
		SET revoked_at = now()
		WHERE token_hash <> $1 AND organization_id = $2 AND site_id = $3
			AND consumed_at IS NULL AND revoked_at IS NULL`, tokenHash[:], scope.OrganizationID, scope.SiteID); err != nil {
		return fmt.Errorf("revoke previous enrollment tokens: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit enrollment token registration: %w", err)
	}
	return nil
}

func (store *Store) ConsumeEnrollmentToken(ctx context.Context, tokenHash [32]byte, agentID string) (tenancy.Agent, error) {
	return consumeEnrollmentToken(ctx, store.pool, tokenHash, agentID)
}

func consumeEnrollmentToken(ctx context.Context, database enrollmentDatabase, tokenHash [32]byte, agentID string) (tenancy.Agent, error) {
	transaction, err := database.Begin(ctx)
	if err != nil {
		return tenancy.Agent{}, fmt.Errorf("begin enrollment token consumption: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	agent := tenancy.Agent{ID: agentID}
	var consumed, revoked bool
	if err := transaction.QueryRow(ctx, `
		SELECT organization_id, site_id, consumed_at IS NOT NULL, revoked_at IS NOT NULL
		FROM enrollment_tokens
		WHERE token_hash = $1
		FOR UPDATE`, tokenHash[:]).Scan(&agent.OrganizationID, &agent.SiteID, &consumed, &revoked); errors.Is(err, pgx.ErrNoRows) {
		return tenancy.Agent{}, enrollment.ErrInvalidToken
	} else if err != nil {
		return tenancy.Agent{}, fmt.Errorf("check enrollment token: %w", err)
	}
	if revoked {
		return tenancy.Agent{}, enrollment.ErrInvalidToken
	}
	if consumed {
		return tenancy.Agent{}, enrollment.ErrTokenConsumed
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO agents (id, organization_id, site_id)
		VALUES ($1, $2, $3)`, agent.ID, agent.OrganizationID, agent.SiteID); err != nil {
		return tenancy.Agent{}, fmt.Errorf("insert enrolled agent: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE enrollment_tokens
		SET consumed_at = now(), consumed_by_agent_id = $2
		WHERE token_hash = $1`, tokenHash[:], agent.ID); err != nil {
		return tenancy.Agent{}, fmt.Errorf("consume enrollment token: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return tenancy.Agent{}, fmt.Errorf("commit enrollment token consumption: %w", err)
	}
	return agent, nil
}

func (store *Store) ResolveAgent(ctx context.Context, agentID string) (tenancy.Agent, error) {
	return resolveAgent(ctx, store.pool, agentID)
}

func resolveAgent(ctx context.Context, database enrollmentDatabase, agentID string) (tenancy.Agent, error) {
	var agent tenancy.Agent
	if err := database.QueryRow(ctx, `
		SELECT id, organization_id, site_id FROM agents WHERE id = $1`, agentID).Scan(&agent.ID, &agent.OrganizationID, &agent.SiteID); errors.Is(err, pgx.ErrNoRows) {
		return tenancy.Agent{}, tenancy.ErrNotFound
	} else if err != nil {
		return tenancy.Agent{}, fmt.Errorf("resolve enrolled agent: %w", err)
	}
	return agent, nil
}

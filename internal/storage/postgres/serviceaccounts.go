package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
)

func (store *Store) CreateAccount(ctx context.Context, account serviceaccounts.ServiceAccount) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO service_accounts (id, organization_id, site_id, name, role, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		account.ID, account.OrganizationID, account.SiteID, account.Name, string(account.Role), account.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert service account: %w", err)
	}
	return nil
}

func (store *Store) AccountByID(ctx context.Context, id string) (serviceaccounts.ServiceAccount, error) {
	var account serviceaccounts.ServiceAccount
	var role string
	err := store.pool.QueryRow(ctx, `
		SELECT id, organization_id, site_id, name, role, created_at, disabled_at
		FROM service_accounts WHERE id=$1`, id).Scan(
		&account.ID, &account.OrganizationID, &account.SiteID, &account.Name, &role, &account.CreatedAt, &account.DisabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return serviceaccounts.ServiceAccount{}, serviceaccounts.ErrAccountNotFound
	}
	if err != nil {
		return serviceaccounts.ServiceAccount{}, fmt.Errorf("query service account: %w", err)
	}
	account.Role = authorization.SiteRole(role)
	return account, nil
}

func (store *Store) DisableAccount(ctx context.Context, id string, at time.Time) error {
	result, err := store.pool.Exec(ctx, `UPDATE service_accounts SET disabled_at=$2 WHERE id=$1 AND disabled_at IS NULL`, id, at)
	if err != nil {
		return fmt.Errorf("disable service account: %w", err)
	}
	if result.RowsAffected() != 1 {
		return serviceaccounts.ErrAccountNotFound
	}
	return nil
}

func (store *Store) AccountsForSite(ctx context.Context, siteID string) ([]serviceaccounts.ServiceAccount, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, organization_id, site_id, name, role, created_at, disabled_at
		FROM service_accounts WHERE site_id=$1 ORDER BY created_at`, siteID)
	if err != nil {
		return nil, fmt.Errorf("query service accounts: %w", err)
	}
	defer rows.Close()
	var accounts []serviceaccounts.ServiceAccount
	for rows.Next() {
		var account serviceaccounts.ServiceAccount
		var role string
		if err := rows.Scan(&account.ID, &account.OrganizationID, &account.SiteID, &account.Name, &role, &account.CreatedAt, &account.DisabledAt); err != nil {
			return nil, fmt.Errorf("scan service account: %w", err)
		}
		account.Role = authorization.SiteRole(role)
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service accounts: %w", err)
	}
	return accounts, nil
}

func (store *Store) CreateToken(ctx context.Context, token serviceaccounts.Token, secretHash string) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO service_account_tokens (id, service_account_id, secret_hash, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5)`,
		token.ID, token.ServiceAccountID, secretHash, token.CreatedAt, token.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert service account token: %w", err)
	}
	return nil
}

func (store *Store) TokenByID(ctx context.Context, id string) (serviceaccounts.Token, string, error) {
	var token serviceaccounts.Token
	var secretHash string
	var lastUsedIP *string
	err := store.pool.QueryRow(ctx, `
		SELECT id, service_account_id, secret_hash, created_at, expires_at, last_used_at, last_used_ip, revoked_at
		FROM service_account_tokens WHERE id=$1`, id).Scan(
		&token.ID, &token.ServiceAccountID, &secretHash, &token.CreatedAt, &token.ExpiresAt,
		&token.LastUsedAt, &lastUsedIP, &token.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return serviceaccounts.Token{}, "", serviceaccounts.ErrTokenNotFound
	}
	if err != nil {
		return serviceaccounts.Token{}, "", fmt.Errorf("query service account token: %w", err)
	}
	if lastUsedIP != nil {
		token.LastUsedIP = *lastUsedIP
	}
	return token, secretHash, nil
}

func (store *Store) RevokeToken(ctx context.Context, id string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE service_account_tokens SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL`, id, at)
	if err != nil {
		return fmt.Errorf("revoke service account token: %w", err)
	}
	return nil
}

func (store *Store) RevokeActiveTokensForAccount(ctx context.Context, accountID string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE service_account_tokens SET revoked_at=$2 WHERE service_account_id=$1 AND revoked_at IS NULL`, accountID, at)
	if err != nil {
		return fmt.Errorf("revoke active service account tokens: %w", err)
	}
	return nil
}

func (store *Store) TouchToken(ctx context.Context, tokenID string, at time.Time, sourceIP string) error {
	_, err := store.pool.Exec(ctx, `UPDATE service_account_tokens SET last_used_at=$2, last_used_ip=$3 WHERE id=$1`, tokenID, at, sourceIP)
	if err != nil {
		return fmt.Errorf("touch service account token: %w", err)
	}
	return nil
}

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/gokayybaz/bazusop/internal/identity"
)

func (store *Store) IsBootstrapped(ctx context.Context) (bool, error) {
	var exists bool
	if err := store.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform_bootstrap WHERE singleton = TRUE)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check bootstrap state: %w", err)
	}
	return exists, nil
}

func (store *Store) CompleteBootstrap(ctx context.Context, user identity.User) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var claimed string
	err = tx.QueryRow(ctx, `
		INSERT INTO platform_bootstrap (singleton, completed_at, user_id)
		VALUES (TRUE, now(), $1)
		ON CONFLICT (singleton) DO NOTHING
		RETURNING user_id`, user.ID).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrAlreadyBootstrapped
	}
	if err != nil {
		return fmt.Errorf("claim bootstrap: %w", err)
	}
	if err := insertUser(ctx, tx, user); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit bootstrap: %w", err)
	}
	return nil
}

func (store *Store) CreateUser(ctx context.Context, user identity.User) error {
	return insertUser(ctx, store.pool, user)
}

type identityDatabase interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertUser(ctx context.Context, database identityDatabase, user identity.User) error {
	var passwordHash *string
	if user.PasswordHash != "" {
		passwordHash = &user.PasswordHash
	}
	_, err := database.Exec(ctx, `
		INSERT INTO users (id, organization_id, email, role, password_hash, totp_secret_encrypted, totp_confirmed_at, created_at, disabled_at, oidc_issuer, oidc_subject)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		user.ID, user.OrganizationID, user.Email, string(user.Role), passwordHash,
		user.TOTPSecretEncrypted, user.TOTPConfirmedAt, user.CreatedAt, user.DisabledAt, user.OIDCIssuer, user.OIDCSubject)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (store *Store) UserByEmail(ctx context.Context, organizationID, email string) (identity.User, error) {
	return scanUser(store.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, role, password_hash, totp_secret_encrypted, totp_confirmed_at, created_at, disabled_at, oidc_issuer, oidc_subject
		FROM users WHERE organization_id=$1 AND email=$2`, organizationID, email))
}

func (store *Store) UserByID(ctx context.Context, id string) (identity.User, error) {
	return scanUser(store.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, role, password_hash, totp_secret_encrypted, totp_confirmed_at, created_at, disabled_at, oidc_issuer, oidc_subject
		FROM users WHERE id=$1`, id))
}

func (store *Store) UserByOIDCIdentity(ctx context.Context, organizationID, issuer, subject string) (identity.User, error) {
	return scanUser(store.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, role, password_hash, totp_secret_encrypted, totp_confirmed_at, created_at, disabled_at, oidc_issuer, oidc_subject
		FROM users WHERE organization_id=$1 AND oidc_issuer=$2 AND oidc_subject=$3 AND oidc_subject <> ''`, organizationID, issuer, subject))
}

func scanUser(row pgx.Row) (identity.User, error) {
	var user identity.User
	var role string
	var passwordHash *string
	if err := row.Scan(&user.ID, &user.OrganizationID, &user.Email, &role, &passwordHash,
		&user.TOTPSecretEncrypted, &user.TOTPConfirmedAt, &user.CreatedAt, &user.DisabledAt,
		&user.OIDCIssuer, &user.OIDCSubject); errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, identity.ErrInvalidCredentials
	} else if err != nil {
		return identity.User{}, fmt.Errorf("scan user: %w", err)
	}
	user.Role = identity.Role(role)
	if passwordHash != nil {
		user.PasswordHash = *passwordHash
	}
	return user, nil
}

func (store *Store) UpdateUser(ctx context.Context, user identity.User) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE users SET totp_secret_encrypted=$2, totp_confirmed_at=$3, disabled_at=$4
		WHERE id=$1`, user.ID, user.TOTPSecretEncrypted, user.TOTPConfirmedAt, user.DisabledAt)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func (store *Store) SaveInvite(ctx context.Context, invite identity.Invite, tokenHash string) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin save invite: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO invites (id, token_hash, organization_id, email, role, identity_type, created_by, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		invite.ID, tokenHash, invite.OrganizationID, invite.Email, string(invite.Role), string(invite.IdentityType), invite.CreatedBy, invite.ExpiresAt, invite.CreatedAt)
	if err != nil {
		return fmt.Errorf("save invite: %w", err)
	}
	for _, grant := range invite.SiteRoleGrants {
		if _, err := tx.Exec(ctx, `INSERT INTO invite_site_roles (invite_id, site_id, role) VALUES ($1,$2,$3)`, invite.ID, grant.SiteID, grant.Role); err != nil {
			return fmt.Errorf("save invite site role: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit save invite: %w", err)
	}
	return nil
}

func (store *Store) InviteByTokenHash(ctx context.Context, tokenHash string) (identity.Invite, error) {
	var invite identity.Invite
	var role, identityType string
	err := store.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, role, identity_type, created_by, expires_at, consumed_at, revoked_at, created_at
		FROM invites WHERE token_hash=$1`, tokenHash).Scan(
		&invite.ID, &invite.OrganizationID, &invite.Email, &role, &identityType, &invite.CreatedBy,
		&invite.ExpiresAt, &invite.ConsumedAt, &invite.RevokedAt, &invite.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Invite{}, identity.ErrInviteNotFound
	}
	if err != nil {
		return identity.Invite{}, fmt.Errorf("query invite: %w", err)
	}
	invite.Role, invite.IdentityType = identity.Role(role), identity.IdentityType(identityType)

	rows, err := store.pool.Query(ctx, `SELECT site_id, role FROM invite_site_roles WHERE invite_id=$1`, invite.ID)
	if err != nil {
		return identity.Invite{}, fmt.Errorf("query invite site roles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var grant identity.SiteRoleGrant
		if err := rows.Scan(&grant.SiteID, &grant.Role); err != nil {
			return identity.Invite{}, fmt.Errorf("scan invite site role: %w", err)
		}
		invite.SiteRoleGrants = append(invite.SiteRoleGrants, grant)
	}
	if err := rows.Err(); err != nil {
		return identity.Invite{}, fmt.Errorf("iterate invite site roles: %w", err)
	}
	return invite, nil
}

// InviteByEmail returns the most recently created invite for email,
// regardless of status — spike 11.9's OIDC first-login path applies the
// same expiry/consumed/revoked/identity-type checks ConsumeInvite already
// applies for local invites, at the service layer, not here.
func (store *Store) InviteByEmail(ctx context.Context, organizationID, email string) (identity.Invite, error) {
	var invite identity.Invite
	var role, identityType string
	err := store.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, role, identity_type, created_by, expires_at, consumed_at, revoked_at, created_at
		FROM invites WHERE organization_id=$1 AND email=$2
		ORDER BY created_at DESC LIMIT 1`, organizationID, email).Scan(
		&invite.ID, &invite.OrganizationID, &invite.Email, &role, &identityType, &invite.CreatedBy,
		&invite.ExpiresAt, &invite.ConsumedAt, &invite.RevokedAt, &invite.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Invite{}, identity.ErrInviteNotFound
	}
	if err != nil {
		return identity.Invite{}, fmt.Errorf("query invite by email: %w", err)
	}
	invite.Role, invite.IdentityType = identity.Role(role), identity.IdentityType(identityType)

	rows, err := store.pool.Query(ctx, `SELECT site_id, role FROM invite_site_roles WHERE invite_id=$1`, invite.ID)
	if err != nil {
		return identity.Invite{}, fmt.Errorf("query invite site roles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var grant identity.SiteRoleGrant
		if err := rows.Scan(&grant.SiteID, &grant.Role); err != nil {
			return identity.Invite{}, fmt.Errorf("scan invite site role: %w", err)
		}
		invite.SiteRoleGrants = append(invite.SiteRoleGrants, grant)
	}
	if err := rows.Err(); err != nil {
		return identity.Invite{}, fmt.Errorf("iterate invite site roles: %w", err)
	}
	return invite, nil
}

func (store *Store) ConsumeInvite(ctx context.Context, tokenHash string, consumedByUserID string, at time.Time) error {
	result, err := store.pool.Exec(ctx, `
		UPDATE invites SET consumed_at=$2, consumed_by_user_id=$3
		WHERE token_hash=$1 AND consumed_at IS NULL`, tokenHash, at, consumedByUserID)
	if err != nil {
		return fmt.Errorf("consume invite: %w", err)
	}
	if result.RowsAffected() != 1 {
		return identity.ErrInviteExpired
	}
	return nil
}

func (store *Store) ConsumeInviteByID(ctx context.Context, inviteID string, consumedByUserID string, at time.Time) error {
	result, err := store.pool.Exec(ctx, `
		UPDATE invites SET consumed_at=$2, consumed_by_user_id=$3
		WHERE id=$1 AND consumed_at IS NULL`, inviteID, at, consumedByUserID)
	if err != nil {
		return fmt.Errorf("consume invite by id: %w", err)
	}
	if result.RowsAffected() != 1 {
		return identity.ErrInviteExpired
	}
	return nil
}

func (store *Store) SaveRecoveryCodes(ctx context.Context, userID string, codeHashes []string) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin save recovery codes: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, hash := range codeHashes {
		id, err := newRecoveryCodeID()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO recovery_codes (id, user_id, code_hash) VALUES ($1,$2,$3)`, id, userID, hash); err != nil {
			return fmt.Errorf("insert recovery code: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit recovery codes: %w", err)
	}
	return nil
}

func (store *Store) ConsumeRecoveryCode(ctx context.Context, userID, codeHash string) (bool, error) {
	result, err := store.pool.Exec(ctx, `
		UPDATE recovery_codes SET consumed_at = now()
		WHERE user_id=$1 AND code_hash=$2 AND consumed_at IS NULL`, userID, codeHash)
	if err != nil {
		return false, fmt.Errorf("consume recovery code: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

func newRecoveryCodeID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate recovery code id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func (store *Store) SaveOIDCConfiguration(ctx context.Context, config identity.OIDCConfiguration) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO oidc_configurations (organization_id, discovery_url, issuer, client_id, client_secret_encrypted, redirect_url, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (organization_id) DO UPDATE SET
			discovery_url=EXCLUDED.discovery_url, issuer=EXCLUDED.issuer, client_id=EXCLUDED.client_id,
			client_secret_encrypted=EXCLUDED.client_secret_encrypted, redirect_url=EXCLUDED.redirect_url,
			updated_at=EXCLUDED.updated_at, updated_by=EXCLUDED.updated_by`,
		config.OrganizationID, config.DiscoveryURL, config.Issuer, config.ClientID, config.ClientSecretEncrypted,
		config.RedirectURL, config.UpdatedAt, config.UpdatedBy)
	if err != nil {
		return fmt.Errorf("save oidc configuration: %w", err)
	}
	return nil
}

func (store *Store) OIDCConfigurationByOrganization(ctx context.Context, organizationID string) (identity.OIDCConfiguration, error) {
	var config identity.OIDCConfiguration
	err := store.pool.QueryRow(ctx, `
		SELECT organization_id, discovery_url, issuer, client_id, client_secret_encrypted, redirect_url, updated_at, updated_by
		FROM oidc_configurations WHERE organization_id=$1`, organizationID).Scan(
		&config.OrganizationID, &config.DiscoveryURL, &config.Issuer, &config.ClientID, &config.ClientSecretEncrypted,
		&config.RedirectURL, &config.UpdatedAt, &config.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.OIDCConfiguration{}, identity.ErrOIDCNotConfigured
	}
	if err != nil {
		return identity.OIDCConfiguration{}, fmt.Errorf("query oidc configuration: %w", err)
	}
	return config, nil
}

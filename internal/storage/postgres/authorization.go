package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/authorization"
)

func (store *Store) AssignRole(ctx context.Context, membership authorization.SiteMembership) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO site_memberships (id, user_id, organization_id, site_id, role, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (user_id, site_id) DO UPDATE SET role = EXCLUDED.role`,
		membership.ID, membership.UserID, membership.OrganizationID, membership.SiteID, string(membership.Role), membership.CreatedAt)
	if err != nil {
		return fmt.Errorf("assign site role: %w", err)
	}
	return nil
}

func (store *Store) RevokeRole(ctx context.Context, userID, siteID string) error {
	_, err := store.pool.Exec(ctx, `DELETE FROM site_memberships WHERE user_id=$1 AND site_id=$2`, userID, siteID)
	if err != nil {
		return fmt.Errorf("revoke site role: %w", err)
	}
	return nil
}

func (store *Store) RoleForUserAtSite(ctx context.Context, userID, siteID string) (authorization.SiteRole, error) {
	var role string
	err := store.pool.QueryRow(ctx, `SELECT role FROM site_memberships WHERE user_id=$1 AND site_id=$2`, userID, siteID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", authorization.ErrMembershipNotFound
	}
	if err != nil {
		return "", fmt.Errorf("query site role: %w", err)
	}
	return authorization.SiteRole(role), nil
}

func (store *Store) MembershipsForUser(ctx context.Context, userID string) ([]authorization.SiteMembership, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, user_id, organization_id, site_id, role, created_at
		FROM site_memberships WHERE user_id=$1`, userID)
	if err != nil {
		return nil, fmt.Errorf("query memberships: %w", err)
	}
	defer rows.Close()
	var memberships []authorization.SiteMembership
	for rows.Next() {
		var membership authorization.SiteMembership
		var role string
		if err := rows.Scan(&membership.ID, &membership.UserID, &membership.OrganizationID, &membership.SiteID, &role, &membership.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		membership.Role = authorization.SiteRole(role)
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memberships: %w", err)
	}
	return memberships, nil
}

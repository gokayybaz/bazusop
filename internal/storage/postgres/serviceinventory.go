package postgres

import (
	"context"
	"fmt"

	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func (store *Store) ReplaceServices(ctx context.Context, scope tenancy.Scope, agentID string, services []serviceinventory.Service) error {
	if err := (tenancy.Agent{ID: agentID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}).Validate(); err != nil {
		return err
	}
	for _, service := range services {
		if service.AgentID != agentID || service.OrganizationID != scope.OrganizationID || service.SiteID != scope.SiteID {
			return tenancy.ErrInvalidScope
		}
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin service snapshot replacement: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var hostExists int
	if err := transaction.QueryRow(ctx, "SELECT 1 FROM hosts WHERE organization_id = $1 AND site_id = $2 AND agent_id = $3 FOR UPDATE", scope.OrganizationID, scope.SiteID, agentID).Scan(&hostExists); err != nil {
		return fmt.Errorf("lock service snapshot host: %w", err)
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM services WHERE organization_id = $1 AND site_id = $2 AND agent_id = $3", scope.OrganizationID, scope.SiteID, agentID); err != nil {
		return fmt.Errorf("clear previous service snapshot: %w", err)
	}
	for _, service := range services {
		if _, err := transaction.Exec(ctx, `
			INSERT INTO services (organization_id, site_id, agent_id, name, display_name, state, startup_type, observed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			service.OrganizationID, service.SiteID, service.AgentID, service.Name, service.DisplayName, service.State, service.StartupType, service.ObservedAt,
		); err != nil {
			return fmt.Errorf("insert service snapshot: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit service snapshot replacement: %w", err)
	}
	return nil
}

func (store *Store) ListServices(ctx context.Context, scope tenancy.Scope, agentID string, filter serviceinventory.Filter) ([]serviceinventory.Service, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT organization_id, site_id, agent_id, name, display_name, state, startup_type, observed_at
		FROM services
		WHERE organization_id = $1 AND site_id = $2 AND agent_id = $3
			AND ($4 = '' OR state = $4)
			AND ($5 = '' OR name ILIKE '%' || $5 || '%' OR display_name ILIKE '%' || $5 || '%')
		ORDER BY name`, scope.OrganizationID, scope.SiteID, agentID, filter.State, filter.Query)
	if err != nil {
		return nil, fmt.Errorf("query service inventory: %w", err)
	}
	defer rows.Close()
	services := make([]serviceinventory.Service, 0)
	for rows.Next() {
		var service serviceinventory.Service
		if err := rows.Scan(&service.OrganizationID, &service.SiteID, &service.AgentID, &service.Name, &service.DisplayName, &service.State, &service.StartupType, &service.ObservedAt); err != nil {
			return nil, fmt.Errorf("scan service inventory: %w", err)
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service inventory: %w", err)
	}
	return services, nil
}

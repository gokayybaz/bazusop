package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func (store *Store) Upsert(ctx context.Context, host inventory.Host) error {
	return upsertHost(ctx, store.pool, host)
}

type inventoryDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
}

func upsertHost(ctx context.Context, database inventoryDatabase, host inventory.Host) error {
	if err := host.ValidateStored(); err != nil {
		return err
	}
	transaction, err := database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin host inventory transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	// Serialize reports with a privileged move, which must update agents and
	// current hosts together in one transaction, leaving historical rows intact.
	var agentID string
	if err := transaction.QueryRow(ctx, `
		SELECT id FROM agents
		WHERE id = $1 AND organization_id = $2 AND site_id = $3
		FOR UPDATE`, host.AgentID, host.OrganizationID, host.SiteID).Scan(&agentID); errors.Is(err, pgx.ErrNoRows) {
		return tenancy.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock host agent assignment: %w", err)
	}
	result, err := transaction.Exec(ctx, `
		INSERT INTO hosts (
			agent_id, organization_id, site_id, hostname, os_family, os_name, os_version, architecture,
			kernel_version, cpu_cores, memory_bytes, ip_addresses, agent_version,
			first_seen_at, last_seen_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (agent_id) DO UPDATE SET
			organization_id = EXCLUDED.organization_id,
			site_id = EXCLUDED.site_id,
			hostname = EXCLUDED.hostname,
			os_family = EXCLUDED.os_family,
			os_name = EXCLUDED.os_name,
			os_version = EXCLUDED.os_version,
			architecture = EXCLUDED.architecture,
			kernel_version = EXCLUDED.kernel_version,
			cpu_cores = EXCLUDED.cpu_cores,
			memory_bytes = EXCLUDED.memory_bytes,
			ip_addresses = EXCLUDED.ip_addresses,
			agent_version = EXCLUDED.agent_version,
			last_seen_at = EXCLUDED.last_seen_at
		WHERE hosts.agent_id = $1 AND hosts.organization_id = $2 AND hosts.site_id = $3`,
		host.AgentID,
		host.OrganizationID,
		host.SiteID,
		host.Hostname,
		host.OSFamily,
		host.OSName,
		host.OSVersion,
		host.Architecture,
		host.KernelVersion,
		host.CPUCores,
		int64(host.MemoryBytes),
		host.IPAddresses,
		host.AgentVersion,
		host.FirstSeenAt,
		host.LastSeenAt,
	)
	if err != nil {
		return fmt.Errorf("upsert host inventory: %w", err)
	}
	if result.RowsAffected() != 1 {
		return tenancy.ErrNotFound
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit host inventory: %w", err)
	}
	return nil
}

func (store *Store) List(ctx context.Context, scope tenancy.Scope) ([]inventory.Host, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT agent_id, organization_id, site_id, hostname, os_family, os_name, os_version, architecture,
			kernel_version, cpu_cores, memory_bytes, ip_addresses, agent_version,
			first_seen_at, last_seen_at
		FROM hosts
		WHERE organization_id = $1 AND site_id = $2
		ORDER BY hostname`, scope.OrganizationID, scope.SiteID)
	if err != nil {
		return nil, fmt.Errorf("query host inventory: %w", err)
	}
	defer rows.Close()

	hosts := make([]inventory.Host, 0)
	for rows.Next() {
		var host inventory.Host
		var memoryBytes int64
		if err := rows.Scan(
			&host.AgentID,
			&host.OrganizationID,
			&host.SiteID,
			&host.Hostname,
			&host.OSFamily,
			&host.OSName,
			&host.OSVersion,
			&host.Architecture,
			&host.KernelVersion,
			&host.CPUCores,
			&memoryBytes,
			&host.IPAddresses,
			&host.AgentVersion,
			&host.FirstSeenAt,
			&host.LastSeenAt,
		); err != nil {
			return nil, fmt.Errorf("scan host inventory: %w", err)
		}
		host.MemoryBytes = uint64(memoryBytes)
		hosts = append(hosts, host)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate host inventory: %w", err)
	}
	return hosts, nil
}

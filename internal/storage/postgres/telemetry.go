package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func (store *Store) Append(ctx context.Context, sample telemetry.Sample) error {
	if err := (tenancy.Agent{ID: sample.AgentID, OrganizationID: sample.OrganizationID, SiteID: sample.SiteID}).Validate(); err != nil {
		return err
	}
	_, err := store.pool.Exec(ctx, `
		INSERT INTO telemetry_samples (
			organization_id, site_id, agent_id, recorded_at, cpu_percent, memory_percent, disk_percent,
			network_rx_bytes, network_tx_bytes
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (organization_id, site_id, agent_id, recorded_at) DO UPDATE SET
			cpu_percent = EXCLUDED.cpu_percent,
			memory_percent = EXCLUDED.memory_percent,
			disk_percent = EXCLUDED.disk_percent,
			network_rx_bytes = EXCLUDED.network_rx_bytes,
			network_tx_bytes = EXCLUDED.network_tx_bytes
		WHERE telemetry_samples.organization_id = $1 AND telemetry_samples.site_id = $2`,
		sample.OrganizationID,
		sample.SiteID,
		sample.AgentID,
		sample.RecordedAt,
		sample.CPUPercent,
		sample.MemoryPercent,
		sample.DiskPercent,
		int64(sample.NetworkRXBytes),
		int64(sample.NetworkTXBytes),
	)
	if err != nil {
		return fmt.Errorf("append telemetry sample: %w", err)
	}
	return nil
}

func (store *Store) History(ctx context.Context, scope tenancy.Scope, agentID string, from, to time.Time, limit int) ([]telemetry.Sample, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT organization_id, site_id, agent_id, recorded_at, cpu_percent, memory_percent, disk_percent,
			network_rx_bytes, network_tx_bytes
		FROM (
			SELECT organization_id, site_id, agent_id, recorded_at, cpu_percent, memory_percent, disk_percent,
				network_rx_bytes, network_tx_bytes
			FROM telemetry_samples
			WHERE organization_id = $1 AND site_id = $2 AND agent_id = $3 AND recorded_at >= $4 AND recorded_at <= $5
			ORDER BY recorded_at DESC
			LIMIT $6
		) bounded
		ORDER BY recorded_at`, scope.OrganizationID, scope.SiteID, agentID, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("query telemetry history: %w", err)
	}
	defer rows.Close()

	samples := make([]telemetry.Sample, 0)
	for rows.Next() {
		var sample telemetry.Sample
		var networkRXBytes int64
		var networkTXBytes int64
		if err := rows.Scan(
			&sample.OrganizationID,
			&sample.SiteID,
			&sample.AgentID,
			&sample.RecordedAt,
			&sample.CPUPercent,
			&sample.MemoryPercent,
			&sample.DiskPercent,
			&networkRXBytes,
			&networkTXBytes,
		); err != nil {
			return nil, fmt.Errorf("scan telemetry history: %w", err)
		}
		sample.NetworkRXBytes = uint64(networkRXBytes)
		sample.NetworkTXBytes = uint64(networkTXBytes)
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate telemetry history: %w", err)
	}
	return samples, nil
}

package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/telemetry"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	pool             *pgxpool.Pool
	timescaleEnabled bool
}

type Option func(*Store)

func WithTimescale(enabled bool) Option {
	return func(store *Store) {
		store.timescaleEnabled = enabled
	}
}

func Open(ctx context.Context, databaseURL string, options ...Option) (*Store, error) {
	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL pool: %w", err)
	}
	store := &Store{pool: pool}
	for _, option := range options {
		option(store)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	if err := store.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if store.timescaleEnabled {
		if err := store.enableTimescale(ctx); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return store, nil
}

func (store *Store) Close() {
	store.pool.Close()
}

func (store *Store) Upsert(ctx context.Context, host inventory.Host) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO hosts (
			agent_id, hostname, os_family, os_name, os_version, architecture,
			kernel_version, cpu_cores, memory_bytes, ip_addresses, agent_version,
			first_seen_at, last_seen_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (agent_id) DO UPDATE SET
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
			last_seen_at = EXCLUDED.last_seen_at`,
		host.AgentID,
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
	return nil
}

func (store *Store) List(ctx context.Context) ([]inventory.Host, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT agent_id, hostname, os_family, os_name, os_version, architecture,
			kernel_version, cpu_cores, memory_bytes, ip_addresses, agent_version,
			first_seen_at, last_seen_at
		FROM hosts
		ORDER BY hostname`)
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

func (store *Store) Append(ctx context.Context, sample telemetry.Sample) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO telemetry_samples (
			agent_id, recorded_at, cpu_percent, memory_percent, disk_percent,
			network_rx_bytes, network_tx_bytes
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (agent_id, recorded_at) DO UPDATE SET
			cpu_percent = EXCLUDED.cpu_percent,
			memory_percent = EXCLUDED.memory_percent,
			disk_percent = EXCLUDED.disk_percent,
			network_rx_bytes = EXCLUDED.network_rx_bytes,
			network_tx_bytes = EXCLUDED.network_tx_bytes`,
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

func (store *Store) History(ctx context.Context, agentID string, from, to time.Time, limit int) ([]telemetry.Sample, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT agent_id, recorded_at, cpu_percent, memory_percent, disk_percent,
			network_rx_bytes, network_tx_bytes
		FROM (
			SELECT agent_id, recorded_at, cpu_percent, memory_percent, disk_percent,
				network_rx_bytes, network_tx_bytes
			FROM telemetry_samples
			WHERE agent_id = $1 AND recorded_at >= $2 AND recorded_at <= $3
			ORDER BY recorded_at DESC
			LIMIT $4
		) bounded
		ORDER BY recorded_at`, agentID, from, to, limit)
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

func (store *Store) migrate(ctx context.Context) error {
	if _, err := store.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		applied, err := store.migrationApplied(ctx, entry.Name())
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		migration, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		transaction, err := store.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", entry.Name(), err)
		}
		if _, err := transaction.Exec(ctx, string(migration)); err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if _, err := transaction.Exec(ctx, "INSERT INTO schema_migrations (name) VALUES ($1)", entry.Name()); err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (store *Store) migrationApplied(ctx context.Context, name string) (bool, error) {
	var applied bool
	err := store.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)", name).Scan(&applied)
	if err != nil {
		return false, fmt.Errorf("check migration %s: %w", name, err)
	}
	return applied, nil
}

func (store *Store) enableTimescale(ctx context.Context) error {
	statements := []string{
		"CREATE EXTENSION IF NOT EXISTS timescaledb",
		"SELECT create_hypertable('telemetry_samples', 'recorded_at', if_not_exists => TRUE, migrate_data => TRUE)",
		"SELECT add_retention_policy('telemetry_samples', INTERVAL '30 days', if_not_exists => TRUE)",
	}
	for _, statement := range statements {
		if _, err := store.pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("configure TimescaleDB: %w", err)
		}
	}
	return nil
}

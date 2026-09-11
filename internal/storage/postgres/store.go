package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
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

func (store *Store) ReplaceServices(ctx context.Context, agentID string, services []serviceinventory.Service) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin service snapshot replacement: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var hostExists int
	if err := transaction.QueryRow(ctx, "SELECT 1 FROM hosts WHERE agent_id = $1 FOR UPDATE", agentID).Scan(&hostExists); err != nil {
		return fmt.Errorf("lock service snapshot host: %w", err)
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM services WHERE agent_id = $1", agentID); err != nil {
		return fmt.Errorf("clear previous service snapshot: %w", err)
	}
	for _, service := range services {
		if _, err := transaction.Exec(ctx, `
			INSERT INTO services (agent_id, name, display_name, state, startup_type, observed_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			service.AgentID, service.Name, service.DisplayName, service.State, service.StartupType, service.ObservedAt,
		); err != nil {
			return fmt.Errorf("insert service snapshot: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit service snapshot replacement: %w", err)
	}
	return nil
}

func (store *Store) ListServices(ctx context.Context, agentID string, filter serviceinventory.Filter) ([]serviceinventory.Service, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT agent_id, name, display_name, state, startup_type, observed_at
		FROM services
		WHERE agent_id = $1
			AND ($2 = '' OR state = $2)
			AND ($3 = '' OR name ILIKE '%' || $3 || '%' OR display_name ILIKE '%' || $3 || '%')
		ORDER BY name`, agentID, filter.State, filter.Query)
	if err != nil {
		return nil, fmt.Errorf("query service inventory: %w", err)
	}
	defer rows.Close()
	services := make([]serviceinventory.Service, 0)
	for rows.Next() {
		var service serviceinventory.Service
		if err := rows.Scan(&service.AgentID, &service.Name, &service.DisplayName, &service.State, &service.StartupType, &service.ObservedAt); err != nil {
			return nil, fmt.Errorf("scan service inventory: %w", err)
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service inventory: %w", err)
	}
	return services, nil
}

func (store *Store) AppendLogs(ctx context.Context, entries []logstream.Entry) error {
	rows := make([][]any, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []any{entry.ID, entry.AgentID, entry.OccurredAt, entry.Collector, entry.Source, entry.Severity, entry.Message})
	}
	if _, err := store.pool.CopyFrom(
		ctx,
		pgx.Identifier{"log_entries"},
		[]string{"id", "agent_id", "occurred_at", "collector", "source", "severity", "message"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return fmt.Errorf("append log batch: %w", err)
	}
	return nil
}

func (store *Store) SearchLogs(ctx context.Context, query logstream.Query) ([]logstream.Entry, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, agent_id, occurred_at, collector, source, severity, message
		FROM (
			SELECT id, agent_id, occurred_at, collector, source, severity, message
			FROM log_entries
			WHERE agent_id = $1 AND occurred_at >= $2 AND occurred_at <= $3
				AND ($4 = '' OR collector = $4)
				AND ($5 = '' OR severity = $5)
				AND ($6 = '' OR source ILIKE '%' || $6 || '%')
				AND ($7 = '' OR message ILIKE '%' || $7 || '%')
			ORDER BY occurred_at DESC, id DESC
			LIMIT $8
		) bounded
		ORDER BY occurred_at, id`,
		query.AgentID, query.From, query.To, query.Collector, query.Severity, query.Source, query.Text, query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()
	entries := make([]logstream.Entry, 0)
	for rows.Next() {
		var entry logstream.Entry
		if err := rows.Scan(&entry.ID, &entry.AgentID, &entry.OccurredAt, &entry.Collector, &entry.Source, &entry.Severity, &entry.Message); err != nil {
			return nil, fmt.Errorf("scan logs: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate logs: %w", err)
	}
	return entries, nil
}

func (store *Store) CreateJob(ctx context.Context, job jobs.Job, event jobs.Event) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin job creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `
		INSERT INTO jobs (id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		job.ID, job.AgentID, job.Action, job.Target, job.ApprovedBy, job.Reason, job.RequestedAt, job.Status, job.LastSequence, job.Signature, job.SigningPublicKey,
	); err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	if err := insertJobEvent(ctx, transaction, event); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit job creation: %w", err)
	}
	return nil
}

func (store *Store) ClaimNextJob(ctx context.Context, agentID string, occurredAt time.Time) (*jobs.Job, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin job claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	job, err := scanJob(transaction.QueryRow(ctx, `
		SELECT id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key
		FROM jobs WHERE agent_id=$1 AND status='queued'
		ORDER BY requested_at, id FOR UPDATE SKIP LOCKED LIMIT 1`, agentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select queued job: %w", err)
	}
	job.Status, job.LastSequence = jobs.StatusRunning, 1
	if _, err := transaction.Exec(ctx, "UPDATE jobs SET status=$2, last_sequence=$3 WHERE id=$1", job.ID, job.Status, job.LastSequence); err != nil {
		return nil, fmt.Errorf("claim queued job: %w", err)
	}
	event := jobs.Event{JobID: job.ID, Sequence: 1, Type: jobs.EventClaimed, Message: "agent claimed job", Actor: "agent:" + agentID, OccurredAt: occurredAt}
	if err := insertJobEvent(ctx, transaction, event); err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit job claim: %w", err)
	}
	return &job, nil
}

func (store *Store) RecordJobEvent(ctx context.Context, agentID, jobID string, request jobs.EventRequest, occurredAt time.Time) (jobs.Job, jobs.Event, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("begin job event: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	job, err := scanJob(transaction.QueryRow(ctx, `
		SELECT id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key
		FROM jobs WHERE id=$1 AND agent_id=$2 FOR UPDATE`, jobID, agentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return jobs.Job{}, jobs.Event{}, jobs.ErrJobNotFound
	}
	if err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("lock job event: %w", err)
	}
	if job.Status != jobs.StatusRunning || request.Sequence != job.LastSequence+1 {
		return jobs.Job{}, jobs.Event{}, jobs.ErrJobConflict
	}
	if request.Type == jobs.EventSucceeded {
		job.Status = jobs.StatusSucceeded
	}
	if request.Type == jobs.EventFailed {
		job.Status = jobs.StatusFailed
	}
	job.LastSequence = request.Sequence
	if _, err := transaction.Exec(ctx, "UPDATE jobs SET status=$2, last_sequence=$3 WHERE id=$1", job.ID, job.Status, job.LastSequence); err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("update job event state: %w", err)
	}
	event := jobs.Event{JobID: job.ID, Sequence: request.Sequence, Type: request.Type, Message: request.Message, Actor: "agent:" + agentID, OccurredAt: occurredAt}
	if err := insertJobEvent(ctx, transaction, event); err != nil {
		return jobs.Job{}, jobs.Event{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("commit job event: %w", err)
	}
	return job, event, nil
}

func (store *Store) ListJobs(ctx context.Context, agentID string, limit int) ([]jobs.Job, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key
		FROM jobs WHERE agent_id=$1 ORDER BY requested_at DESC, id DESC LIMIT $2`, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("query jobs: %w", err)
	}
	defer rows.Close()
	values := make([]jobs.Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan jobs: %w", err)
		}
		values = append(values, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return values, nil
}

func (store *Store) ListJobEvents(ctx context.Context, agentID, jobID string) ([]jobs.Event, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT event.job_id, event.sequence, event.type, event.message, event.actor, event.occurred_at
		FROM job_events event JOIN jobs job ON job.id=event.job_id
		WHERE event.job_id=$1 AND job.agent_id=$2 ORDER BY event.sequence`, jobID, agentID)
	if err != nil {
		return nil, fmt.Errorf("query job events: %w", err)
	}
	defer rows.Close()
	values := make([]jobs.Event, 0)
	for rows.Next() {
		var event jobs.Event
		if err := rows.Scan(&event.JobID, &event.Sequence, &event.Type, &event.Message, &event.Actor, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan job events: %w", err)
		}
		values = append(values, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job events: %w", err)
	}
	if len(values) == 0 {
		return nil, jobs.ErrJobNotFound
	}
	return values, nil
}

type rowScanner interface{ Scan(...any) error }

func scanJob(row rowScanner) (jobs.Job, error) {
	var job jobs.Job
	err := row.Scan(&job.ID, &job.AgentID, &job.Action, &job.Target, &job.ApprovedBy, &job.Reason, &job.RequestedAt, &job.Status, &job.LastSequence, &job.Signature, &job.SigningPublicKey)
	return job, err
}

func insertJobEvent(ctx context.Context, transaction pgx.Tx, event jobs.Event) error {
	if _, err := transaction.Exec(ctx, `INSERT INTO job_events (job_id, sequence, type, message, actor, occurred_at) VALUES ($1,$2,$3,$4,$5,$6)`, event.JobID, event.Sequence, event.Type, event.Message, event.Actor, event.OccurredAt); err != nil {
		return fmt.Errorf("insert job event: %w", err)
	}
	return nil
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
		"SELECT create_hypertable('log_entries', 'occurred_at', if_not_exists => TRUE, migrate_data => TRUE)",
		"SELECT add_retention_policy('log_entries', INTERVAL '14 days', if_not_exists => TRUE)",
	}
	for _, statement := range statements {
		if _, err := store.pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("configure TimescaleDB: %w", err)
		}
	}
	return nil
}

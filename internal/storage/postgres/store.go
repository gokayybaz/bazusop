package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	pool                   *pgxpool.Pool
	listenerConfig         *pgx.ConnConfig
	timescaleEnabled       bool
	telemetryRetentionDays int
	logRetentionDays       int
	liveMu                 sync.RWMutex
	liveSubscribers        map[tenancy.Agent]map[chan logstream.Entry]struct{}
	listenerCancel         context.CancelFunc
	listenerDone           chan struct{}
}

var _ enrollment.StateStore = (*Store)(nil)

const (
	defaultTelemetryRetentionDays = 30
	defaultLogRetentionDays       = 14
)

type Option func(*Store)

func WithTimescale(enabled bool) Option {
	return func(store *Store) {
		store.timescaleEnabled = enabled
	}
}

func WithRetention(telemetryDays, logDays int) Option {
	return func(store *Store) {
		store.telemetryRetentionDays = telemetryDays
		store.logRetentionDays = logDays
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
	store := &Store{
		pool: pool, listenerConfig: configuration.ConnConfig,
		telemetryRetentionDays: defaultTelemetryRetentionDays, logRetentionDays: defaultLogRetentionDays,
		liveSubscribers: make(map[tenancy.Agent]map[chan logstream.Entry]struct{}),
	}
	for _, option := range options {
		option(store)
	}
	if err := store.validateRetention(); err != nil {
		pool.Close()
		return nil, err
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
	if err := store.startLogListener(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) Close() {
	if store.listenerCancel != nil {
		store.listenerCancel()
		<-store.listenerDone
	}
	store.pool.Close()
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
		"SELECT create_hypertable('log_entries', 'occurred_at', if_not_exists => TRUE, migrate_data => TRUE)",
	}
	for _, statement := range statements {
		if _, err := store.pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("configure TimescaleDB: %w", err)
		}
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin TimescaleDB retention configuration: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('bazusop_retention_policy'))"); err != nil {
		return fmt.Errorf("lock TimescaleDB retention configuration: %w", err)
	}
	for _, policy := range store.retentionPolicies() {
		if _, err := transaction.Exec(ctx, "SELECT remove_retention_policy($1::regclass, if_exists => TRUE)", policy.table); err != nil {
			return fmt.Errorf("remove %s retention policy: %w", policy.table, err)
		}
		if _, err := transaction.Exec(ctx, "SELECT add_retention_policy($1::regclass, drop_after => make_interval(days => $2))", policy.table, policy.days); err != nil {
			return fmt.Errorf("add %s retention policy: %w", policy.table, err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit TimescaleDB retention configuration: %w", err)
	}
	return nil
}

type retentionPolicy struct {
	table string
	days  int
}

func (store *Store) retentionPolicies() []retentionPolicy {
	return []retentionPolicy{
		{table: "telemetry_samples", days: store.telemetryRetentionDays},
		{table: "log_entries", days: store.logRetentionDays},
	}
}

func (store *Store) validateRetention() error {
	for _, policy := range store.retentionPolicies() {
		if policy.days < 1 || policy.days > 3650 {
			return fmt.Errorf("%s retention must be between 1 and 3650 days", policy.table)
		}
	}
	return nil
}

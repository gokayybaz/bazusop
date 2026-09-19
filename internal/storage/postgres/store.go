package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/telemetry"
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
	logNotificationChannel        = "bazusop_log_entries"
	postgresNotifyPayloadLimit    = 8000
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

func (store *Store) AppendLogs(ctx context.Context, entries []logstream.Entry) ([]logstream.Entry, error) {
	for _, entry := range entries {
		if err := (tenancy.Agent{ID: entry.AgentID, OrganizationID: entry.OrganizationID, SiteID: entry.SiteID}).Validate(); err != nil {
			return nil, err
		}
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin log batch: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	stored := make([]logstream.Entry, 0, len(entries))
	for _, entry := range entries {
		tag, err := transaction.Exec(ctx, `
			INSERT INTO log_entries (organization_id,site_id,id,agent_id,occurred_at,collector,source,severity,message)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (organization_id,site_id,id,occurred_at) DO NOTHING`,
			entry.OrganizationID, entry.SiteID, entry.ID, entry.AgentID, entry.OccurredAt, entry.Collector, entry.Source, entry.Severity, entry.Message)
		if err != nil {
			return nil, fmt.Errorf("append log entry: %w", err)
		}
		if tag.RowsAffected() == 1 {
			stored = append(stored, entry)
		}
	}
	payloads, err := encodeLogNotificationBatches(stored)
	if err != nil {
		return nil, err
	}
	for _, payload := range payloads {
		if _, err := transaction.Exec(ctx, "SELECT pg_notify($1, $2)", logNotificationChannel, payload); err != nil {
			return nil, fmt.Errorf("notify live log batch: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit log batch: %w", err)
	}
	return stored, nil
}

type logNotificationIdentity struct {
	OrganizationID string    `json:"organization_id"`
	SiteID         string    `json:"site_id"`
	ID             string    `json:"id"`
	OccurredAt     time.Time `json:"occurred_at"`
}

func encodeLogNotificationBatches(entries []logstream.Entry) ([]string, error) {
	payloads := make([]string, 0, 1)
	current := make([]logNotificationIdentity, 0, len(entries))
	for _, entry := range entries {
		identity := logNotificationIdentity{entry.OrganizationID, entry.SiteID, entry.ID, entry.OccurredAt.UTC().Truncate(time.Microsecond)}
		candidate := append(append([]logNotificationIdentity(nil), current...), identity)
		encoded, err := json.Marshal(candidate)
		if err != nil {
			return nil, fmt.Errorf("encode live log notification: %w", err)
		}
		if len(encoded) >= postgresNotifyPayloadLimit {
			if len(current) == 0 {
				return nil, fmt.Errorf("encode live log notification: log id exceeds PostgreSQL payload limit")
			}
			completed, err := json.Marshal(current)
			if err != nil {
				return nil, fmt.Errorf("encode live log notification: %w", err)
			}
			payloads = append(payloads, string(completed))
			current = []logNotificationIdentity{identity}
			continue
		}
		current = candidate
	}
	if len(current) > 0 {
		encoded, err := json.Marshal(current)
		if err != nil {
			return nil, fmt.Errorf("encode live log notification: %w", err)
		}
		payloads = append(payloads, string(encoded))
	}
	return payloads, nil
}

func (store *Store) SubscribeLogs(ctx context.Context, scope tenancy.Scope, agentID string) (<-chan logstream.Entry, error) {
	key := tenancy.Agent{ID: agentID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	stream := make(chan logstream.Entry, logstream.LiveSubscriberBuffer)
	store.liveMu.Lock()
	if store.liveSubscribers[key] == nil {
		store.liveSubscribers[key] = make(map[chan logstream.Entry]struct{})
	}
	store.liveSubscribers[key][stream] = struct{}{}
	store.liveMu.Unlock()
	go func() {
		<-ctx.Done()
		store.liveMu.Lock()
		delete(store.liveSubscribers[key], stream)
		if len(store.liveSubscribers[key]) == 0 {
			delete(store.liveSubscribers, key)
		}
		store.liveMu.Unlock()
	}()
	return stream, nil
}

func (store *Store) startLogListener(startupContext context.Context) error {
	connection, err := pgx.ConnectConfig(startupContext, store.listenerConfig)
	if err != nil {
		return fmt.Errorf("acquire live log listener: %w", err)
	}
	if _, err := connection.Exec(startupContext, "LISTEN "+logNotificationChannel); err != nil {
		_ = connection.Close(startupContext)
		return fmt.Errorf("listen for live logs: %w", err)
	}
	listenerContext, cancel := context.WithCancel(context.Background())
	store.listenerCancel = cancel
	store.listenerDone = make(chan struct{})
	go store.runLogListener(listenerContext, connection)
	return nil
}

func (store *Store) runLogListener(ctx context.Context, connection *pgx.Conn) {
	defer close(store.listenerDone)
	for {
		for ctx.Err() == nil {
			notification, err := connection.WaitForNotification(ctx)
			if err != nil {
				break
			}
			store.deliverLogNotification(ctx, notification.Payload)
		}
		_ = connection.Close(context.Background())
		if ctx.Err() != nil {
			return
		}
		var err error
		connection, err = store.acquireLogListener(ctx)
		if err != nil {
			return
		}
	}
}

func (store *Store) acquireLogListener(ctx context.Context) (*pgx.Conn, error) {
	for ctx.Err() == nil {
		connection, err := pgx.ConnectConfig(ctx, store.listenerConfig)
		if err == nil {
			if _, err = connection.Exec(ctx, "LISTEN "+logNotificationChannel); err == nil {
				return connection, nil
			}
			_ = connection.Close(context.Background())
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, ctx.Err()
}

func (store *Store) deliverLogNotification(ctx context.Context, payload string) {
	store.liveMu.RLock()
	hasSubscribers := len(store.liveSubscribers) > 0
	store.liveMu.RUnlock()
	if !hasSubscribers {
		return
	}
	var identities []logNotificationIdentity
	if err := json.Unmarshal([]byte(payload), &identities); err != nil || len(identities) == 0 {
		return
	}
	for _, identity := range identities {
		if err := (tenancy.Scope{OrganizationID: identity.OrganizationID, SiteID: identity.SiteID}).Validate(); err != nil || identity.ID == "" || identity.OccurredAt.IsZero() {
			return
		}
	}
	rows, err := store.pool.Query(ctx, `
		SELECT entry.organization_id, entry.site_id, entry.id, entry.agent_id, entry.occurred_at, entry.collector, entry.source, entry.severity, entry.message
		FROM log_entries entry
		WHERE EXISTS (
			SELECT 1 FROM jsonb_to_recordset($1::jsonb)
			AS identity(organization_id text, site_id text, id text, occurred_at timestamptz)
			WHERE entry.organization_id = identity.organization_id AND entry.site_id = identity.site_id
				AND entry.id = identity.id AND entry.occurred_at = identity.occurred_at
		)
		ORDER BY entry.occurred_at, entry.id`, payload)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var entry logstream.Entry
		if err := rows.Scan(&entry.OrganizationID, &entry.SiteID, &entry.ID, &entry.AgentID, &entry.OccurredAt, &entry.Collector, &entry.Source, &entry.Severity, &entry.Message); err != nil {
			return
		}
		store.publishLiveLog(entry)
	}
}

func (store *Store) publishLiveLog(entry logstream.Entry) {
	store.liveMu.RLock()
	defer store.liveMu.RUnlock()
	for stream := range store.liveSubscribers[tenancy.Agent{ID: entry.AgentID, OrganizationID: entry.OrganizationID, SiteID: entry.SiteID}] {
		select {
		case stream <- entry:
		default:
		}
	}
}

func (store *Store) SearchLogs(ctx context.Context, query logstream.Query) ([]logstream.Entry, error) {
	if err := query.Scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT organization_id, site_id, id, agent_id, occurred_at, collector, source, severity, message
		FROM (
			SELECT organization_id, site_id, id, agent_id, occurred_at, collector, source, severity, message
			FROM log_entries
			WHERE organization_id = $1 AND site_id = $2 AND agent_id = $3 AND occurred_at >= $4 AND occurred_at <= $5
				AND ($6 = '' OR collector = $6)
				AND ($7 = '' OR severity = $7)
				AND ($8 = '' OR source ILIKE '%' || $8 || '%')
				AND ($9 = '' OR message ILIKE '%' || $9 || '%')
			ORDER BY occurred_at DESC, id DESC
			LIMIT $10
		) bounded
		ORDER BY occurred_at, id`,
		query.Scope.OrganizationID, query.Scope.SiteID, query.AgentID, query.From, query.To, query.Collector, query.Severity, query.Source, query.Text, query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()
	entries := make([]logstream.Entry, 0)
	for rows.Next() {
		var entry logstream.Entry
		if err := rows.Scan(&entry.OrganizationID, &entry.SiteID, &entry.ID, &entry.AgentID, &entry.OccurredAt, &entry.Collector, &entry.Source, &entry.Severity, &entry.Message); err != nil {
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
	scope := tenancy.Scope{OrganizationID: job.OrganizationID, SiteID: job.SiteID}
	if err := scope.Validate(); err != nil || event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
		return tenancy.ErrInvalidScope
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin job creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var hostExists int
	if err := transaction.QueryRow(ctx, "SELECT 1 FROM hosts WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3 FOR UPDATE", scope.OrganizationID, scope.SiteID, job.AgentID).Scan(&hostExists); errors.Is(err, pgx.ErrNoRows) {
		return jobs.ErrJobNotFound
	} else if err != nil {
		return fmt.Errorf("lock job target host: %w", err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO jobs (id, organization_id, site_id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, job.ID, job.OrganizationID, job.SiteID, job.AgentID, job.Action, job.Target, job.ApprovedBy, job.Reason, job.RequestedAt, job.Status, job.LastSequence, job.Signature, job.SigningPublicKey); err != nil {
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

func (store *Store) ClaimNextJob(ctx context.Context, agent tenancy.Agent, occurredAt, resumeBefore time.Time) (*jobs.Job, error) {
	if err := agent.Validate(); err != nil {
		return nil, err
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin job claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(json_build_array($1::text, $2::text, $3::text)::text, 0))", agent.OrganizationID, agent.SiteID, agent.ID); err != nil {
		return nil, fmt.Errorf("lock agent job queue: %w", err)
	}
	running, err := scanJob(transaction.QueryRow(ctx, `SELECT id, organization_id, site_id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key FROM jobs WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3 AND status='running' ORDER BY requested_at, id FOR UPDATE LIMIT 1`, agent.OrganizationID, agent.SiteID, agent.ID))
	if err == nil {
		var claimedAt time.Time
		if err := transaction.QueryRow(ctx, "SELECT occurred_at FROM job_events WHERE organization_id=$1 AND site_id=$2 AND job_id=$3 AND type='claimed' ORDER BY sequence DESC LIMIT 1", agent.OrganizationID, agent.SiteID, running.ID).Scan(&claimedAt); err != nil {
			return nil, fmt.Errorf("read running job lease: %w", err)
		}
		if claimedAt.After(resumeBefore) {
			if err := transaction.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit active job lease check: %w", err)
			}
			return nil, nil
		}
		running.Resumed = true
		if err := transaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit running job resume: %w", err)
		}
		return &running, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("select running job: %w", err)
	}
	job, err := scanJob(transaction.QueryRow(ctx, `SELECT id, organization_id, site_id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key FROM jobs WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3 AND status='queued' ORDER BY requested_at, id FOR UPDATE SKIP LOCKED LIMIT 1`, agent.OrganizationID, agent.SiteID, agent.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select queued job: %w", err)
	}
	job.Status, job.LastSequence = jobs.StatusRunning, 1
	if _, err := transaction.Exec(ctx, "UPDATE jobs SET status=$4, last_sequence=$5 WHERE id=$3 AND organization_id=$1 AND site_id=$2", agent.OrganizationID, agent.SiteID, job.ID, job.Status, job.LastSequence); err != nil {
		return nil, fmt.Errorf("claim queued job: %w", err)
	}
	event := jobs.Event{JobID: job.ID, OrganizationID: agent.OrganizationID, SiteID: agent.SiteID, Sequence: 1, Type: jobs.EventClaimed, Message: "agent claimed job", Actor: "agent:" + agent.ID, OccurredAt: occurredAt}
	if err := insertJobEvent(ctx, transaction, event); err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit job claim: %w", err)
	}
	return &job, nil
}

func (store *Store) RecordJobEvent(ctx context.Context, agent tenancy.Agent, jobID string, request jobs.EventRequest, occurredAt time.Time) (jobs.Job, jobs.Event, error) {
	if err := agent.Validate(); err != nil {
		return jobs.Job{}, jobs.Event{}, err
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("begin job event: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	job, err := scanJob(transaction.QueryRow(ctx, `SELECT id, organization_id, site_id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key FROM jobs WHERE id=$1 AND organization_id=$2 AND site_id=$3 AND agent_id=$4 FOR UPDATE`, jobID, agent.OrganizationID, agent.SiteID, agent.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return jobs.Job{}, jobs.Event{}, jobs.ErrJobNotFound
	}
	if err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("lock job event: %w", err)
	}
	if request.Sequence <= job.LastSequence {
		var event jobs.Event
		err := transaction.QueryRow(ctx, `SELECT job_id,organization_id,site_id,sequence,type,message,actor,occurred_at FROM job_events WHERE job_id=$1 AND organization_id=$2 AND site_id=$3 AND sequence=$4`, jobID, agent.OrganizationID, agent.SiteID, request.Sequence).Scan(&event.JobID, &event.OrganizationID, &event.SiteID, &event.Sequence, &event.Type, &event.Message, &event.Actor, &event.OccurredAt)
		if err == nil && event.Type == request.Type && event.Message == request.Message {
			if err := transaction.Commit(ctx); err != nil {
				return jobs.Job{}, jobs.Event{}, fmt.Errorf("commit duplicate job event: %w", err)
			}
			return job, event, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return jobs.Job{}, jobs.Event{}, fmt.Errorf("read duplicate job event: %w", err)
		}
		return jobs.Job{}, jobs.Event{}, jobs.ErrJobConflict
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
	if _, err := transaction.Exec(ctx, "UPDATE jobs SET status=$4, last_sequence=$5 WHERE id=$3 AND organization_id=$1 AND site_id=$2", agent.OrganizationID, agent.SiteID, job.ID, job.Status, job.LastSequence); err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("update job event state: %w", err)
	}
	event := jobs.Event{JobID: job.ID, OrganizationID: agent.OrganizationID, SiteID: agent.SiteID, Sequence: request.Sequence, Type: request.Type, Message: request.Message, Actor: "agent:" + agent.ID, OccurredAt: occurredAt}
	if err := insertJobEvent(ctx, transaction, event); err != nil {
		return jobs.Job{}, jobs.Event{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("commit job event: %w", err)
	}
	return job, event, nil
}

func (store *Store) ListJobs(ctx context.Context, scope tenancy.Scope, agentID string, limit int) ([]jobs.Job, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `SELECT id, organization_id, site_id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key FROM jobs WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3 ORDER BY requested_at DESC, id DESC LIMIT $4`, scope.OrganizationID, scope.SiteID, agentID, limit)
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

func (store *Store) ListJobEvents(ctx context.Context, scope tenancy.Scope, agentID, jobID string) ([]jobs.Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `SELECT event.job_id, event.organization_id, event.site_id, event.sequence, event.type, event.message, event.actor, event.occurred_at FROM job_events event JOIN jobs job ON job.id=event.job_id WHERE event.job_id=$1 AND event.organization_id=$2 AND event.site_id=$3 AND job.organization_id=$2 AND job.site_id=$3 AND job.agent_id=$4 ORDER BY event.sequence`, jobID, scope.OrganizationID, scope.SiteID, agentID)
	if err != nil {
		return nil, fmt.Errorf("query job events: %w", err)
	}
	defer rows.Close()
	values := make([]jobs.Event, 0)
	for rows.Next() {
		var event jobs.Event
		if err := rows.Scan(&event.JobID, &event.OrganizationID, &event.SiteID, &event.Sequence, &event.Type, &event.Message, &event.Actor, &event.OccurredAt); err != nil {
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
	err := row.Scan(&job.ID, &job.OrganizationID, &job.SiteID, &job.AgentID, &job.Action, &job.Target, &job.ApprovedBy, &job.Reason, &job.RequestedAt, &job.Status, &job.LastSequence, &job.Signature, &job.SigningPublicKey)
	return job, err
}

func insertJobEvent(ctx context.Context, transaction pgx.Tx, event jobs.Event) error {
	if _, err := transaction.Exec(ctx, `INSERT INTO job_events (job_id, organization_id, site_id, sequence, type, message, actor, occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, event.JobID, event.OrganizationID, event.SiteID, event.Sequence, event.Type, event.Message, event.Actor, event.OccurredAt); err != nil {
		return fmt.Errorf("insert job event: %w", err)
	}
	return nil
}

func (store *Store) CreateAlertRule(ctx context.Context, scope tenancy.Scope, rule alerting.Rule) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if rule.OrganizationID != scope.OrganizationID || rule.SiteID != scope.SiteID {
		return tenancy.ErrInvalidScope
	}
	_, err := store.pool.Exec(ctx, `INSERT INTO alert_rules (organization_id,site_id,id,name,kind,metric,threshold,stale_after_seconds,severity,enabled,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, scope.OrganizationID, scope.SiteID, rule.ID, rule.Name, rule.Kind, rule.Metric, rule.Threshold, rule.StaleAfterSeconds, rule.Severity, rule.Enabled, rule.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert alert rule: %w", err)
	}
	return nil
}

func (store *Store) ListAlertRules(ctx context.Context, scope tenancy.Scope) ([]alerting.Rule, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `SELECT organization_id,site_id,id,name,kind,metric,threshold,stale_after_seconds,severity,enabled,created_at FROM alert_rules WHERE organization_id=$1 AND site_id=$2 ORDER BY created_at,id`, scope.OrganizationID, scope.SiteID)
	if err != nil {
		return nil, fmt.Errorf("query alert rules: %w", err)
	}
	defer rows.Close()
	values := make([]alerting.Rule, 0)
	for rows.Next() {
		var value alerting.Rule
		if err := rows.Scan(&value.OrganizationID, &value.SiteID, &value.ID, &value.Name, &value.Kind, &value.Metric, &value.Threshold, &value.StaleAfterSeconds, &value.Severity, &value.Enabled, &value.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan alert rule: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert rules: %w", err)
	}
	return values, nil
}

func (store *Store) CreateMaintenanceWindow(ctx context.Context, scope tenancy.Scope, window alerting.MaintenanceWindow) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if window.OrganizationID != scope.OrganizationID || window.SiteID != scope.SiteID {
		return tenancy.ErrInvalidScope
	}
	if window.AgentID != "" {
		var exists int
		if err := store.pool.QueryRow(ctx, `SELECT 1 FROM hosts WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3`, scope.OrganizationID, scope.SiteID, window.AgentID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
			return alerting.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("verify maintenance host: %w", err)
		}
	}
	_, err := store.pool.Exec(ctx, `INSERT INTO maintenance_windows (organization_id,site_id,id,name,agent_id,starts_at,ends_at,created_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, scope.OrganizationID, scope.SiteID, window.ID, window.Name, window.AgentID, window.StartsAt, window.EndsAt, window.CreatedBy, window.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert maintenance window: %w", err)
	}
	return nil
}

func (store *Store) HasHost(ctx context.Context, scope tenancy.Scope, agentID string) (bool, error) {
	if err := scope.Validate(); err != nil {
		return false, err
	}
	var exists int
	err := store.pool.QueryRow(ctx, `SELECT 1 FROM hosts WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3`, scope.OrganizationID, scope.SiteID, agentID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query alert host: %w", err)
	}
	return true, nil
}

func (store *Store) ListMaintenanceWindows(ctx context.Context, scope tenancy.Scope) ([]alerting.MaintenanceWindow, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `SELECT organization_id,site_id,id,name,agent_id,starts_at,ends_at,created_by,created_at FROM maintenance_windows WHERE organization_id=$1 AND site_id=$2 ORDER BY starts_at,id`, scope.OrganizationID, scope.SiteID)
	if err != nil {
		return nil, fmt.Errorf("query maintenance windows: %w", err)
	}
	defer rows.Close()
	values := make([]alerting.MaintenanceWindow, 0)
	for rows.Next() {
		var value alerting.MaintenanceWindow
		if err := rows.Scan(&value.OrganizationID, &value.SiteID, &value.ID, &value.Name, &value.AgentID, &value.StartsAt, &value.EndsAt, &value.CreatedBy, &value.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan maintenance window: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate maintenance windows: %w", err)
	}
	return values, nil
}

func (store *Store) EnsureIncident(ctx context.Context, scope tenancy.Scope, incident alerting.Incident, event alerting.Event) (alerting.Incident, bool, error) {
	if err := scope.Validate(); err != nil {
		return alerting.Incident{}, false, err
	}
	if incident.OrganizationID != scope.OrganizationID || incident.SiteID != scope.SiteID || event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
		return alerting.Incident{}, false, tenancy.ErrInvalidScope
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return alerting.Incident{}, false, fmt.Errorf("begin incident: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	created := true
	err = tx.QueryRow(ctx, `INSERT INTO alert_incidents (organization_id,site_id,id,rule_id,rule_name,agent_id,severity,status,message,latest_value,opened_at,acknowledged_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'') ON CONFLICT (organization_id,site_id,rule_id,agent_id) WHERE status IN ('open','acknowledged') DO NOTHING RETURNING id`, scope.OrganizationID, scope.SiteID, incident.ID, incident.RuleID, incident.RuleName, incident.AgentID, incident.Severity, incident.Status, incident.Message, incident.LatestValue, incident.OpenedAt).Scan(&incident.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		created = false
		incident, err = scanAlertIncident(tx.QueryRow(ctx, `SELECT organization_id,site_id,id,rule_id,rule_name,agent_id,severity,status,message,latest_value,opened_at,acknowledged_at,acknowledged_by,resolved_at FROM alert_incidents WHERE organization_id=$1 AND site_id=$2 AND rule_id=$3 AND agent_id=$4 AND status IN ('open','acknowledged')`, scope.OrganizationID, scope.SiteID, incident.RuleID, incident.AgentID))
	}
	if err != nil {
		return alerting.Incident{}, false, fmt.Errorf("ensure incident: %w", err)
	}
	if created {
		event.IncidentID = incident.ID
		if err := insertAlertEvent(ctx, tx, event); err != nil {
			return alerting.Incident{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.Incident{}, false, fmt.Errorf("commit incident: %w", err)
	}
	return incident, created, nil
}

func (store *Store) ResolveIncident(ctx context.Context, scope tenancy.Scope, ruleID, agentID string, value float64, message string, event alerting.Event) (*alerting.Incident, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
		return nil, tenancy.ErrInvalidScope
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin incident resolution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	incident, err := scanAlertIncident(tx.QueryRow(ctx, `SELECT organization_id,site_id,id,rule_id,rule_name,agent_id,severity,status,message,latest_value,opened_at,acknowledged_at,acknowledged_by,resolved_at FROM alert_incidents WHERE organization_id=$1 AND site_id=$2 AND rule_id=$3 AND agent_id=$4 AND status IN ('open','acknowledged') FOR UPDATE`, scope.OrganizationID, scope.SiteID, ruleID, agentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock incident resolution: %w", err)
	}
	resolvedAt := event.OccurredAt
	incident.Status = alerting.StatusResolved
	incident.ResolvedAt = &resolvedAt
	incident.LatestValue = value
	incident.Message = message
	if _, err := tx.Exec(ctx, `UPDATE alert_incidents SET status=$2,resolved_at=$3,latest_value=$4,message=$5 WHERE id=$1 AND organization_id=$6 AND site_id=$7`, incident.ID, incident.Status, resolvedAt, value, message, scope.OrganizationID, scope.SiteID); err != nil {
		return nil, fmt.Errorf("resolve incident: %w", err)
	}
	event.IncidentID = incident.ID
	if err := insertAlertEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit incident resolution: %w", err)
	}
	return &incident, nil
}

func (store *Store) AcknowledgeIncident(ctx context.Context, scope tenancy.Scope, id, actor string, at time.Time, event alerting.Event) (alerting.Incident, error) {
	if err := scope.Validate(); err != nil {
		return alerting.Incident{}, err
	}
	if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
		return alerting.Incident{}, tenancy.ErrInvalidScope
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return alerting.Incident{}, fmt.Errorf("begin incident acknowledgement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	incident, err := scanAlertIncident(tx.QueryRow(ctx, `SELECT organization_id,site_id,id,rule_id,rule_name,agent_id,severity,status,message,latest_value,opened_at,acknowledged_at,acknowledged_by,resolved_at FROM alert_incidents WHERE id=$1 AND organization_id=$2 AND site_id=$3 FOR UPDATE`, id, scope.OrganizationID, scope.SiteID))
	if errors.Is(err, pgx.ErrNoRows) {
		return alerting.Incident{}, alerting.ErrNotFound
	}
	if err != nil {
		return alerting.Incident{}, fmt.Errorf("lock incident acknowledgement: %w", err)
	}
	if incident.Status != alerting.StatusOpen {
		return alerting.Incident{}, alerting.ErrConflict
	}
	incident.Status = alerting.StatusAcknowledged
	incident.AcknowledgedAt = &at
	incident.AcknowledgedBy = actor
	event.IncidentID = incident.ID
	if _, err := tx.Exec(ctx, `UPDATE alert_incidents SET status=$2,acknowledged_at=$3,acknowledged_by=$4 WHERE id=$1 AND organization_id=$5 AND site_id=$6`, id, incident.Status, at, actor, scope.OrganizationID, scope.SiteID); err != nil {
		return alerting.Incident{}, fmt.Errorf("acknowledge incident: %w", err)
	}
	if err := insertAlertEvent(ctx, tx, event); err != nil {
		return alerting.Incident{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.Incident{}, fmt.Errorf("commit incident acknowledgement: %w", err)
	}
	return incident, nil
}

func (store *Store) ListAlertIncidents(ctx context.Context, scope tenancy.Scope, limit int) ([]alerting.Incident, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `SELECT organization_id,site_id,id,rule_id,rule_name,agent_id,severity,status,message,latest_value,opened_at,acknowledged_at,acknowledged_by,resolved_at FROM alert_incidents WHERE organization_id=$1 AND site_id=$2 ORDER BY opened_at DESC,id DESC LIMIT $3`, scope.OrganizationID, scope.SiteID, limit)
	if err != nil {
		return nil, fmt.Errorf("query alert incidents: %w", err)
	}
	defer rows.Close()
	values := make([]alerting.Incident, 0)
	for rows.Next() {
		value, err := scanAlertIncident(rows)
		if err != nil {
			return nil, fmt.Errorf("scan alert incident: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert incidents: %w", err)
	}
	return values, nil
}

func (store *Store) ListAlertEvents(ctx context.Context, scope tenancy.Scope, incidentID string) ([]alerting.Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `SELECT event.organization_id,event.site_id,event.id,event.incident_id,event.type,event.actor,event.message,event.occurred_at FROM alert_events event JOIN alert_incidents incident ON incident.id=event.incident_id WHERE event.incident_id=$1 AND event.organization_id=$2 AND event.site_id=$3 AND incident.organization_id=$2 AND incident.site_id=$3 ORDER BY event.occurred_at,event.id`, incidentID, scope.OrganizationID, scope.SiteID)
	if err != nil {
		return nil, fmt.Errorf("query alert events: %w", err)
	}
	defer rows.Close()
	values := make([]alerting.Event, 0)
	for rows.Next() {
		var value alerting.Event
		if err := rows.Scan(&value.OrganizationID, &value.SiteID, &value.ID, &value.IncidentID, &value.Type, &value.Actor, &value.Message, &value.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan alert event: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert events: %w", err)
	}
	if len(values) == 0 {
		return nil, alerting.ErrNotFound
	}
	return values, nil
}

func scanAlertIncident(row rowScanner) (alerting.Incident, error) {
	var value alerting.Incident
	err := row.Scan(&value.OrganizationID, &value.SiteID, &value.ID, &value.RuleID, &value.RuleName, &value.AgentID, &value.Severity, &value.Status, &value.Message, &value.LatestValue, &value.OpenedAt, &value.AcknowledgedAt, &value.AcknowledgedBy, &value.ResolvedAt)
	return value, err
}
func insertAlertEvent(ctx context.Context, tx pgx.Tx, event alerting.Event) error {
	_, err := tx.Exec(ctx, `INSERT INTO alert_events (organization_id,site_id,id,incident_id,type,actor,message,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, event.OrganizationID, event.SiteID, event.ID, event.IncidentID, event.Type, event.Actor, event.Message, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("insert alert event: %w", err)
	}
	return nil
}

func (store *Store) CreateCloudAccount(ctx context.Context, account cloudinventory.Account) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO cloud_accounts (id,name,provider,external_id,status,last_sync_at,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, account.ID, account.Name, account.Provider, account.ExternalID, account.Status, account.LastSyncAt, account.CreatedAt)
	if err != nil {
		return fmt.Errorf("create cloud account: %w", err)
	}
	return nil
}

func (store *Store) GetCloudAccount(ctx context.Context, id string) (cloudinventory.Account, error) {
	var account cloudinventory.Account
	err := store.pool.QueryRow(ctx, `SELECT id,name,provider,external_id,status,last_sync_at,created_at FROM cloud_accounts WHERE id=$1`, id).Scan(
		&account.ID, &account.Name, &account.Provider, &account.ExternalID, &account.Status, &account.LastSyncAt, &account.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudinventory.Account{}, cloudinventory.ErrCloudAccountNotFound
	}
	if err != nil {
		return cloudinventory.Account{}, fmt.Errorf("get cloud account: %w", err)
	}
	return account, nil
}

func (store *Store) ListCloudAccounts(ctx context.Context) ([]cloudinventory.Account, error) {
	rows, err := store.pool.Query(ctx, `SELECT id,name,provider,external_id,status,last_sync_at,created_at FROM cloud_accounts ORDER BY name,id`)
	if err != nil {
		return nil, fmt.Errorf("query cloud accounts: %w", err)
	}
	defer rows.Close()
	accounts := make([]cloudinventory.Account, 0)
	for rows.Next() {
		var account cloudinventory.Account
		if err := rows.Scan(&account.ID, &account.Name, &account.Provider, &account.ExternalID, &account.Status, &account.LastSyncAt, &account.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan cloud account: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud accounts: %w", err)
	}
	return accounts, nil
}

func (store *Store) ReplaceCloudInstances(ctx context.Context, account cloudinventory.Account, instances []cloudinventory.Instance) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cloud reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `UPDATE cloud_accounts SET status=$2,last_sync_at=$3 WHERE id=$1`, account.ID, account.Status, account.LastSyncAt)
	if err != nil {
		return fmt.Errorf("update cloud account sync: %w", err)
	}
	if result.RowsAffected() == 0 {
		return cloudinventory.ErrCloudAccountNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cloud_instances WHERE account_id=$1`, account.ID); err != nil {
		return fmt.Errorf("replace cloud instances: %w", err)
	}
	for _, instance := range instances {
		metadata, err := json.Marshal(instance.Metadata)
		if err != nil {
			return fmt.Errorf("encode cloud instance metadata: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO cloud_instances (
				account_id,provider_instance_id,name,region,zone,state,os_family,private_ips,public_ips,
				agent_id_hint,metadata,agent_id,candidate_agent_id,match_status,match_reason,discovered_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''),NULLIF($13,''),$14,$15,$16)`,
			instance.AccountID, instance.ProviderInstanceID, instance.Name, instance.Region, instance.Zone, instance.State,
			instance.OSFamily, instance.PrivateIPs, instance.PublicIPs, instance.AgentIDHint, metadata, instance.AgentID,
			instance.CandidateAgentID, instance.MatchStatus, instance.MatchReason, instance.DiscoveredAt,
		)
		if err != nil {
			return fmt.Errorf("insert cloud instance: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cloud reconciliation: %w", err)
	}
	return nil
}

func (store *Store) ListCloudInstances(ctx context.Context) ([]cloudinventory.Instance, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT i.account_id,a.name,a.provider,i.provider_instance_id,i.name,i.region,i.zone,i.state,i.os_family,
			i.private_ips,i.public_ips,i.agent_id_hint,i.metadata,COALESCE(i.agent_id,''),COALESCE(i.candidate_agent_id,''),
			i.match_status,i.match_reason,i.discovered_at
		FROM cloud_instances i JOIN cloud_accounts a ON a.id=i.account_id
		ORDER BY a.name,i.name,i.provider_instance_id`)
	if err != nil {
		return nil, fmt.Errorf("query cloud instances: %w", err)
	}
	defer rows.Close()
	instances := make([]cloudinventory.Instance, 0)
	for rows.Next() {
		var instance cloudinventory.Instance
		var metadata []byte
		if err := rows.Scan(
			&instance.AccountID, &instance.AccountName, &instance.Provider, &instance.ProviderInstanceID, &instance.Name,
			&instance.Region, &instance.Zone, &instance.State, &instance.OSFamily, &instance.PrivateIPs, &instance.PublicIPs,
			&instance.AgentIDHint, &metadata, &instance.AgentID, &instance.CandidateAgentID, &instance.MatchStatus,
			&instance.MatchReason, &instance.DiscoveredAt,
		); err != nil {
			return nil, fmt.Errorf("scan cloud instance: %w", err)
		}
		if err := json.Unmarshal(metadata, &instance.Metadata); err != nil {
			return nil, fmt.Errorf("decode cloud instance metadata: %w", err)
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud instances: %w", err)
	}
	return instances, nil
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

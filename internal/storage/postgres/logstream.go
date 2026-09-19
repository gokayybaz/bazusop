package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

const (
	logNotificationChannel     = "bazusop_log_entries"
	postgresNotifyPayloadLimit = 8000
)

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

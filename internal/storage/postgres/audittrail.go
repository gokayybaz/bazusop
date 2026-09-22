package postgres

import (
	"context"
	"fmt"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func (store *Store) Record(ctx context.Context, event audittrail.Event) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO audit_events (
			event_id, occurred_at, correlation_id, actor_type, actor_id, session_or_token_id,
			organization_id, site_id, action, permission, resource_type, resource_id,
			outcome, error_code, source_ip, user_agent, change_summary
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		event.EventID, event.OccurredAt, event.CorrelationID, string(event.ActorType), event.ActorID, event.SessionOrTokenID,
		event.OrganizationID, event.SiteID, event.Action, event.Permission, event.ResourceType, event.ResourceID,
		string(event.Outcome), event.ErrorCode, event.SourceIP, event.UserAgent, event.ChangeSummary,
	)
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

func (store *Store) ListAuditTrail(ctx context.Context, scope tenancy.Scope, filter audittrail.Filter, limit int) ([]audittrail.Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	query := `
		SELECT event_id, occurred_at, correlation_id, actor_type, actor_id, session_or_token_id,
			organization_id, site_id, action, permission, resource_type, resource_id,
			outcome, error_code, source_ip, user_agent, change_summary
		FROM audit_events
		WHERE organization_id = $1 AND site_id = $2`
	args := []any{scope.OrganizationID, scope.SiteID}
	if filter.ActorType != "" {
		args = append(args, string(filter.ActorType))
		query += fmt.Sprintf(" AND actor_type = $%d", len(args))
	}
	if filter.ResourceType != "" {
		args = append(args, filter.ResourceType)
		query += fmt.Sprintf(" AND resource_type = $%d", len(args))
	}
	if filter.Outcome != "" {
		args = append(args, string(filter.Outcome))
		query += fmt.Sprintf(" AND outcome = $%d", len(args))
	}
	if !filter.Since.IsZero() {
		args = append(args, filter.Since)
		query += fmt.Sprintf(" AND occurred_at >= $%d", len(args))
	}
	if !filter.Until.IsZero() {
		args = append(args, filter.Until)
		query += fmt.Sprintf(" AND occurred_at <= $%d", len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY occurred_at DESC LIMIT $%d", len(args))

	rows, err := store.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit trail: %w", err)
	}
	defer rows.Close()
	events := make([]audittrail.Event, 0)
	for rows.Next() {
		var event audittrail.Event
		var actorType, outcome string
		if err := rows.Scan(&event.EventID, &event.OccurredAt, &event.CorrelationID, &actorType, &event.ActorID, &event.SessionOrTokenID,
			&event.OrganizationID, &event.SiteID, &event.Action, &event.Permission, &event.ResourceType, &event.ResourceID,
			&outcome, &event.ErrorCode, &event.SourceIP, &event.UserAgent, &event.ChangeSummary); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		event.ActorType, event.Outcome = audittrail.ActorType(actorType), audittrail.Outcome(outcome)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit trail: %w", err)
	}
	return events, nil
}

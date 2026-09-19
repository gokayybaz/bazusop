package postgres

import (
	"context"
	"fmt"

	"github.com/gokayybaz/bazusop/internal/audit"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func (store *Store) ListAuditEvents(ctx context.Context, scope tenancy.Scope, limit int) ([]audit.Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT organization_id, site_id, source, reference_id, agent_id, type, actor, message, occurred_at
		FROM (
			SELECT job_events.organization_id, job_events.site_id, 'job' AS source, job_events.job_id AS reference_id,
				jobs.agent_id, job_events.type, job_events.actor, job_events.message, job_events.occurred_at
			FROM job_events
			JOIN jobs ON jobs.id = job_events.job_id
				AND jobs.organization_id = job_events.organization_id AND jobs.site_id = job_events.site_id
			WHERE job_events.organization_id = $1 AND job_events.site_id = $2

			UNION ALL

			SELECT event.organization_id, event.site_id, 'alert' AS source, event.incident_id AS reference_id,
				incident.agent_id, event.type, event.actor, event.message, event.occurred_at
			FROM alert_events event
			JOIN alert_incidents incident ON incident.id = event.incident_id
				AND incident.organization_id = event.organization_id AND incident.site_id = event.site_id
			WHERE event.organization_id = $1 AND event.site_id = $2
		) combined
		ORDER BY occurred_at DESC, source, reference_id
		LIMIT $3`, scope.OrganizationID, scope.SiteID, limit)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()
	events := make([]audit.Event, 0)
	for rows.Next() {
		var event audit.Event
		if err := rows.Scan(&event.OrganizationID, &event.SiteID, &event.Source, &event.ReferenceID, &event.AgentID, &event.Type, &event.Actor, &event.Message, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return events, nil
}

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

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

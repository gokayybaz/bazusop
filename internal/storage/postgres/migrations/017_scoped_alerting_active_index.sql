-- Replace the legacy global active-incident key with the tenant-scoped key.
-- The migration runner executes this file in one transaction, so replay is safe.
DROP INDEX IF EXISTS alert_incidents_active_rule_agent_idx;
CREATE UNIQUE INDEX IF NOT EXISTS alert_incidents_active_scoped_rule_agent_idx
    ON alert_incidents (organization_id, site_id, rule_id, agent_id)
    WHERE status IN ('open', 'acknowledged');

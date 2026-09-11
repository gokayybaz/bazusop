CREATE INDEX IF NOT EXISTS alert_rules_kind_enabled_idx ON alert_rules (kind, enabled);
CREATE INDEX IF NOT EXISTS maintenance_windows_time_agent_idx ON maintenance_windows (starts_at, ends_at, agent_id);
CREATE INDEX IF NOT EXISTS alert_incidents_opened_idx ON alert_incidents (opened_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS alert_events_incident_time_idx ON alert_events (incident_id, occurred_at, id);

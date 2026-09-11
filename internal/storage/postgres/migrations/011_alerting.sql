CREATE TABLE IF NOT EXISTS alert_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('metric', 'reachability')),
    metric TEXT NOT NULL CHECK (metric IN ('', 'cpu', 'memory', 'disk')),
    threshold DOUBLE PRECISION NOT NULL,
    stale_after_seconds INTEGER NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('warning', 'critical')),
    enabled BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS maintenance_windows (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (starts_at < ends_at)
);

CREATE TABLE IF NOT EXISTS alert_incidents (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    rule_name TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('warning', 'critical')),
    status TEXT NOT NULL CHECK (status IN ('open', 'acknowledged', 'resolved')),
    message TEXT NOT NULL,
    latest_value DOUBLE PRECISION NOT NULL,
    opened_at TIMESTAMPTZ NOT NULL,
    acknowledged_at TIMESTAMPTZ,
    acknowledged_by TEXT NOT NULL DEFAULT '',
    resolved_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS alert_incidents_active_rule_agent_idx
    ON alert_incidents (rule_id, agent_id) WHERE status IN ('open', 'acknowledged');

CREATE TABLE IF NOT EXISTS alert_events (
    id TEXT PRIMARY KEY,
    incident_id TEXT NOT NULL REFERENCES alert_incidents(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('opened', 'acknowledged', 'resolved')),
    actor TEXT NOT NULL,
    message TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL
);

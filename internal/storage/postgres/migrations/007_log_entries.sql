CREATE TABLE IF NOT EXISTS log_entries (
    id TEXT NOT NULL,
    agent_id TEXT NOT NULL REFERENCES hosts(agent_id) ON DELETE CASCADE,
    occurred_at TIMESTAMPTZ NOT NULL,
    collector TEXT NOT NULL CHECK (collector IN ('journald', 'file', 'windows_event')),
    source TEXT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('debug', 'info', 'warn', 'error', 'critical')),
    message TEXT NOT NULL,
    PRIMARY KEY (id, occurred_at)
);

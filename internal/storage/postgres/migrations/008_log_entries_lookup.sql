CREATE INDEX IF NOT EXISTS log_entries_agent_occurred_idx
    ON log_entries (agent_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS log_entries_agent_severity_occurred_idx
    ON log_entries (agent_id, severity, occurred_at DESC);

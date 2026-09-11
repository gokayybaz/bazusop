CREATE TABLE IF NOT EXISTS services (
    agent_id TEXT NOT NULL REFERENCES hosts(agent_id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('running', 'stopped', 'failed', 'unknown')),
    startup_type TEXT NOT NULL CHECK (startup_type IN ('automatic', 'manual', 'disabled', 'unknown')),
    observed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (agent_id, name)
);

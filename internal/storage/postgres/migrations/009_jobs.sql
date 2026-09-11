CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES hosts(agent_id) ON DELETE CASCADE,
    action TEXT NOT NULL CHECK (action IN ('service.restart', 'host.reboot')),
    target TEXT NOT NULL,
    approved_by TEXT NOT NULL,
    reason TEXT NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    last_sequence INTEGER NOT NULL CHECK (last_sequence >= 0),
    signature TEXT NOT NULL,
    signing_public_key TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS job_events (
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 0),
    type TEXT NOT NULL CHECK (type IN ('approved', 'claimed', 'output', 'succeeded', 'failed')),
    message TEXT NOT NULL,
    actor TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (job_id, sequence)
);

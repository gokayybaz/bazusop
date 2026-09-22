CREATE TABLE IF NOT EXISTS activity_events (
    id BIGSERIAL PRIMARY KEY,
    organization_id TEXT NOT NULL,
    site_id TEXT NOT NULL,
    source TEXT NOT NULL,
    reference_id TEXT NOT NULL DEFAULT '',
    agent_id TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    actor TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS activity_events_site_time_idx
    ON activity_events (organization_id, site_id, occurred_at DESC);

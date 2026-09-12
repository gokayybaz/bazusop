CREATE TABLE IF NOT EXISTS cloud_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('aws', 'azure', 'gcp')),
    external_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'connected')),
    last_sync_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (provider, external_id)
);

CREATE TABLE IF NOT EXISTS cloud_instances (
    account_id TEXT NOT NULL REFERENCES cloud_accounts(id) ON DELETE CASCADE,
    provider_instance_id TEXT NOT NULL,
    name TEXT NOT NULL,
    region TEXT NOT NULL,
    zone TEXT NOT NULL,
    state TEXT NOT NULL,
    os_family TEXT NOT NULL CHECK (os_family IN ('linux', 'windows', 'unknown')),
    private_ips TEXT[] NOT NULL DEFAULT '{}',
    public_ips TEXT[] NOT NULL DEFAULT '{}',
    agent_id_hint TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}',
    agent_id TEXT REFERENCES hosts(agent_id) ON DELETE SET NULL,
    candidate_agent_id TEXT REFERENCES hosts(agent_id) ON DELETE SET NULL,
    match_status TEXT NOT NULL CHECK (match_status IN ('verified', 'candidate', 'unmatched')),
    match_reason TEXT NOT NULL,
    discovered_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (account_id, provider_instance_id)
);

CREATE INDEX IF NOT EXISTS cloud_instances_match_idx
    ON cloud_instances (match_status, provider_instance_id);

CREATE TABLE IF NOT EXISTS hosts (
    agent_id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    os_family TEXT NOT NULL CHECK (os_family IN ('linux', 'windows')),
    os_name TEXT NOT NULL,
    os_version TEXT NOT NULL,
    architecture TEXT NOT NULL,
    kernel_version TEXT NOT NULL,
    cpu_cores INTEGER NOT NULL CHECK (cpu_cores > 0),
    memory_bytes BIGINT NOT NULL CHECK (memory_bytes > 0),
    ip_addresses TEXT[] NOT NULL DEFAULT '{}',
    agent_version TEXT NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS telemetry_samples (
    agent_id TEXT NOT NULL REFERENCES hosts(agent_id) ON DELETE CASCADE,
    recorded_at TIMESTAMPTZ NOT NULL,
    cpu_percent DOUBLE PRECISION NOT NULL CHECK (cpu_percent BETWEEN 0 AND 100),
    memory_percent DOUBLE PRECISION NOT NULL CHECK (memory_percent BETWEEN 0 AND 100),
    disk_percent DOUBLE PRECISION NOT NULL CHECK (disk_percent BETWEEN 0 AND 100),
    network_rx_bytes BIGINT NOT NULL CHECK (network_rx_bytes >= 0),
    network_tx_bytes BIGINT NOT NULL CHECK (network_tx_bytes >= 0),
    PRIMARY KEY (agent_id, recorded_at)
);

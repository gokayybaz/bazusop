CREATE INDEX IF NOT EXISTS telemetry_samples_agent_time_idx
    ON telemetry_samples (agent_id, recorded_at DESC);

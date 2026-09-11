CREATE INDEX IF NOT EXISTS jobs_agent_status_requested_idx
    ON jobs (agent_id, status, requested_at, id);

CREATE INDEX IF NOT EXISTS job_events_job_occurred_idx
    ON job_events (job_id, occurred_at, sequence);

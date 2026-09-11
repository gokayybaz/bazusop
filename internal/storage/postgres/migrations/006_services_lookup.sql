CREATE INDEX IF NOT EXISTS services_agent_state_name_idx
    ON services (agent_id, state, name);

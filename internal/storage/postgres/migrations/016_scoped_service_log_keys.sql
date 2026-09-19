-- Historical snapshots and stable log IDs remain independent after an agent
-- moves. Preserve occurred_at in the log key for Timescale hypertables.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'services'::regclass AND conname = 'services_pkey'
          AND pg_get_constraintdef(oid) = 'PRIMARY KEY (agent_id, name)'
    ) THEN
        ALTER TABLE services DROP CONSTRAINT services_pkey;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'services'::regclass AND conname = 'services_pkey'
    ) THEN
        ALTER TABLE services ADD CONSTRAINT services_pkey
            PRIMARY KEY (organization_id, site_id, agent_id, name);
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'log_entries'::regclass AND conname = 'log_entries_pkey'
          AND pg_get_constraintdef(oid) = 'PRIMARY KEY (id, occurred_at)'
    ) THEN
        ALTER TABLE log_entries DROP CONSTRAINT log_entries_pkey;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'log_entries'::regclass AND conname = 'log_entries_pkey'
    ) THEN
        ALTER TABLE log_entries ADD CONSTRAINT log_entries_pkey
            PRIMARY KEY (organization_id, site_id, id, occurred_at);
    END IF;
END
$$;

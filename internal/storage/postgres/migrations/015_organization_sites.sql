-- The migration runner executes this file in one transaction.
CREATE TABLE IF NOT EXISTS organizations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK (name <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sites (
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL,
    name TEXT NOT NULL CHECK (name <> ''),
    slug TEXT NOT NULL CHECK (slug <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sites_organization_fk FOREIGN KEY (organization_id)
        REFERENCES organizations(id) ON DELETE RESTRICT,
    CONSTRAINT sites_organization_id_key UNIQUE (organization_id, id),
    CONSTRAINT sites_organization_slug_key UNIQUE (organization_id, slug)
);

INSERT INTO organizations (id, name) VALUES ('org_default', 'bazUSOP')
ON CONFLICT (id) DO NOTHING;
INSERT INTO sites (id, organization_id, name, slug)
VALUES ('site_default', 'org_default', 'Varsayılan', 'varsayilan')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL,
    site_id TEXT NOT NULL,
    enrolled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    moved_at TIMESTAMPTZ,
    CONSTRAINT agents_site_fk FOREIGN KEY (organization_id, site_id)
        REFERENCES sites(organization_id, id) ON DELETE RESTRICT
);

INSERT INTO agents (id, organization_id, site_id, enrolled_at)
SELECT agent_id, 'org_default', 'site_default', first_seen_at FROM hosts
ON CONFLICT (id) DO NOTHING;

-- Add nullable columns before touching legacy rows. Preserve scope on replay.
ALTER TABLE hosts
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE enrollment_tokens
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE telemetry_samples
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE services
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE log_entries
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE jobs
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE job_events
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE alert_rules
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE maintenance_windows
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE alert_incidents
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE alert_events
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE cloud_accounts
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE cloud_instances
    ADD COLUMN IF NOT EXISTS organization_id TEXT,
    ADD COLUMN IF NOT EXISTS site_id TEXT;
ALTER TABLE enrollment_tokens
    ADD COLUMN IF NOT EXISTS consumed_by_agent_id TEXT;

UPDATE hosts
SET organization_id = COALESCE(organization_id, 'org_default'),
    site_id = COALESCE(site_id, 'site_default')
WHERE organization_id IS NULL OR site_id IS NULL;

UPDATE enrollment_tokens
SET organization_id = COALESCE(organization_id, 'org_default'),
    site_id = COALESCE(site_id, 'site_default')
WHERE organization_id IS NULL OR site_id IS NULL;

UPDATE telemetry_samples
SET organization_id = COALESCE(organization_id, 'org_default'),
    site_id = COALESCE(site_id, 'site_default')
WHERE organization_id IS NULL OR site_id IS NULL;

UPDATE services
SET organization_id = COALESCE(organization_id, 'org_default'),
    site_id = COALESCE(site_id, 'site_default')
WHERE organization_id IS NULL OR site_id IS NULL;

UPDATE log_entries
SET organization_id = COALESCE(organization_id, 'org_default'),
    site_id = COALESCE(site_id, 'site_default')
WHERE organization_id IS NULL OR site_id IS NULL;

UPDATE jobs
SET organization_id = COALESCE(organization_id, 'org_default'),
    site_id = COALESCE(site_id, 'site_default')
WHERE organization_id IS NULL OR site_id IS NULL;

UPDATE alert_rules
SET organization_id = COALESCE(organization_id, 'org_default'),
    site_id = COALESCE(site_id, 'site_default')
WHERE organization_id IS NULL OR site_id IS NULL;

UPDATE maintenance_windows
SET organization_id = COALESCE(organization_id, 'org_default'),
    site_id = COALESCE(site_id, 'site_default')
WHERE organization_id IS NULL OR site_id IS NULL;

UPDATE alert_incidents
SET organization_id = COALESCE(organization_id, 'org_default'),
    site_id = COALESCE(site_id, 'site_default')
WHERE organization_id IS NULL OR site_id IS NULL;

UPDATE cloud_accounts
SET organization_id = COALESCE(organization_id, 'org_default'),
    site_id = COALESCE(site_id, 'site_default')
WHERE organization_id IS NULL OR site_id IS NULL;

-- Historical children inherit their parent's scope, including on replay.
UPDATE job_events AS child
SET organization_id = parent.organization_id,
    site_id = parent.site_id
FROM jobs AS parent
WHERE child.job_id = parent.id
  AND (child.organization_id IS NULL OR child.site_id IS NULL);

UPDATE alert_events AS child
SET organization_id = parent.organization_id,
    site_id = parent.site_id
FROM alert_incidents AS parent
WHERE child.incident_id = parent.id
  AND (child.organization_id IS NULL OR child.site_id IS NULL);

UPDATE cloud_instances AS child
SET organization_id = parent.organization_id,
    site_id = parent.site_id
FROM cloud_accounts AS parent
WHERE child.account_id = parent.id
  AND (child.organization_id IS NULL OR child.site_id IS NULL);

-- Validate every backfill before installing mandatory scope constraints.
-- Catalog checks make repeated execution safe without replacing existing data.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM hosts WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: hosts';
    END IF;
    IF EXISTS (SELECT 1 FROM enrollment_tokens WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: enrollment_tokens';
    END IF;
    IF EXISTS (SELECT 1 FROM telemetry_samples WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: telemetry_samples';
    END IF;
    IF EXISTS (SELECT 1 FROM services WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: services';
    END IF;
    IF EXISTS (SELECT 1 FROM log_entries WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: log_entries';
    END IF;
    IF EXISTS (SELECT 1 FROM jobs WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: jobs';
    END IF;
    IF EXISTS (SELECT 1 FROM job_events WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: job_events';
    END IF;
    IF EXISTS (SELECT 1 FROM alert_rules WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: alert_rules';
    END IF;
    IF EXISTS (SELECT 1 FROM maintenance_windows WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: maintenance_windows';
    END IF;
    IF EXISTS (SELECT 1 FROM alert_incidents WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: alert_incidents';
    END IF;
    IF EXISTS (SELECT 1 FROM alert_events WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: alert_events';
    END IF;
    IF EXISTS (SELECT 1 FROM cloud_accounts WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: cloud_accounts';
    END IF;
    IF EXISTS (SELECT 1 FROM cloud_instances WHERE organization_id IS NULL OR site_id IS NULL) THEN
        RAISE EXCEPTION 'site scope backfill incomplete: cloud_instances';
    END IF;

    ALTER TABLE hosts
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'hosts'::regclass AND conname = 'hosts_site_fk'
    ) THEN
        ALTER TABLE hosts ADD CONSTRAINT hosts_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE enrollment_tokens
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'enrollment_tokens'::regclass AND conname = 'enrollment_tokens_site_fk'
    ) THEN
        ALTER TABLE enrollment_tokens ADD CONSTRAINT enrollment_tokens_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE telemetry_samples
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'telemetry_samples'::regclass AND conname = 'telemetry_samples_site_fk'
    ) THEN
        ALTER TABLE telemetry_samples ADD CONSTRAINT telemetry_samples_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE services
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'services'::regclass AND conname = 'services_site_fk'
    ) THEN
        ALTER TABLE services ADD CONSTRAINT services_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE log_entries
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'log_entries'::regclass AND conname = 'log_entries_site_fk'
    ) THEN
        ALTER TABLE log_entries ADD CONSTRAINT log_entries_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE jobs
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'jobs'::regclass AND conname = 'jobs_site_fk'
    ) THEN
        ALTER TABLE jobs ADD CONSTRAINT jobs_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE job_events
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'job_events'::regclass AND conname = 'job_events_site_fk'
    ) THEN
        ALTER TABLE job_events ADD CONSTRAINT job_events_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE alert_rules
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'alert_rules'::regclass AND conname = 'alert_rules_site_fk'
    ) THEN
        ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE maintenance_windows
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'maintenance_windows'::regclass AND conname = 'maintenance_windows_site_fk'
    ) THEN
        ALTER TABLE maintenance_windows ADD CONSTRAINT maintenance_windows_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE alert_incidents
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'alert_incidents'::regclass AND conname = 'alert_incidents_site_fk'
    ) THEN
        ALTER TABLE alert_incidents ADD CONSTRAINT alert_incidents_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE alert_events
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'alert_events'::regclass AND conname = 'alert_events_site_fk'
    ) THEN
        ALTER TABLE alert_events ADD CONSTRAINT alert_events_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE cloud_accounts
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'cloud_accounts'::regclass AND conname = 'cloud_accounts_site_fk'
    ) THEN
        ALTER TABLE cloud_accounts ADD CONSTRAINT cloud_accounts_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    ALTER TABLE cloud_instances
        ALTER COLUMN organization_id SET NOT NULL,
        ALTER COLUMN site_id SET NOT NULL;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'cloud_instances'::regclass AND conname = 'cloud_instances_site_fk'
    ) THEN
        ALTER TABLE cloud_instances ADD CONSTRAINT cloud_instances_site_fk
            FOREIGN KEY (organization_id, site_id)
            REFERENCES sites(organization_id, id) ON DELETE RESTRICT;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'hosts'::regclass AND conname = 'hosts_agent_fk'
    ) THEN
        ALTER TABLE hosts ADD CONSTRAINT hosts_agent_fk
            FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE RESTRICT;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'enrollment_tokens'::regclass AND conname = 'enrollment_tokens_consumed_agent_fk'
    ) THEN
        ALTER TABLE enrollment_tokens ADD CONSTRAINT enrollment_tokens_consumed_agent_fk
            FOREIGN KEY (consumed_by_agent_id) REFERENCES agents(id) ON DELETE RESTRICT;
    END IF;

    -- Keep the partition time column in the key for Timescale compatibility.
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'telemetry_samples'::regclass
          AND conname = 'telemetry_samples_pkey'
          AND pg_get_constraintdef(oid) = 'PRIMARY KEY (agent_id, recorded_at)'
    ) THEN
        ALTER TABLE telemetry_samples DROP CONSTRAINT telemetry_samples_pkey;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'telemetry_samples'::regclass AND conname = 'telemetry_samples_pkey'
    ) THEN
        ALTER TABLE telemetry_samples ADD CONSTRAINT telemetry_samples_pkey
            PRIMARY KEY (organization_id, site_id, agent_id, recorded_at);
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'cloud_accounts'::regclass AND conname = 'cloud_accounts_provider_external_id_key'
    ) THEN
        ALTER TABLE cloud_accounts DROP CONSTRAINT cloud_accounts_provider_external_id_key;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'cloud_accounts'::regclass AND conname = 'cloud_accounts_site_provider_external_key'
    ) THEN
        ALTER TABLE cloud_accounts ADD CONSTRAINT cloud_accounts_site_provider_external_key
            UNIQUE (organization_id, site_id, provider, external_id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS agents_site_idx
    ON agents (organization_id, site_id, id);
CREATE INDEX IF NOT EXISTS hosts_site_last_seen_idx
    ON hosts (organization_id, site_id, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS enrollment_tokens_site_created_idx
    ON enrollment_tokens (organization_id, site_id, created_at DESC);
CREATE INDEX IF NOT EXISTS telemetry_samples_site_agent_time_idx
    ON telemetry_samples (organization_id, site_id, agent_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS services_site_agent_state_name_idx
    ON services (organization_id, site_id, agent_id, state, name);
CREATE INDEX IF NOT EXISTS log_entries_site_agent_occurred_idx
    ON log_entries (organization_id, site_id, agent_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS log_entries_site_agent_severity_occurred_idx
    ON log_entries (organization_id, site_id, agent_id, severity, occurred_at DESC);
CREATE INDEX IF NOT EXISTS jobs_site_agent_status_requested_idx
    ON jobs (organization_id, site_id, agent_id, status, requested_at, id);
CREATE INDEX IF NOT EXISTS job_events_site_job_occurred_idx
    ON job_events (organization_id, site_id, job_id, occurred_at, sequence);
CREATE INDEX IF NOT EXISTS alert_rules_site_kind_enabled_idx
    ON alert_rules (organization_id, site_id, kind, enabled);
CREATE INDEX IF NOT EXISTS maintenance_windows_site_time_agent_idx
    ON maintenance_windows (organization_id, site_id, starts_at, ends_at, agent_id);
CREATE INDEX IF NOT EXISTS alert_incidents_site_opened_idx
    ON alert_incidents (organization_id, site_id, opened_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS alert_events_site_incident_time_idx
    ON alert_events (organization_id, site_id, incident_id, occurred_at, id);
CREATE INDEX IF NOT EXISTS cloud_accounts_site_created_idx
    ON cloud_accounts (organization_id, site_id, created_at, id);
CREATE INDEX IF NOT EXISTS cloud_instances_site_match_idx
    ON cloud_instances (organization_id, site_id, match_status, provider_instance_id);

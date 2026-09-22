CREATE TABLE IF NOT EXISTS service_accounts (
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL,
    site_id TEXT NOT NULL,
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,
    CONSTRAINT service_accounts_org_fk FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    CONSTRAINT service_accounts_site_fk FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS service_accounts_site_idx ON service_accounts (site_id);

CREATE TABLE IF NOT EXISTS service_account_tokens (
    id TEXT PRIMARY KEY,
    service_account_id TEXT NOT NULL,
    secret_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    last_used_ip TEXT,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT service_account_tokens_account_fk FOREIGN KEY (service_account_id) REFERENCES service_accounts(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS service_account_tokens_account_idx ON service_account_tokens (service_account_id);

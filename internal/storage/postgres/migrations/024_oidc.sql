-- OIDC users have no local password; NOT NULL was correct before this
-- spike (every user was local) and is now relaxed.
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_subject TEXT NOT NULL DEFAULT '';

-- Partial (oidc_subject <> '') so multiple local users, which all have
-- empty oidc_issuer/oidc_subject, never collide on this uniqueness rule.
CREATE UNIQUE INDEX IF NOT EXISTS users_oidc_identity_key
    ON users (organization_id, oidc_issuer, oidc_subject)
    WHERE oidc_subject <> '';

ALTER TABLE invites ADD COLUMN IF NOT EXISTS identity_type TEXT NOT NULL DEFAULT 'local';

CREATE TABLE IF NOT EXISTS oidc_configurations (
    organization_id TEXT PRIMARY KEY,
    discovery_url TEXT NOT NULL,
    issuer TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret_encrypted BYTEA NOT NULL,
    redirect_url TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by TEXT NOT NULL,
    CONSTRAINT oidc_configurations_org_fk FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT
);

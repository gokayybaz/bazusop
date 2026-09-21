CREATE TABLE IF NOT EXISTS site_memberships (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    organization_id TEXT NOT NULL,
    site_id TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT site_memberships_user_fk FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT site_memberships_site_fk FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE RESTRICT,
    CONSTRAINT site_memberships_user_site_key UNIQUE (user_id, site_id)
);

CREATE INDEX IF NOT EXISTS site_memberships_user_idx ON site_memberships (user_id);
CREATE INDEX IF NOT EXISTS site_memberships_site_idx ON site_memberships (site_id);

CREATE TABLE IF NOT EXISTS invite_site_roles (
    invite_id TEXT NOT NULL,
    site_id TEXT NOT NULL,
    role TEXT NOT NULL,
    CONSTRAINT invite_site_roles_invite_fk FOREIGN KEY (invite_id) REFERENCES invites(id) ON DELETE CASCADE,
    PRIMARY KEY (invite_id, site_id)
);

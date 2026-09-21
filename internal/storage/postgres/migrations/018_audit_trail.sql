CREATE TABLE IF NOT EXISTS audit_events (
    event_id TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    correlation_id TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL DEFAULT '',
    session_or_token_id TEXT NOT NULL DEFAULT '',
    organization_id TEXT NOT NULL,
    site_id TEXT NOT NULL,
    action TEXT NOT NULL,
    permission TEXT NOT NULL DEFAULT '',
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL,
    error_code TEXT NOT NULL DEFAULT '',
    source_ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    change_summary TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (event_id, occurred_at)
    -- Deliberately no FK to sites: audit_events is append-only (see the
    -- triggers below) and can never be deleted, so a hard FK with ON DELETE
    -- RESTRICT would make every organization/site that ever appeared in a
    -- request permanently undeletable. The historical record must outlive
    -- the site it describes.
);

CREATE INDEX IF NOT EXISTS audit_events_site_time_idx
    ON audit_events (organization_id, site_id, occurred_at DESC);

-- Append-only: reject any UPDATE or DELETE at the database level so a bug or
-- a compromised app-layer credential cannot rewrite history through this
-- table, independent of what the product API happens to expose.
CREATE OR REPLACE FUNCTION audit_events_reject_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only: % is not permitted', TG_OP;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_events_no_update ON audit_events;
CREATE TRIGGER audit_events_no_update
    BEFORE UPDATE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_reject_mutation();

DROP TRIGGER IF EXISTS audit_events_no_delete ON audit_events;
CREATE TRIGGER audit_events_no_delete
    BEFORE DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_reject_mutation();

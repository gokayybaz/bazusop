CREATE TABLE enrollment_authority (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton = TRUE),
    certificate_pem TEXT NOT NULL CHECK (certificate_pem <> ''),
    private_key_pem TEXT NOT NULL CHECK (private_key_pem <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE enrollment_tokens (
    token_hash BYTEA PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    consumed_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

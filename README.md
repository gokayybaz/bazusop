# bazUSOP

**Unified Server Operations Platform**

bazUSOP is an agent–hub platform for monitoring and operating Linux and
Windows servers from one control plane.

## Development

```bash
make test
make build
BAZUSOP_ENROLLMENT_TOKEN="replace-with-a-one-time-secret" ./bin/bazusop-hub
```

The React application is built first and embedded in the Go hub binary.

Set `BAZUSOP_TLS_CERT_FILE` and `BAZUSOP_TLS_KEY_FILE` together to serve HTTPS.
The hub then requests and verifies enrolled client certificates for mTLS renewal;
TLS 1.3 is the minimum supported version.

Set `DATABASE_URL` to a PostgreSQL connection string to persist normalized host
inventory. The hub applies ordered embedded migrations at startup. Without this
variable, a process-local in-memory store is used for development.

## Agent enrollment

`POST /api/v1/agents/enroll` exchanges a one-time bootstrap token and a signed
PKCS#10 CSR for a 24-hour client certificate. The certificate carries a stable
SPIFFE agent ID and is renewed through `POST /api/v1/agents/renew`, which only
accepts the enrolled mTLS client identity. Linux and Windows are accepted as
normalized operating-system families. Remote enrollment over plaintext HTTP is
rejected; loopback HTTP remains available for local development.

The current spike keeps the token-consumption state and certificate authority in
the running hub process. Durable PostgreSQL-backed enrollment state and operator
token issuance are the next hardening step before production use.

Authenticated agents report host facts with `PUT /api/v1/agents/inventory`.
Operators read the normalized fleet from `GET /api/v1/instances`.

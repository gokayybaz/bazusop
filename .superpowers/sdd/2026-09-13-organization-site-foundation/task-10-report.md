# Task 10 report: PostgreSQL isolation, migration backfill, and history preservation

## Changes

- Added `internal/storage/postgres/tenancy_integration_test.go` with a disposable isolated-schema harness gated by `BAZUSOP_TEST_DATABASE_URL`.
- The harness creates a cryptographically random lowercase schema, validates it against `^[a-z0-9_]+$`, sets the connection `search_path` to that schema, and drops only that quoted schema with `CASCADE` during cleanup. It does not clear or reuse `public` or any caller schema.
- Added a real PostgreSQL migration replay test covering migrations 001-014, legacy rows in every pre-015 scoped resource table, migration 015 replay, and migrations 016-017. It verifies default organization/site and agent identity preservation plus non-null scope backfill.
- Added a real PostgreSQL agent move test covering persistent enrollment/mTLS resolution, current host placement, site-A telemetry/log/job/incident history, site-B future writes, and wrong-site/wrong-organization no-row/domain-not-found behavior.
- Updated `enrollment_state_integration_test.go` to use isolated schemas and removed table-wide cleanup deletes.

## Verification

Focused real PostgreSQL suite, run through the local disposable TimescaleDB container network:

```text
TestPostgresEnrollmentSiteIsolationAndAtomicConsumption: PASS
TestOrganizationSiteMigrationBackfillsExistingData: PASS
TestPostgresAgentMovePreservesScopedHistoryAndRefreshesIdentity: PASS
```

The package-wide PostgreSQL run was also executed against a newly created disposable TimescaleDB database after the cloud integration fixture was corrected to use non-nil empty IP slices. The complete `internal/storage/postgres` package passed, including cloud isolation, migration replay, enrollment, and move/history acceptance tests.

Other checks:

- `go vet ./...`: PASS
- `go test ./...`: PASS without `BAZUSOP_TEST_DATABASE_URL` (integration tests skip)
- `npm --prefix web test -- --run`: PASS (12 tests)
- `npm --prefix web run build`: PASS
- `git diff --check`: PASS

The real PostgreSQL package-wide result is verified against the disposable database; the database was dropped after the run.

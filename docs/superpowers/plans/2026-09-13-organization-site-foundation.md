# Organization and Site Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver Spike 11.1 by assigning every existing and new operational resource to one organization and one site, binding enrolled agents to the site carried by their enrollment token, and making every service/store read and write explicitly site-scoped.

**Architecture:** Add a small `internal/tenancy` domain containing validated organization/site scope and agent identity values. PostgreSQL migration `015` creates the single-organization/multi-site schema, backfills all existing records into deterministic default organization/site rows, and records each agent's current site separately from historical records. Enrollment resolves the current agent scope after mTLS verification. Application services require a trusted scope or resolved agent identity; handlers never accept site identity from query/body input. During this spike, existing human-facing routes are pinned to the default scope until sessions and RBAC supply actor scope in Spikes 11.3–11.5.

**Tech Stack:** Go 1.26.4, standard-library `net/http` routing, PostgreSQL with pgx v5, embedded SQL migrations, in-memory test stores, Go `testing`.

**Spec:** `docs/superpowers/specs/2026-09-13-enterprise-rbac-identity-audit-design.md` (organization/site model, migration, enrollment binding, historical-site preservation, delivery Spike 11.1).

**Global Constraints:**

- Keep the deployment single-organization; do not implement tenant switching.
- Use stable IDs `org_default` and `site_default` in both Go and SQL so bootstrap, memory mode, and upgrade mode agree.
- Never accept `organization_id` or `site_id` from an agent report body, path, or query string. Agent scope comes only from the consumed enrollment token and later from the persistent agent record.
- Existing `BAZUSOP_ENROLLMENT_TOKEN` remains a temporary compatibility bootstrap token for the default site. Site-specific token creation UI/API belongs to the later enrollment/RBAC slice.
- Existing human-facing APIs are temporarily scoped to `tenancy.DefaultScope()`. Do not expose create/move-site HTTP routes before platform-admin authorization exists.
- Resource structs store `OrganizationID` and `SiteID` explicitly. Historical telemetry, logs, jobs, job events, incidents, alert events, and cloud discovery snapshots retain the scope present at creation even after an agent moves.
- Every PostgreSQL query must predicate on both `organization_id` and `site_id`; an `agent_id` predicate alone is not sufficient.
- Scope mismatch must behave like absence at the service/store boundary (`ErrNotFound` or an empty list), preparing the HTTP `404` behavior for Spike 11.5.
- Audit/activity are not implemented in this spike; their transaction boundary is Spike 11.2. Do not add unaudited privileged site-management routes as a shortcut.

---

## Planned file map

**Create**

- `internal/tenancy/tenancy.go` — validated `Scope`, `Organization`, `Site`, and `Agent` value types plus default IDs.
- `internal/tenancy/tenancy_test.go` — validation and default-scope tests.
- `internal/storage/postgres/migrations/015_organization_sites.sql` — organization/site/agent tables and complete default-site backfill.
- `internal/storage/postgres/tenancy_integration_test.go` — real PostgreSQL scope-isolation, enrollment binding, and move/history tests.

**Modify**

- `internal/enrollment/authority.go`, `internal/enrollment/authority_test.go` — site-scoped token consumption and context-aware agent authentication.
- `internal/inventory/inventory.go`, `internal/inventory/memory.go`, `internal/inventory/inventory_test.go` — scoped hosts.
- `internal/telemetry/telemetry.go`, `internal/telemetry/telemetry_test.go` — scoped telemetry history.
- `internal/serviceinventory/serviceinventory.go`, `internal/serviceinventory/serviceinventory_test.go` — scoped service snapshots.
- `internal/logstream/logstream.go`, `internal/logstream/logstream_test.go` — scoped history and live subscribers.
- `internal/jobs/jobs.go`, `internal/jobs/jobs_test.go` — scoped jobs and immutable event history.
- `internal/alerting/alerting.go`, `internal/alerting/alerting_test.go` — scoped rules, windows, incidents, and events.
- `internal/cloudinventory/cloudinventory.go`, `internal/cloudinventory/cloudinventory_test.go` — scoped accounts, discovery, and host matching.
- `internal/storage/postgres/store.go`, `internal/storage/postgres/store_test.go`, `internal/storage/postgres/enrollment_state_integration_test.go`, `internal/storage/postgres/live_logs_integration_test.go` — implement all revised interfaces and SQL predicates.
- `internal/server/server.go` and all `internal/server/*_test.go` feature tests — resolved agent scope on mTLS routes and default scope on transitional human routes.
- `cmd/bazusop-hub/main.go` — create the default scope once, pass it to persistent enrollment and HTTP wiring, and evaluate alerts per host scope.

No web bundle changes are planned; the API response additions are backward-compatible fields and no site chooser is exposed before RBAC.

---

### Task 1: Introduce validated tenancy primitives

**Files:**

- Create: `internal/tenancy/tenancy.go`
- Create: `internal/tenancy/tenancy_test.go`

**Interfaces:**

- Produces `tenancy.DefaultOrganizationID`, `tenancy.DefaultSiteID`, `tenancy.DefaultScope()`.
- Produces `tenancy.Scope`, consumed by every service/store in Tasks 4–8.
- Produces `tenancy.Agent`, consumed by enrollment and agent-only handlers in Tasks 3 and 9.

- [ ] **Step 1: Write failing value-object tests**

```go
func TestDefaultScopeIsValid(t *testing.T) {
	scope := DefaultScope()
	if scope.OrganizationID != DefaultOrganizationID || scope.SiteID != DefaultSiteID {
		t.Fatalf("unexpected default scope: %#v", scope)
	}
	if err := scope.Validate(); err != nil {
		t.Fatalf("default scope must be valid: %v", err)
	}
}

func TestScopeRejectsMissingOrOversizedIdentifiers(t *testing.T) {
	for _, scope := range []Scope{
		{},
		{OrganizationID: DefaultOrganizationID},
		{OrganizationID: strings.Repeat("x", 129), SiteID: DefaultSiteID},
	} {
		if !errors.Is(scope.Validate(), ErrInvalidScope) {
			t.Fatalf("expected invalid scope for %#v", scope)
		}
	}
}

func TestAgentRequiresIDAndValidScope(t *testing.T) {
	agent := Agent{ID: "agent-1", OrganizationID: DefaultOrganizationID, SiteID: DefaultSiteID}
	if err := agent.Validate(); err != nil {
		t.Fatalf("valid agent rejected: %v", err)
	}
}
```

- [ ] **Step 2: Run the focused test and confirm RED**

Run: `go test ./internal/tenancy`

Expected: compile failure because package/types do not exist.

- [ ] **Step 3: Add the minimal tenancy values**

```go
package tenancy

import (
	"errors"
	"strings"
	"time"
)

const (
	DefaultOrganizationID = "org_default"
	DefaultSiteID         = "site_default"
)

var (
	ErrInvalidScope = errors.New("invalid tenancy scope")
	ErrNotFound     = errors.New("tenancy resource not found")
)

type Scope struct {
	OrganizationID string `json:"organization_id"`
	SiteID         string `json:"site_id"`
}

func DefaultScope() Scope {
	return Scope{OrganizationID: DefaultOrganizationID, SiteID: DefaultSiteID}
}

func (scope Scope) Validate() error {
	if strings.TrimSpace(scope.OrganizationID) == "" || strings.TrimSpace(scope.SiteID) == "" ||
		len(scope.OrganizationID) > 128 || len(scope.SiteID) > 128 {
		return ErrInvalidScope
	}
	return nil
}

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Site struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	CreatedAt      time.Time `json:"created_at"`
}

type Agent struct {
	ID             string `json:"agent_id"`
	OrganizationID string `json:"organization_id"`
	SiteID         string `json:"site_id"`
}

func (agent Agent) Scope() Scope {
	return Scope{OrganizationID: agent.OrganizationID, SiteID: agent.SiteID}
}

func (agent Agent) Validate() error {
	if strings.TrimSpace(agent.ID) == "" || len(agent.ID) > 128 {
		return ErrInvalidScope
	}
	return agent.Scope().Validate()
}
```

- [ ] **Step 4: Run GREEN and commit**

Run: `go test ./internal/tenancy`

Expected: PASS.

```bash
git add internal/tenancy
git commit -m "feat: add tenancy scope primitives"
```

---

### Task 2: Add the transactional default-site migration

**Files:**

- Create: `internal/storage/postgres/migrations/015_organization_sites.sql`
- Modify: `internal/storage/postgres/store_test.go:12-38`

**Interfaces:**

- Consumes stable IDs from Task 1 by matching their literal values in SQL.
- Produces `organizations`, `sites`, `agents`, and mandatory scope columns consumed by PostgreSQL methods in Tasks 3–8.

- [ ] **Step 1: Extend migration contract tests**

Update the count/range assertion to expect 15 migrations ending in `015_organization_sites.sql`. Add:

```go
func TestOrganizationSiteMigrationBackfillsEveryScopedTable(t *testing.T) {
	migration, err := migrationFiles.ReadFile("migrations/015_organization_sites.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(migration)
	for _, required := range []string{
		"org_default", "site_default", "CREATE TABLE IF NOT EXISTS organizations",
		"CREATE TABLE IF NOT EXISTS sites", "CREATE TABLE IF NOT EXISTS agents",
		"ALTER TABLE hosts", "ALTER TABLE enrollment_tokens",
		"ALTER TABLE telemetry_samples", "ALTER TABLE services", "ALTER TABLE log_entries",
		"ALTER TABLE jobs", "ALTER TABLE job_events", "ALTER TABLE alert_rules",
		"ALTER TABLE maintenance_windows", "ALTER TABLE alert_incidents",
		"ALTER TABLE alert_events", "ALTER TABLE cloud_accounts", "ALTER TABLE cloud_instances",
		"SET NOT NULL", "FOREIGN KEY (organization_id, site_id)",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("site migration must contain %q", required)
		}
	}
}
```

- [ ] **Step 2: Run RED**

Run: `go test ./internal/storage/postgres -run 'TestStorageMigrations|TestOrganizationSiteMigration'`

Expected: count/range failure and missing migration file.

- [ ] **Step 3: Implement migration 015**

The SQL must perform these operations in this order; the existing migration runner supplies the outer transaction:

```sql
CREATE TABLE IF NOT EXISTS organizations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK (name <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sites (
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    name TEXT NOT NULL CHECK (name <> ''),
    slug TEXT NOT NULL CHECK (slug <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, slug)
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
    FOREIGN KEY (organization_id, site_id)
        REFERENCES sites(organization_id, id) ON DELETE RESTRICT
);

INSERT INTO agents (id, organization_id, site_id, enrolled_at)
SELECT agent_id, 'org_default', 'site_default', first_seen_at FROM hosts
ON CONFLICT (id) DO NOTHING;
```

For each existing scoped table, add nullable `organization_id` and `site_id`, backfill both to the default IDs, then make both columns `NOT NULL` and add the composite site foreign key. Use explicit, unique constraint names. Apply this to:

```text
hosts, enrollment_tokens, telemetry_samples, services, log_entries,
jobs, job_events, alert_rules, maintenance_windows, alert_incidents,
alert_events, cloud_accounts, cloud_instances
```

Also:

- Add `consumed_by_agent_id TEXT REFERENCES agents(id)` to `enrollment_tokens`.
- Add `hosts.agent_id -> agents.id` as a foreign key after legacy hosts have been copied into `agents`.
- Set historical child scope from its parent where possible (`job_events <- jobs`, `alert_events <- alert_incidents`, `cloud_instances <- cloud_accounts`) instead of assuming a future multi-site row is default.
- Add scoped lookup indexes beginning with `(organization_id, site_id)` for hosts, services, logs, jobs, alert records, and cloud records.
- Replace `telemetry_samples_pkey` with `(organization_id, site_id, agent_id, recorded_at)` so a moved agent cannot overwrite old-site telemetry at the same timestamp.
- Drop the old global cloud-account uniqueness constraint and replace it with `UNIQUE (organization_id, site_id, provider, external_id)`.
- Add a final PostgreSQL `DO` block that checks each named table through `EXISTS (SELECT 1 FROM table WHERE organization_id IS NULL OR site_id IS NULL)` and raises `site scope backfill incomplete` before constraints are installed.
- Keep globally generated resource IDs (`jobs.id`, alert IDs, log IDs, cloud account IDs) globally unique; site predicates still remain mandatory on every lookup.

- [ ] **Step 4: Run GREEN and commit**

Run: `go test ./internal/storage/postgres -run 'TestStorageMigrations|TestOrganizationSiteMigration'`

Expected: PASS.

```bash
git add internal/storage/postgres/migrations/015_organization_sites.sql internal/storage/postgres/store_test.go
git commit -m "feat: migrate resources into default site"
```

---

### Task 3: Bind enrollment tokens and mTLS agents to site scope

**Files:**

- Modify: `internal/enrollment/authority.go:40-181,247-265`
- Modify: `internal/enrollment/authority_test.go:18-128`
- Modify: `internal/storage/postgres/store.go:115-199`
- Modify: `internal/storage/postgres/enrollment_state_integration_test.go:19-104`

**Interfaces:**

- Consumes `tenancy.Scope` and `tenancy.Agent` from Task 1.
- Consumes `agents`, `enrollment_tokens.organization_id/site_id/consumed_by_agent_id` from Task 2.
- Produces `Authority.AuthenticateContext`, used by agent handlers in Task 9.

- [ ] **Step 1: Add failing enrollment tests**

Update the fake state store contract and add tests proving the site is trusted from the token rather than the request:

```go
func TestEnrollmentBindsAgentToTokenScope(t *testing.T) {
	scope := tenancy.Scope{OrganizationID: "org_default", SiteID: "site_istanbul"}
	store := newFakeStateStore(scope)
	authority, err := enrollment.NewPersistentAuthority(t.Context(), "bootstrap-secret", scope, store)
	if err != nil { t.Fatal(err) }
	identity, err := authority.EnrollContext(t.Context(), validRequest(t, "bootstrap-secret"))
	if err != nil { t.Fatal(err) }
	agent, err := authority.AuthenticateContext(t.Context(), parseCertificate(t, identity.CertificatePEM))
	if err != nil { t.Fatal(err) }
	if agent.ID != identity.AgentID || agent.Scope() != scope {
		t.Fatalf("unexpected scoped agent: %#v", agent)
	}
}

func TestAuthenticationRejectsCertificateWithoutPersistentAgent(t *testing.T) {
	scope := tenancy.DefaultScope()
	store := newFakeStateStore(scope)
	authority, err := enrollment.NewPersistentAuthority(t.Context(), "bootstrap-secret", scope, store)
	if err != nil { t.Fatal(err) }
	identity, err := authority.EnrollContext(t.Context(), validRequest(t, "bootstrap-secret"))
	if err != nil { t.Fatal(err) }
	store.deleteAgent(identity.AgentID)
	_, err = authority.AuthenticateContext(t.Context(), parseCertificate(t, identity.CertificatePEM))
	if !errors.Is(err, enrollment.ErrInvalidIdentity) {
		t.Fatalf("expected invalid identity, got %v", err)
	}
}
```

Add `validRequest`, `newFakeStateStore`, and `deleteAgent` as concrete test helpers beside the existing fake state store; `validRequest` must build a signed CSR using the existing `newCSR` helper and must not add site fields to `enrollment.Request`.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/enrollment`

Expected: compile failures for the new constructor signature and `AuthenticateContext`.

- [ ] **Step 3: Revise the state-store contract and authority**

Use this exact interface shape:

```go
type StateStore interface {
	LoadOrCreateEnrollmentAuthority(context.Context, AuthorityState) (AuthorityState, error)
	RegisterEnrollmentToken(context.Context, [sha256.Size]byte, tenancy.Scope) error
	ConsumeEnrollmentToken(context.Context, [sha256.Size]byte, string) (tenancy.Agent, error)
	ResolveAgent(context.Context, string) (tenancy.Agent, error)
}
```

`NewPersistentAuthority` validates the supplied scope and registers the token with it. `NewAuthority` uses `tenancy.DefaultScope()` and a memory token store that keeps `map[string]tenancy.Agent`. During enrollment, issue the certificate, then atomically consume the token and insert the agent mapping. Add:

```go
func (authority *Authority) AuthenticateContext(ctx context.Context, peer *x509.Certificate) (tenancy.Agent, error) {
	agentID, err := authority.Authenticate(peer)
	if err != nil { return tenancy.Agent{}, err }
	agent, err := authority.tokens.ResolveAgent(ctx, agentID)
	if err != nil || agent.Validate() != nil {
		return tenancy.Agent{}, ErrInvalidIdentity
	}
	return agent, nil
}
```

Add `RenewContext`; make the existing `Renew` wrapper use `context.Background()`, and have the server call the context-aware method later.

- [ ] **Step 4: Implement the PostgreSQL enrollment transaction**

`RegisterEnrollmentToken` inserts the supplied scope. Its revocation update must be restricted to the same organization/site so creating a token for one site never revokes another site's token.

`ConsumeEnrollmentToken` must use one transaction and row lock:

```sql
SELECT organization_id, site_id, consumed_at IS NOT NULL, revoked_at IS NOT NULL
FROM enrollment_tokens
WHERE token_hash = $1
FOR UPDATE;

INSERT INTO agents (id, organization_id, site_id)
VALUES ($1, $2, $3);

UPDATE enrollment_tokens
SET consumed_at = now(), consumed_by_agent_id = $2
WHERE token_hash = $1;
```

Return `ErrInvalidToken` for absent/revoked rows and `ErrTokenConsumed` for consumed rows. Roll back the whole transaction if agent insertion or token update fails. `ResolveAgent` selects all three `tenancy.Agent` fields from `agents` and returns `tenancy.ErrNotFound` on `pgx.ErrNoRows`.

- [ ] **Step 5: Update and run unit/integration tests**

Run: `go test ./internal/enrollment ./internal/storage/postgres`

Expected: unit tests PASS; PostgreSQL tests PASS when `BAZUSOP_TEST_DATABASE_URL` is configured, otherwise skip only the integration cases.

- [ ] **Step 6: Commit**

```bash
git add internal/enrollment internal/storage/postgres/store.go internal/storage/postgres/enrollment_state_integration_test.go
git commit -m "feat: bind enrolled agents to sites"
```

---

### Task 4: Scope host inventory and define agent-move semantics

**Files:**

- Modify: `internal/inventory/inventory.go:37-99`
- Modify: `internal/inventory/memory.go:8-40`
- Modify: `internal/inventory/inventory_test.go`
- Modify: `internal/storage/postgres/store.go:201-280`

**Interfaces:**

- Consumes trusted `tenancy.Agent` on reports and `tenancy.Scope` on reads.
- Produces site-aware `inventory.Host`, consumed by alert and cloud services.

- [ ] **Step 1: Add failing cross-site inventory tests**

```go
func TestHostsAreIsolatedBySite(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := inventory.NewService(store)
	istanbul := tenancy.Agent{ID: "agent-1", OrganizationID: "org_default", SiteID: "site_istanbul"}
	ankara := tenancy.Agent{ID: "agent-2", OrganizationID: "org_default", SiteID: "site_ankara"}
	facts := func(hostname string) inventory.Facts {
		return inventory.Facts{Hostname: hostname, OSFamily: "linux", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024}
	}
	if err := service.Report(t.Context(), istanbul, facts("edge-1")); err != nil { t.Fatal(err) }
	if err := service.Report(t.Context(), ankara, facts("edge-2")); err != nil { t.Fatal(err) }
	hosts, err := service.List(t.Context(), istanbul.Scope())
	if err != nil { t.Fatal(err) }
	if len(hosts) != 1 || hosts[0].AgentID != istanbul.ID || hosts[0].SiteID != istanbul.SiteID {
		t.Fatalf("cross-site inventory leak: %#v", hosts)
	}
}
```

Add a validation test for an invalid/missing scope and a report test showing payload fields cannot override the supplied agent scope.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/inventory`

Expected: signature/field compile failures.

- [ ] **Step 3: Implement scoped host behavior**

Add these fields to `Host`:

```go
OrganizationID string `json:"organization_id"`
SiteID         string `json:"site_id"`
```

Change service/store contracts to:

```go
type Store interface {
	Upsert(context.Context, Host) error
	List(context.Context, tenancy.Scope) ([]Host, error)
}

func (service *Service) Report(ctx context.Context, agent tenancy.Agent, facts Facts) error
func (service *Service) List(ctx context.Context, scope tenancy.Scope) ([]Host, error)
```

Key the memory store by `organizationID + "\x00" + siteID + "\x00" + agentID`. PostgreSQL `INSERT`, `SELECT`, and `ON CONFLICT` update clauses include scope; the `SELECT` must contain:

```sql
WHERE organization_id = $1 AND site_id = $2
```

The PostgreSQL upsert must first lock the matching `agents` row and return `tenancy.ErrNotFound` when `(agent_id, organization_id, site_id)` is not the agent's current assignment. On a later platform-admin move, its transaction updates `agents` and current `hosts.organization_id/site_id`; it must not update historical tables. Add this helper for scheduled work:

```go
func (host Host) Scope() tenancy.Scope {
	return tenancy.Scope{OrganizationID: host.OrganizationID, SiteID: host.SiteID}
}
```

- [ ] **Step 4: Run GREEN and commit**

Run: `go test ./internal/inventory ./internal/storage/postgres`

Expected: PASS or integration skip as documented.

```bash
git add internal/inventory internal/storage/postgres/store.go
git commit -m "feat: scope host inventory by site"
```

---

### Task 5: Scope telemetry, service inventory, and log streams

**Files:**

- Modify: `internal/telemetry/telemetry.go:15-111`, `internal/telemetry/telemetry_test.go`
- Modify: `internal/serviceinventory/serviceinventory.go:44-190`, `internal/serviceinventory/serviceinventory_test.go`
- Modify: `internal/logstream/logstream.go:41-310`, `internal/logstream/logstream_test.go`
- Modify: `internal/storage/postgres/store.go:282-623`
- Modify: `internal/storage/postgres/live_logs_integration_test.go`

**Interfaces:**

- Consumes the agent's current scope for new ingest.
- Produces historical records whose stored scope, not current agent scope, controls reads.

- [ ] **Step 1: Add one cross-site isolation test per package**

Follow this telemetry pattern and mirror it for services and logs:

```go
func TestHistoryIsIsolatedByStoredSite(t *testing.T) {
	service := telemetry.NewService(telemetry.NewMemoryStore())
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	old := tenancy.Agent{ID: "agent-1", OrganizationID: "org_default", SiteID: "site_old"}
	moved := tenancy.Agent{ID: "agent-1", OrganizationID: "org_default", SiteID: "site_new"}
	sample := func(recordedAt time.Time) telemetry.Sample {
		return telemetry.Sample{RecordedAt: recordedAt, CPUPercent: 10, MemoryPercent: 20, DiskPercent: 30}
	}
	if err := service.Report(t.Context(), old, sample(at)); err != nil { t.Fatal(err) }
	if err := service.Report(t.Context(), moved, sample(at.Add(time.Minute))); err != nil { t.Fatal(err) }
	oldHistory, _ := service.History(t.Context(), old.Scope(), old.ID, at.Add(-time.Second), at.Add(time.Hour), 10)
	newHistory, _ := service.History(t.Context(), moved.Scope(), moved.ID, at.Add(-time.Second), at.Add(time.Hour), 10)
	if len(oldHistory) != 1 || len(newHistory) != 1 {
		t.Fatalf("history crossed sites: old=%#v new=%#v", oldHistory, newHistory)
	}
}
```

For services, report the same agent ID into two sites with different service names, list both scopes, and assert each result contains only its own name. For live logs, subscribe in both sites using the same `agent_id`, ingest into one site, receive the matching entry with a one-second test timeout, and assert the other channel has no ready value with a non-blocking `select`.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/telemetry ./internal/serviceinventory ./internal/logstream`

Expected: missing scope fields/signatures.

- [ ] **Step 3: Add scope fields and signatures**

Add `OrganizationID` and `SiteID` JSON fields to `telemetry.Sample`, `serviceinventory.Service`, and `logstream.Entry`. Use:

```go
func (service *Service) Report(ctx context.Context, agent tenancy.Agent, sample Sample) error
func (service *Service) History(ctx context.Context, scope tenancy.Scope, agentID string, from, to time.Time, limit int) ([]Sample, error)

func (manager *Manager) Report(ctx context.Context, agent tenancy.Agent, snapshot Snapshot) error
func (manager *Manager) List(ctx context.Context, scope tenancy.Scope, agentID string, filter Filter) ([]Service, error)

type Query struct {
	Scope tenancy.Scope
	AgentID   string
	From      time.Time
	To        time.Time
	Collector Collector
	Severity  Severity
	Source    string
	Text      string
	Limit     int
}
func (service *Service) Ingest(ctx context.Context, agent tenancy.Agent, batch Batch) error
func (service *Service) Subscribe(ctx context.Context, scope tenancy.Scope, agentID string) (<-chan Entry, error)
```

Extend `SharedLiveStore.SubscribeLogs` to accept `tenancy.Scope`. Key memory maps/subscribers by scope plus agent ID.

- [ ] **Step 4: Scope all PostgreSQL writes, reads, deletes, conflicts, and notifications**

Every statement includes both scope columns and predicates. In particular:

- Telemetry conflict identity becomes `(organization_id, site_id, agent_id, recorded_at)` in application SQL.
- Service snapshot delete is `WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3`.
- Log search and live notification fetch use `(organization_id, site_id, id, occurred_at)`.
- Notification payloads include non-secret scope and entry ID, e.g. `[{"organization_id":"org_default","site_id":"site_default","id":"..."}]`, so another hub process cannot publish an ID into the wrong site's subscribers.

- [ ] **Step 5: Run GREEN and commit**

Run: `go test ./internal/telemetry ./internal/serviceinventory ./internal/logstream ./internal/storage/postgres`

Expected: PASS or documented PostgreSQL skips.

```bash
git add internal/telemetry internal/serviceinventory internal/logstream internal/storage/postgres
git commit -m "feat: isolate telemetry services and logs by site"
```

---

### Task 6: Scope jobs while preserving signed payload compatibility

**Files:**

- Modify: `internal/jobs/jobs.go:61-360`
- Modify: `internal/jobs/jobs_test.go`
- Modify: `internal/storage/postgres/store.go:625-819`

**Interfaces:**

- Consumes human/default scope for job creation and trusted agent identity for claim/report.
- Produces scoped `Job` and `Event` records.

- [ ] **Step 1: Add failing job isolation/move tests**

Create the same agent ID in old/new `tenancy.Agent` values. Create a job in the old site, then verify:

- New-site `List` and `Events` do not return the old job.
- New-site `ClaimNext` cannot claim the old job.
- Old-site identity can claim/report the old job.
- A newly created new-site job is independently visible.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/jobs`

Expected: current agent-only signatures leak the old-site job.

- [ ] **Step 3: Add scope to job models and service methods**

Add `OrganizationID`/`SiteID` to `Job` and `Event`. Use these signatures:

```go
func (service *Service) Create(ctx context.Context, scope tenancy.Scope, agentID string, request CreateRequest) (Job, error)
func (service *Service) ClaimNext(ctx context.Context, agent tenancy.Agent) (*Job, error)
func (service *Service) Report(ctx context.Context, agent tenancy.Agent, jobID string, request EventRequest) (Job, error)
func (service *Service) List(ctx context.Context, scope tenancy.Scope, agentID string, limit int) ([]Job, error)
func (service *Service) Events(ctx context.Context, scope tenancy.Scope, agentID, jobID string) ([]Event, error)
```

Include organization/site in the versioned signed job payload and bump its `Version` from `1` to `2`; update verification tests so scope tampering invalidates the signature. Scope every memory key and PostgreSQL statement, including the transaction locks used for claim/event sequencing. Before creating a job, both memory and PostgreSQL stores must confirm the target host exists in the supplied scope and return `ErrJobNotFound` for a host assigned elsewhere.

- [ ] **Step 4: Run GREEN and commit**

Run: `go test ./internal/jobs ./internal/storage/postgres`

Expected: PASS.

```bash
git add internal/jobs internal/storage/postgres/store.go
git commit -m "feat: scope remote jobs by site"
```

---

### Task 7: Scope alert rules, maintenance, incidents, and events

**Files:**

- Modify: `internal/alerting/alerting.go:43-400`
- Modify: `internal/alerting/alerting_test.go`
- Modify: `internal/storage/postgres/store.go:821-1020`

**Interfaces:**

- Consumes host/site scope during scheduled and telemetry evaluation.
- Produces scoped alert state and immutable historical events.

- [ ] **Step 1: Add failing alert isolation tests**

For identical agent IDs in `site_old` and `site_new`, prove that rules/windows in one site cannot suppress or create incidents in the other. After acknowledging an old-site incident, verify a new-site lookup returns `ErrNotFound`, not the incident.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/alerting`

Expected: existing global rule/window lists cause cross-site evaluation.

- [ ] **Step 3: Implement scoped alert contracts**

Add `OrganizationID`/`SiteID` to `Rule`, `MaintenanceWindow`, `Incident`, and `Event`. Add `scope tenancy.Scope` as the first domain argument to all public service methods:

```go
CreateRule, ListRules, CreateMaintenance, ListMaintenance,
EvaluateTelemetry, EvaluateReachability, Acknowledge,
ListIncidents, ListEvents
```

Propagate the same scope through every store method. PostgreSQL `EnsureIncident` and active-incident uniqueness must use `(organization_id, site_id, rule_id, agent_id)`; replace the old partial unique index in migration 015 with a scoped partial unique index. All incident/event reads and mutations predicate on scope. When `MaintenanceRequest.AgentID` is non-empty, creation must verify that the host belongs to the supplied scope and behave as not found on mismatch.

- [ ] **Step 4: Run GREEN and commit**

Run: `go test ./internal/alerting ./internal/storage/postgres`

Expected: PASS.

```bash
git add internal/alerting internal/storage/postgres
git commit -m "feat: isolate alerting by site"
```

---

### Task 8: Scope cloud accounts and discovery reconciliation

**Files:**

- Modify: `internal/cloudinventory/cloudinventory.go:39-330`
- Modify: `internal/cloudinventory/cloudinventory_test.go`
- Modify: `internal/storage/postgres/store.go:1022-1138`

**Interfaces:**

- Consumes `inventory.Service.List(ctx, scope)` from Task 4.
- Produces site-scoped cloud account and instance records.

- [ ] **Step 1: Add failing discovery isolation tests**

Create two sites with hosts that deliberately share hostname/private IP hints. Reconcile an account in site A and assert that only site A hosts can become verified/candidate matches. Assert site B cannot get/reconcile/list site A's account or instances.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/cloudinventory`

Expected: global account/host lists leak matches.

- [ ] **Step 3: Implement scoped cloud contracts**

Add scope fields to `Account` and `Instance`; add `tenancy.Scope` to every service/store method. Update `HostLister` to:

```go
type HostLister interface {
	List(context.Context, tenancy.Scope) ([]inventory.Host, error)
}
```

`Reconcile` must load the account under the supplied scope first, then list hosts under that same scope. PostgreSQL must scope its account uniqueness as `(organization_id, site_id, provider, external_id)`, all account lookups/updates/deletes, and all instance queries.

- [ ] **Step 4: Run GREEN and commit**

Run: `go test ./internal/cloudinventory ./internal/storage/postgres`

Expected: PASS.

```bash
git add internal/cloudinventory internal/storage/postgres
git commit -m "feat: scope cloud inventory by site"
```

---

### Task 9: Wire trusted scopes through HTTP and hub startup

**Files:**

- Modify: `internal/server/server.go:25-205,270-976`
- Modify: `internal/server/enrollment_test.go`
- Modify: `internal/server/inventory_test.go`
- Modify: `internal/server/telemetry_test.go`
- Modify: `internal/server/services_test.go`
- Modify: `internal/server/logs_test.go`
- Modify: `internal/server/jobs_test.go`
- Modify: `internal/server/alerting_test.go`
- Modify: `internal/server/cloudinventory_test.go`
- Modify: `cmd/bazusop-hub/main.go:44-156`

**Interfaces:**

- Consumes `Authority.AuthenticateContext` for agent scope.
- Consumes `tenancy.DefaultScope()` for transitional human routes.
- Produces a handler surface that cannot be site-switched by untrusted request data.

- [ ] **Step 1: Add failing handler tests**

Update existing feature tests to assert returned objects contain the default organization/site. Add a cross-site spoofing test:

```go
func TestAgentReportIgnoresUntrustedSiteSelectors(t *testing.T) {
	authority, identity := enrolledIdentity(t)
	service := inventory.NewService(inventory.NewMemoryStore())
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithInventory(service))
	facts := inventory.Facts{Hostname: "edge-01", OSFamily: "linux", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/agents/inventory?site_id=site_other", encodeJSON(t, facts))
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, identity.CertificatePEM)}}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent { t.Fatalf("unexpected status: %d", response.Code) }
	other, err := service.List(t.Context(), tenancy.Scope{OrganizationID: "org_default", SiteID: "site_other"})
	if err != nil { t.Fatal(err) }
	if len(other) != 0 { t.Fatalf("query parameter changed trusted scope: %#v", other) }
	defaults, err := service.List(t.Context(), tenancy.DefaultScope())
	if err != nil || len(defaults) != 1 { t.Fatalf("default scoped report missing: %#v, %v", defaults, err) }
}
```

Add a second request whose JSON includes `"site_id":"site_other"`; because `decodeJSON` disallows unknown fields, assert `400 Bad Request` and no additional host write.

Also add a test where a valid CA-signed certificate has no persistent agent mapping; an ingest route must return `401`.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/server`

Expected: compile failures until all handler calls provide scope.

- [ ] **Step 3: Add scope to handler configuration**

Add `scope tenancy.Scope` to `handlerOptions`, initialize it to `tenancy.DefaultScope()` in `NewHandler`, and add a `WithDefaultScope` option for explicit startup/tests. Validate once and panic on invalid programmer configuration; do not fall back from an explicitly invalid scope.

Change the shared authentication helper to:

```go
func authenticateAgent(response http.ResponseWriter, request *http.Request, authority *enrollment.Authority) (tenancy.Agent, bool) {
	if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
		http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return tenancy.Agent{}, false
	}
	agent, err := authority.AuthenticateContext(request.Context(), request.TLS.PeerCertificates[0])
	if err != nil {
		http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return tenancy.Agent{}, false
	}
	return agent, true
}
```

Use `agent` for all ingest/claim/report calls. Use `configuration.scope` for current human-facing inventory, history, search, job, alert, and cloud routes. Do not inspect `site_id` request values. Call `RenewContext` on renewal.

- [ ] **Step 4: Wire hub startup and scheduled alert evaluation**

In `cmd/bazusop-hub/main.go`, declare `defaultScope := tenancy.DefaultScope()` once. Pass it into `NewPersistentAuthority` and `server.WithDefaultScope`. Update reachability evaluation to iterate hosts from the default scope and call `EvaluateReachability(ctx, host.Scope(), ...)`; add a `Host.Scope()` helper if that removes repeated field assembly.

- [ ] **Step 5: Run focused and full GREEN**

Run:

```bash
go test ./internal/server ./cmd/bazusop-hub
go test ./...
```

Expected: all Go tests PASS; environment-gated PostgreSQL tests may skip only when the database URL is absent.

- [ ] **Step 6: Commit**

```bash
git add internal/server cmd/bazusop-hub/main.go
git commit -m "feat: enforce trusted site scope in hub routes"
```

---

### Task 10: Prove PostgreSQL isolation, migration backfill, and history preservation

**Files:**

- Create: `internal/storage/postgres/tenancy_integration_test.go`
- Modify: `internal/storage/postgres/enrollment_state_integration_test.go`

**Interfaces:**

- Exercises the concrete PostgreSQL implementation of every interface changed above.
- Validates the Spike 11.1 acceptance signal against real constraints and transactions, not only memory stores.

- [ ] **Step 1: Add a reusable isolated-schema integration harness**

When `BAZUSOP_TEST_DATABASE_URL` is set, create a random schema, connect with `search_path` set to it, and drop only that exact schema in `t.Cleanup`. Validate the generated schema name against `^[a-z0-9_]+$` before issuing quoted DDL. Do not clear or reuse the caller's public schema.

- [ ] **Step 2: Write the upgrade-backfill test**

In the isolated schema:

1. Execute embedded migrations `001` through `014` in order.
2. Insert one coherent row into every pre-015 resource table.
3. Execute `015_organization_sites.sql` twice inside separate transactions. Constraint creation must use catalog-checked `DO` blocks and index creation must use `IF NOT EXISTS`, so the second execution succeeds without creating a second organization/site or altering the first IDs.
4. Assert exactly one `org_default`, one `site_default`, no null scope in any listed resource table, and an `agents` row for the existing host.

Run: `BAZUSOP_TEST_DATABASE_URL='postgres://test-connection' go test ./internal/storage/postgres -run TestOrganizationSiteMigrationBackfillsExistingData -count=1`

Expected RED before the harness/SQL is complete; GREEN afterward.

- [ ] **Step 3: Write the cross-site and agent-move history test**

Using two sites in the same organization:

1. Register/enroll an agent into site A.
2. Write host, telemetry, log, job/event, and incident/event data under site A.
3. Update only `agents` and current `hosts` to site B in one transaction (the future platform-admin move primitive).
4. Resolve the mTLS agent again and confirm new telemetry/log/job data is stored under site B.
5. Assert site B cannot read the old site A history and site A can still read it.
6. Query each created agent/resource through the wrong organization or site and assert no rows or domain not-found errors.

- [ ] **Step 4: Run the real-database suite**

Run: `BAZUSOP_TEST_DATABASE_URL='postgres://test-connection' go test ./internal/storage/postgres -count=1`

Expected: PASS. If no test database is available, report the integration tests as unverified rather than claiming them passed.

- [ ] **Step 5: Run formatting, static checks, and complete regression suite**

Run:

```bash
gofmt -w internal/tenancy/*.go internal/enrollment/*.go internal/inventory/*.go internal/telemetry/*.go internal/serviceinventory/*.go internal/logstream/*.go internal/jobs/*.go internal/alerting/*.go internal/cloudinventory/*.go internal/storage/postgres/*.go internal/server/*.go cmd/bazusop-hub/*.go
go vet ./...
go test ./...
npm --prefix web test -- --run
npm --prefix web run build
git diff --check
```

Expected: all commands PASS. The web test/build ensures added JSON fields did not break the existing frontend contract.

- [ ] **Step 6: Commit the acceptance proof**

```bash
git add internal/storage/postgres
git commit -m "test: prove site migration and isolation"
```

---

## Completion gate

Before declaring Spike 11.1 complete, verify all of the following:

- [ ] Every resource named in the approved design has non-null organization/site identity in PostgreSQL and explicit fields in its Go model.
- [ ] Every store read and mutation includes validated scope; search confirms no operational query still filters only by `agent_id`, resource ID, or account ID.
- [ ] Enrollment tokens are registered for a site and atomically bind a new agent to that site when consumed.
- [ ] A valid certificate without a current persistent agent record cannot ingest or claim work.
- [ ] Request body/query/path cannot choose an agent's site.
- [ ] A resource or agent ID queried through the wrong site does not leak through list, history, stream, claim, acknowledge, or reconcile flows.
- [ ] Agent moves affect current agent/host placement and future records only; historical records retain their original site.
- [ ] No new site-management HTTP mutation exists before actor authorization and audit are implemented.
- [ ] Unit tests, `go vet`, frontend tests/build, and `git diff --check` pass.
- [ ] PostgreSQL integration tests pass against a real database, or the handoff explicitly marks that single verification as pending.

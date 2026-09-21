# Audit Trail Foundation (Spike 11.2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every API request the hub serves produces an append-only, redacted audit record with a server-generated correlation ID and a best-effort actor identity, stored in PostgreSQL (or an in-process store in memory mode), so spike 11.3+ can build identity/session/RBAC on top of a working audit substrate instead of retrofitting one later.

**Architecture:** A new `internal/audittrail` package defines the `Event` shape and a `Store` interface (PostgreSQL + in-memory implementations, following the same pattern as every other domain package in this codebase). `internal/server` gains a route-registration wrapper that every `mux.HandleFunc` call in `NewHandler` goes through; the wrapper generates a correlation ID, derives a best-effort actor from the request (mTLS cert present → `agent`; `Authorization` header present → `legacy_token`; neither → `anonymous`), captures the response status via a `http.ResponseWriter` wrapper, and records one audit event after the handler returns.

**Tech Stack:** Go 1.26, `net/http` (stdlib `ServeMux`), PostgreSQL via `pgx/v5` (existing `internal/storage/postgres` package), `crypto/rand` for ID generation (matching the existing `newID()` pattern in `internal/jobs` and `internal/alerting`).

**Spec:** [docs/superpowers/specs/2026-09-13-enterprise-rbac-identity-audit-design.md](../specs/2026-09-13-enterprise-rbac-identity-audit-design.md) — sections "Ortak istek bağlamı" and "Audit, activity ve operasyon logları" → "Audit". This plan implements roadmap spike 11.2 only.

## Global Constraints

- Audit events are append-only: no update or delete API, and the PostgreSQL table rejects `UPDATE`/`DELETE` at the trigger level (spec: "Audit olayları append-only'dir. Ürün API'si update veya delete sunmaz.").
- Audit event fields, verbatim from the spec: `event_id, occurred_at, correlation_id, actor_type, actor_id, session_id veya token_id, organization_id, site_id, action, permission, resource_type, resource_id, outcome, error_code, source_ip, user_agent, redacted_change_summary`.
- No secret value is ever written to an audit event field (spec: "Hassas veri redaksiyonu" — password, TOTP secret, recovery code, cookie, CSRF token, invite token, bearer token, OIDC client secret, authorization code, private key, full request bodies never appear in any event field).
- Scope boundaries of this spike, explicit so later spikes know what to build on top of rather than assuming it already exists:
  - `session_id`/`token_id` are always empty in this spike — human sessions (11.4) and service account tokens (11.6) don't exist yet. `actor_type` is `agent` or `legacy_token` (today's `BAZUSOP_OPERATOR_TOKEN`/`BAZUSOP_ADMIN_TOKEN`) or `anonymous`; `human`/`service` are added when 11.3/11.6 land, by adding new `ActorType` values, not by changing the schema.
  - `permission` is always empty — the permission catalog is spike 11.5. It is a real, present, empty-string column, not a placeholder.
  - `redacted_change_summary` in this spike is always empty. Field-level allowlisted change summaries per mutation endpoint (spec: "Değişiklik özeti alan bazlı allowlist ile üretilir") are real work for a follow-up spike; an empty summary is vacuously redacted (nothing captured, nothing to leak) rather than an approximation that could leak something. Do not attempt to serialize request bodies in this spike.
  - Audit writes in this spike happen after the wrapped handler returns, in a separate statement from whatever the handler did — not in the same database transaction as the handler's own writes. The spec's same-transaction / rollback-on-audit-failure guarantee ("Audit yazılamıyorsa mutasyon commit edilmez") applies to the security-critical identity/session/RBAC mutations spikes 11.3+ introduce; those mutations will call `audittrail.Service.Record` directly inside their own transaction. Document this boundary in the package doc comment so it isn't silently assumed to already hold.
  - Static asset serving (`mux.Handle("/", webui.Handler())`) and the `/api/` catch-all 404 are not wrapped — they're not API requests in the spec's sense (serving `index.html` or a JS bundle isn't a resource access worth an audit row), and wrapping the catch-all would audit-log every 404 probe from internet scanners at high volume with no security value beyond what access logs already give.

## File Structure

- `internal/audittrail/audittrail.go` — `Event`, `ActorType`, `Outcome` types; `Store` interface; `Service` (thin wrapper that fills `EventID`/`OccurredAt` if unset and calls `Store.Record`); `MemoryStore` (dev/test).
- `internal/audittrail/audittrail_test.go` — `MemoryStore` and `Service` unit tests.
- `internal/storage/postgres/migrations/018_audit_trail.sql` — `audit_events` table, append-only trigger, scoped index.
- `internal/storage/postgres/audittrail.go` — `(*Store) Record` implementing `audittrail.Store` (same pattern as `internal/storage/postgres/jobs.go` etc.), plus a `ListAuditTrail` method the integration test uses to verify writes.
- `internal/storage/postgres/audittrail_integration_test.go` — real-PostgreSQL test: insert succeeds, `UPDATE`/`DELETE` are rejected by the trigger, fields round-trip correctly.
- `internal/server/middleware.go` — `newCorrelationID()`, `correlationIDKey` context key, `statusRecorder`, `deriveActor(*http.Request) (audittrail.ActorType, string)`, `registerAudited(mux *http.ServeMux, pattern, method, resourceType string, pathParams []string, service *audittrail.Service, scope tenancy.Scope, handler http.HandlerFunc)`.
- `internal/server/middleware_test.go` — unit tests for `deriveActor`, `statusRecorder`, and `registerAudited` (using a `MemoryStore`-backed service and `httptest`).
- `internal/server/server.go` — modified: `handlerOptions` gains `auditTrail *audittrail.Service`; `NewHandler` gains `WithAuditTrail(service *audittrail.Service) Option`; every existing `mux.HandleFunc(...)` call is replaced with a `registerAudited(mux, ...)` call.
- `cmd/bazusop-hub/main.go` — modified: construct an `audittrail.Store` (memory or PostgreSQL, following the exact pattern already used for `jobStore`/`alertStore`/`auditStore` — note the existing `audit.Store`/`auditStore` variable in this file is the *unrelated* job/alert timeline feature from spike 12; name the new variable `auditTrailStore`/`auditTrailService` to avoid confusion) and pass `server.WithAuditTrail(auditTrailService)`.

## Task 1: `internal/audittrail` package — Event, Store, MemoryStore

**Files:**
- Create: `internal/audittrail/audittrail.go`
- Create: `internal/audittrail/audittrail_test.go`

**Interfaces:**
- Produces: `type ActorType string` with `ActorAgent ActorType = "agent"`, `ActorLegacyToken ActorType = "legacy_token"`, `ActorAnonymous ActorType = "anonymous"`; `type Outcome string` with `OutcomeSuccess Outcome = "success"`, `OutcomeFailure Outcome = "failure"`; `type Event struct` with fields `EventID, CorrelationID, ActorType (ActorType), ActorID, SessionOrTokenID, OrganizationID, SiteID, Action, Permission, ResourceType, ResourceID, Outcome (Outcome), ErrorCode, SourceIP, UserAgent, ChangeSummary string` and `OccurredAt time.Time`; `type Store interface { Record(ctx context.Context, event Event) error }`; `type Service struct` with `func NewService(store Store) *Service` and `func (service *Service) Record(ctx context.Context, event Event) error`; `func NewMemoryStore() *MemoryStore` and `func (store *MemoryStore) Record(ctx context.Context, event Event) error` plus `func (store *MemoryStore) Events() []Event` (test-only accessor, returns a copy).

- [x] **Step 1: Write the failing test**

```go
// internal/audittrail/audittrail_test.go
package audittrail_test

import (
	"context"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/audittrail"
)

func TestMemoryStoreRecordsEventsInOrder(t *testing.T) {
	t.Parallel()
	store := audittrail.NewMemoryStore()
	first := audittrail.Event{CorrelationID: "corr-1", ActorType: audittrail.ActorAgent, ActorID: "agent-1", OrganizationID: "org_default", SiteID: "site_default", Action: "GET", ResourceType: "health", Outcome: audittrail.OutcomeSuccess}
	second := audittrail.Event{CorrelationID: "corr-2", ActorType: audittrail.ActorLegacyToken, ActorID: "bearer", OrganizationID: "org_default", SiteID: "site_default", Action: "POST", ResourceType: "jobs", Outcome: audittrail.OutcomeFailure, ErrorCode: "401"}

	if err := store.Record(context.Background(), first); err != nil {
		t.Fatalf("record first: %v", err)
	}
	if err := store.Record(context.Background(), second); err != nil {
		t.Fatalf("record second: %v", err)
	}

	events := store.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].CorrelationID != "corr-1" || events[1].CorrelationID != "corr-2" {
		t.Fatalf("unexpected order: %#v", events)
	}
	if events[1].Outcome != audittrail.OutcomeFailure || events[1].ErrorCode != "401" {
		t.Fatalf("failure event not recorded correctly: %#v", events[1])
	}
}

func TestServiceFillsEventIDAndOccurredAtWhenUnset(t *testing.T) {
	t.Parallel()
	store := audittrail.NewMemoryStore()
	service := audittrail.NewService(store)

	if err := service.Record(context.Background(), audittrail.Event{CorrelationID: "corr-3", ActorType: audittrail.ActorAnonymous, OrganizationID: "org_default", SiteID: "site_default", Action: "GET", ResourceType: "health", Outcome: audittrail.OutcomeSuccess}); err != nil {
		t.Fatalf("record: %v", err)
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventID == "" {
		t.Fatal("expected Service to fill EventID")
	}
	if events[0].OccurredAt.IsZero() {
		t.Fatal("expected Service to fill OccurredAt")
	}
	if time.Since(events[0].OccurredAt) > time.Minute {
		t.Fatalf("OccurredAt looks stale: %v", events[0].OccurredAt)
	}
}

func TestServiceDoesNotOverwriteCallerSuppliedEventIDOrOccurredAt(t *testing.T) {
	t.Parallel()
	store := audittrail.NewMemoryStore()
	service := audittrail.NewService(store)
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := service.Record(context.Background(), audittrail.Event{EventID: "explicit-id", OccurredAt: fixed, CorrelationID: "corr-4", ActorType: audittrail.ActorAnonymous, OrganizationID: "org_default", SiteID: "site_default", Action: "GET", ResourceType: "health", Outcome: audittrail.OutcomeSuccess}); err != nil {
		t.Fatalf("record: %v", err)
	}

	events := store.Events()
	if events[0].EventID != "explicit-id" {
		t.Fatalf("expected explicit EventID to be preserved, got %q", events[0].EventID)
	}
	if !events[0].OccurredAt.Equal(fixed) {
		t.Fatalf("expected explicit OccurredAt to be preserved, got %v", events[0].OccurredAt)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/audittrail/... -v`
Expected: FAIL — `package audittrail: no Go files in ...` (package doesn't exist yet).

- [x] **Step 3: Write minimal implementation**

```go
// internal/audittrail/audittrail.go

// Package audittrail records one append-only event per API request: who
// (best-effort actor), what (resource/action), and the outcome. It is the
// substrate spike 11.3+ (identity, sessions, RBAC) builds on. Writes in this
// package happen after a request's handler has already returned, in a
// separate statement — NOT inside the handler's own database transaction.
// The spec's same-transaction / rollback-on-audit-failure guarantee applies
// to the security-critical mutations later spikes introduce (user creation,
// role changes, session revocation); those call Service.Record directly
// inside their own transaction instead of going through the HTTP-layer
// wrapper in internal/server this package's caller uses today.
package audittrail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

type ActorType string

const (
	ActorAgent       ActorType = "agent"
	ActorLegacyToken ActorType = "legacy_token"
	ActorAnonymous   ActorType = "anonymous"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

// Event is intentionally flat (no nested structs) so every field maps to
// exactly one audit_events column with no ambiguity about how to store it.
type Event struct {
	EventID          string
	OccurredAt       time.Time
	CorrelationID    string
	ActorType        ActorType
	ActorID          string
	SessionOrTokenID string
	OrganizationID   string
	SiteID           string
	Action           string
	Permission       string
	ResourceType     string
	ResourceID       string
	Outcome          Outcome
	ErrorCode        string
	SourceIP         string
	UserAgent        string
	ChangeSummary    string
}

type Store interface {
	Record(ctx context.Context, event Event) error
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (service *Service) Record(ctx context.Context, event Event) error {
	if event.EventID == "" {
		id, err := newEventID()
		if err != nil {
			return err
		}
		event.EventID = id
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	return service.store.Record(ctx, event)
}

func newEventID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
```

```go
// internal/audittrail/memorystore.go
package audittrail

import (
	"context"
	"sync"
)

// MemoryStore is for local development and tests only; it holds no
// durability guarantee and is never used against a real deployment (the
// hub only constructs it when DATABASE_URL is unset, matching every other
// domain's memory-mode fallback).
type MemoryStore struct {
	mu     sync.Mutex
	events []Event
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (store *MemoryStore) Record(_ context.Context, event Event) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.events = append(store.events, event)
	return nil
}

// Events returns a copy of every recorded event, oldest first. Test-only.
func (store *MemoryStore) Events() []Event {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]Event(nil), store.events...)
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/audittrail/... -v`
Expected: PASS (3 tests)

- [x] **Step 5: Run go vet and gofmt**

Run: `go vet ./internal/audittrail/... && gofmt -l internal/audittrail/*.go`
Expected: no output from either command

- [x] **Step 6: Commit**

```bash
git add internal/audittrail/audittrail.go internal/audittrail/memorystore.go internal/audittrail/audittrail_test.go
git commit -m "feat: add audittrail package with event model and memory store"
```

## Task 2: PostgreSQL store for audit events

**Files:**
- Create: `internal/storage/postgres/migrations/018_audit_trail.sql`
- Create: `internal/storage/postgres/audittrail.go`
- Create: `internal/storage/postgres/audittrail_integration_test.go`

**Interfaces:**
- Consumes: `audittrail.Event`, `audittrail.Store` (Task 1).
- Produces: `func (store *Store) Record(ctx context.Context, event audittrail.Event) error` (satisfies `audittrail.Store`); `func (store *Store) ListAuditTrail(ctx context.Context, scope tenancy.Scope, limit int) ([]audittrail.Event, error)` (test-only read path — no HTTP endpoint in this spike).

- [x] **Step 1: Write the failing integration test**

```go
// internal/storage/postgres/audittrail_integration_test.go
package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresAuditTrailIsAppendOnly(t *testing.T) {
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	scope := tenancy.Scope{OrganizationID: "audittrail-org-" + suffix, SiteID: "audittrail-site-" + suffix}
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Audit trail test')`, scope.OrganizationID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, table := range []string{"audit_events", "sites", "organizations"} {
			column := "organization_id"
			if table == "organizations" {
				column = "id"
			}
			if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM "+table+" WHERE "+column+"=$1", scope.OrganizationID); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	}()
	if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1, $2, $1, $1)`, scope.SiteID, scope.OrganizationID); err != nil {
		t.Fatal(err)
	}

	event := audittrail.Event{
		EventID: "audittrail-event-" + suffix, OccurredAt: time.Now().UTC(),
		CorrelationID: "corr-" + suffix, ActorType: audittrail.ActorAgent, ActorID: "agent-" + suffix,
		OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
		Action: "GET", ResourceType: "health", Outcome: audittrail.OutcomeSuccess,
		SourceIP: "127.0.0.1", UserAgent: "test-agent",
	}
	if err := store.Record(ctx, event); err != nil {
		t.Fatalf("record event: %v", err)
	}

	events, err := store.ListAuditTrail(ctx, scope, 10)
	if err != nil || len(events) != 1 || events[0].EventID != event.EventID || events[0].CorrelationID != event.CorrelationID || events[0].Outcome != audittrail.OutcomeSuccess {
		t.Fatalf("expected the recorded event back, got %#v, %v", events, err)
	}

	if _, err := store.pool.Exec(ctx, `UPDATE audit_events SET outcome='failure' WHERE event_id=$1`, event.EventID); err == nil {
		t.Fatal("expected UPDATE on audit_events to be rejected")
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM audit_events WHERE event_id=$1`, event.EventID); err == nil {
		t.Fatal("expected DELETE on audit_events to be rejected")
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

Run: `BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./internal/storage/postgres/... -run TestPostgresAuditTrailIsAppendOnly -v`

(Start a throwaway PostgreSQL 18 container first if none is running: `docker run -d --rm --name audittrail-check -e POSTGRES_DB=bazusop_test -e POSTGRES_USER=bazusop -e POSTGRES_PASSWORD=bazusop_test -p 5432:5432 postgres:18` and wait for `pg_isready`.)

Expected: FAIL — `store.Record undefined` / `store.ListAuditTrail undefined` (methods don't exist yet).

- [x] **Step 3: Write the migration**

```sql
-- internal/storage/postgres/migrations/018_audit_trail.sql
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
    PRIMARY KEY (event_id, occurred_at),
    CONSTRAINT audit_events_site_fk FOREIGN KEY (organization_id, site_id)
        REFERENCES sites(organization_id, id) ON DELETE RESTRICT
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
```

- [x] **Step 4: Write the Store implementation**

```go
// internal/storage/postgres/audittrail.go
package postgres

import (
	"context"
	"fmt"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func (store *Store) Record(ctx context.Context, event audittrail.Event) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO audit_events (
			event_id, occurred_at, correlation_id, actor_type, actor_id, session_or_token_id,
			organization_id, site_id, action, permission, resource_type, resource_id,
			outcome, error_code, source_ip, user_agent, change_summary
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		event.EventID, event.OccurredAt, event.CorrelationID, string(event.ActorType), event.ActorID, event.SessionOrTokenID,
		event.OrganizationID, event.SiteID, event.Action, event.Permission, event.ResourceType, event.ResourceID,
		string(event.Outcome), event.ErrorCode, event.SourceIP, event.UserAgent, event.ChangeSummary,
	)
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

func (store *Store) ListAuditTrail(ctx context.Context, scope tenancy.Scope, limit int) ([]audittrail.Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT event_id, occurred_at, correlation_id, actor_type, actor_id, session_or_token_id,
			organization_id, site_id, action, permission, resource_type, resource_id,
			outcome, error_code, source_ip, user_agent, change_summary
		FROM audit_events
		WHERE organization_id = $1 AND site_id = $2
		ORDER BY occurred_at DESC
		LIMIT $3`, scope.OrganizationID, scope.SiteID, limit)
	if err != nil {
		return nil, fmt.Errorf("query audit trail: %w", err)
	}
	defer rows.Close()
	events := make([]audittrail.Event, 0)
	for rows.Next() {
		var event audittrail.Event
		var actorType, outcome string
		if err := rows.Scan(&event.EventID, &event.OccurredAt, &event.CorrelationID, &actorType, &event.ActorID, &event.SessionOrTokenID,
			&event.OrganizationID, &event.SiteID, &event.Action, &event.Permission, &event.ResourceType, &event.ResourceID,
			&outcome, &event.ErrorCode, &event.SourceIP, &event.UserAgent, &event.ChangeSummary); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		event.ActorType, event.Outcome = audittrail.ActorType(actorType), audittrail.Outcome(outcome)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit trail: %w", err)
	}
	return events, nil
}
```

- [x] **Step 5: Run the integration test to verify it passes**

Run: `BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./internal/storage/postgres/... -run TestPostgresAuditTrailIsAppendOnly -v`
Expected: PASS

- [x] **Step 6: Run the full test suite, vet, and gofmt**

Run: `go vet ./... && gofmt -l internal/storage/postgres/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./...`
Expected: no vet/gofmt output; all packages `ok`

- [x] **Step 7: Commit**

```bash
git add internal/storage/postgres/migrations/018_audit_trail.sql internal/storage/postgres/audittrail.go internal/storage/postgres/audittrail_integration_test.go
git commit -m "feat: add append-only PostgreSQL store for audit events"
```

## Task 3: Correlation ID, actor derivation, and status recorder

**Files:**
- Create: `internal/server/middleware.go`
- Create: `internal/server/middleware_test.go`

**Interfaces:**
- Consumes: `audittrail.ActorType`, `audittrail.ActorAgent`, `audittrail.ActorLegacyToken`, `audittrail.ActorAnonymous` (Task 1).
- Produces: `func newCorrelationID() (string, error)`; `func deriveActor(request *http.Request) (audittrail.ActorType, string)`; `type statusRecorder struct` wrapping `http.ResponseWriter` with `func (recorder *statusRecorder) WriteHeader(status int)` and a `status int` field (defaults to `http.StatusOK` if `WriteHeader` is never called, matching `net/http` convention).

- [x] **Step 1: Write the failing test**

```go
// internal/server/middleware_test.go
package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
)

func TestDeriveActorPrefersAgentCertOverBearerHeader(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Authorization", "Bearer some-token")
	actorType, actorID := deriveActor(request)
	if actorType != audittrail.ActorLegacyToken || actorID != "bearer" {
		t.Fatalf("expected legacy_token/bearer without a client cert, got %v/%q", actorType, actorID)
	}
}

func TestDeriveActorIsAnonymousWithoutCertOrHeader(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	actorType, actorID := deriveActor(request)
	if actorType != audittrail.ActorAnonymous || actorID != "" {
		t.Fatalf("expected anonymous/empty, got %v/%q", actorType, actorID)
	}
}

func TestStatusRecorderDefaultsToOKWhenWriteHeaderNeverCalled(t *testing.T) {
	t.Parallel()
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	if _, err := recorder.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if recorder.status != http.StatusOK {
		t.Fatalf("expected default 200, got %d", recorder.status)
	}
}

func TestStatusRecorderCapturesExplicitWriteHeader(t *testing.T) {
	t.Parallel()
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	recorder.WriteHeader(http.StatusNotFound)
	if recorder.status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.status)
	}
}

func TestNewCorrelationIDProducesDistinctNonEmptyValues(t *testing.T) {
	t.Parallel()
	first, err := newCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || second == "" || first == second {
		t.Fatalf("expected distinct non-empty ids, got %q and %q", first, second)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestDeriveActor|TestStatusRecorder|TestNewCorrelationID' -v`
Expected: FAIL — `undefined: deriveActor` / `undefined: statusRecorder` / `undefined: newCorrelationID`

- [x] **Step 3: Write minimal implementation**

```go
// internal/server/middleware.go
package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/audittrail"
)

func newCorrelationID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// deriveActor makes a best-effort identification of who is calling, purely
// for audit labeling. It does not authenticate anything — a route's own
// handler (authenticateAgent, authorizeRole) independently verifies the
// client certificate or bearer token and is solely responsible for
// authorization decisions. An unverified or forged cert/header still gets a
// label here; the audit outcome/error_code on the same event reflects
// whether the handler actually accepted it.
func deriveActor(request *http.Request) (audittrail.ActorType, string) {
	if request.TLS != nil && len(request.TLS.PeerCertificates) > 0 {
		cert := request.TLS.PeerCertificates[0]
		if len(cert.URIs) > 0 {
			return audittrail.ActorAgent, cert.URIs[0].String()
		}
		return audittrail.ActorAgent, cert.Subject.CommonName
	}
	if request.Header.Get("Authorization") != "" {
		return audittrail.ActorLegacyToken, "bearer"
	}
	return audittrail.ActorAnonymous, ""
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestDeriveActor|TestStatusRecorder|TestNewCorrelationID' -v`
Expected: PASS (5 tests)

- [x] **Step 5: Run go vet and gofmt**

Run: `go vet ./internal/server/... && gofmt -l internal/server/*.go`
Expected: no output from either command

- [x] **Step 6: Commit**

```bash
git add internal/server/middleware.go internal/server/middleware_test.go
git commit -m "feat: add correlation ID, actor derivation, and status recorder for audit middleware"
```

## Task 4: Wire audit recording into every route

**Files:**
- Modify: `internal/server/middleware.go` (add `registerAudited`)
- Modify: `internal/server/server.go` (add `WithAuditTrail`, `auditTrail` field, replace every `mux.HandleFunc` call)
- Modify: `internal/server/middleware_test.go` (add `registerAudited` test)
- Modify: `internal/server/audit_test.go` (existing spike-12 audit-timeline test file; only the `server.NewHandler(...)` call in its test needs `server.WithAuditTrail(...)` added so the handler under test has a non-nil audit trail service — the assertions are unchanged)
- Modify: `cmd/bazusop-hub/main.go` (construct the store/service, pass the option)

**Interfaces:**
- Consumes: `audittrail.Service`, `audittrail.Event`, `deriveActor`, `statusRecorder`, `newCorrelationID` (Tasks 1 and 3); existing `handlerOptions`, `NewHandler`, `Option` (`internal/server/server.go`).
- Produces: `func WithAuditTrail(service *audittrail.Service) Option`; `func registerAudited(mux *http.ServeMux, pattern, method, resourceType string, pathParams []string, service *audittrail.Service, scope tenancy.Scope, handler http.HandlerFunc)` — registers `mux.HandleFunc(method+" "+pattern, wrapped)` where `wrapped` runs `handler`, then (if `service != nil`) records one `audittrail.Event`.

- [x] **Step 1: Write the failing test**

```go
// internal/server/middleware_test.go — append to the existing file
func TestRegisterAuditedRecordsOneEventPerRequestWithPathValues(t *testing.T) {
	t.Parallel()
	store := audittrail.NewMemoryStore()
	service := audittrail.NewService(store)
	mux := http.NewServeMux()
	scope := tenancy.DefaultScope()

	registerAudited(mux, "/api/v1/instances/{agentID}/jobs/{jobID}/events", http.MethodGet, "job_events", []string{"agentID", "jobID"}, service, scope,
		func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNotFound) })

	request := httptest.NewRequest(http.MethodGet, "/api/v1/instances/agent-1/jobs/job-1/events", nil)
	request.Header.Set("User-Agent", "test-client")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected the wrapped handler's status to pass through, got %d", response.Code)
	}
	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	event := events[0]
	if event.Action != http.MethodGet || event.ResourceType != "job_events" || event.ResourceID != "agent-1/job-1" {
		t.Fatalf("unexpected event shape: %#v", event)
	}
	if event.Outcome != audittrail.OutcomeFailure || event.ErrorCode != "404" {
		t.Fatalf("expected failure/404, got outcome=%v error_code=%q", event.Outcome, event.ErrorCode)
	}
	if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
		t.Fatalf("expected the configured scope on the event, got %#v", event)
	}
	if event.CorrelationID == "" {
		t.Fatal("expected a non-empty correlation id")
	}
	if event.UserAgent != "test-client" {
		t.Fatalf("expected the request's User-Agent to be captured, got %q", event.UserAgent)
	}
	if event.ActorType != audittrail.ActorAnonymous {
		t.Fatalf("expected anonymous actor for a request with no cert or auth header, got %v", event.ActorType)
	}
}

func TestRegisterAuditedIsANoOpWhenServiceIsNil(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	called := false
	registerAudited(mux, "/api/v1/health", http.MethodGet, "health", nil, nil, tenancy.DefaultScope(),
		func(response http.ResponseWriter, _ *http.Request) { called = true; response.WriteHeader(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if !called {
		t.Fatal("expected the wrapped handler to still run with a nil audit service")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
}
```

Add `"github.com/gokayybaz/bazusop/internal/tenancy"` to this test file's imports.

- [x] **Step 2: Run the test to verify it fails**

Run: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run TestRegisterAudited -v`
Expected: FAIL — `undefined: registerAudited`

- [x] **Step 3: Implement `registerAudited`**

Append to `internal/server/middleware.go`:

```go
func registerAudited(mux *http.ServeMux, pattern, method, resourceType string, pathParams []string, service *audittrail.Service, scope tenancy.Scope, handler http.HandlerFunc) {
	mux.HandleFunc(method+" "+pattern, func(response http.ResponseWriter, request *http.Request) {
		if service == nil {
			handler(response, request)
			return
		}
		correlationID, err := newCorrelationID()
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}
		actorType, actorID := deriveActor(request)

		handler(recorder, request)

		resourceID := ""
		for index, name := range pathParams {
			if index > 0 {
				resourceID += "/"
			}
			resourceID += request.PathValue(name)
		}
		outcome, errorCode := audittrail.OutcomeSuccess, ""
		if recorder.status >= 400 {
			outcome, errorCode = audittrail.OutcomeFailure, strconv.Itoa(recorder.status)
		}
		event := audittrail.Event{
			CorrelationID: correlationID, ActorType: actorType, ActorID: actorID,
			OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
			Action: method, ResourceType: resourceType, ResourceID: resourceID,
			Outcome: outcome, ErrorCode: errorCode,
			SourceIP: sourceIP(request), UserAgent: request.UserAgent(),
		}
		if err := service.Record(request.Context(), event); err != nil {
			// Audit is best-effort at this layer (see the package doc comment
			// in internal/audittrail on the same-transaction boundary); a
			// write failure here must never take the API down.
			_ = err
		}
	})
}

func sourceIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}
```

Add `"net"`, `"strconv"`, and `"github.com/gokayybaz/bazusop/internal/tenancy"` to `internal/server/middleware.go`'s imports.

- [x] **Step 4: Run the test to verify it passes**

Run: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run TestRegisterAudited -v`
Expected: PASS (2 tests)

- [x] **Step 5: Add `WithAuditTrail` and the `auditTrail` field**

In `internal/server/server.go`, add to imports: `"github.com/gokayybaz/bazusop/internal/audittrail"`.

Add to `handlerOptions`:

```go
	auditTrail *audittrail.Service
```

Add a new option function, next to `WithDefaultScope`:

```go
func WithAuditTrail(service *audittrail.Service) Option {
	return func(options *handlerOptions) {
		options.auditTrail = service
	}
}
```

- [x] **Step 6: Replace every route registration in `NewHandler` with `registerAudited`**

In `internal/server/server.go`, replace the full body of `NewHandler` from the `mux := http.NewServeMux()` line through the `return mux` line with:

```go
	mux := http.NewServeMux()
	registerAudited(mux, "/api/v1/health", http.MethodGet, "health", nil, configuration.auditTrail, configuration.scope, handleHealth)
	if configuration.runtimeConfiguration != nil {
		registerAudited(mux, "/api/v1/system/configuration", http.MethodGet, "system_configuration", nil, configuration.auditTrail, configuration.scope, handleRuntimeConfiguration(*configuration.runtimeConfiguration))
	}
	if configuration.enrollmentAuthority != nil {
		registerAudited(mux, "/api/v1/agents/enroll", http.MethodPost, "enrollment", nil, configuration.auditTrail, configuration.scope, handleEnroll(configuration.enrollmentAuthority))
		registerAudited(mux, "/api/v1/agents/renew", http.MethodPost, "enrollment_renewal", nil, configuration.auditTrail, configuration.scope, handleRenew(configuration.enrollmentAuthority))
	}
	if configuration.inventoryService != nil {
		registerAudited(mux, "/api/v1/instances", http.MethodGet, "inventory", nil, configuration.auditTrail, configuration.scope, handleListInstances(configuration.inventoryService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			registerAudited(mux, "/api/v1/agents/inventory", http.MethodPut, "inventory", nil, configuration.auditTrail, configuration.scope, handleInventoryReport(configuration.enrollmentAuthority, configuration.inventoryService))
		}
	}
	if configuration.telemetryService != nil {
		registerAudited(mux, "/api/v1/instances/{agentID}/telemetry", http.MethodGet, "telemetry", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleTelemetryHistory(configuration.telemetryService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			registerAudited(mux, "/api/v1/agents/telemetry", http.MethodPost, "telemetry", nil, configuration.auditTrail, configuration.scope, handleTelemetryReport(configuration.enrollmentAuthority, configuration.telemetryService, configuration.alertService))
		}
	}
	if configuration.serviceInventory != nil {
		registerAudited(mux, "/api/v1/instances/{agentID}/services", http.MethodGet, "services", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleListServices(configuration.serviceInventory, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			registerAudited(mux, "/api/v1/agents/services", http.MethodPut, "services", nil, configuration.auditTrail, configuration.scope, handleServiceReport(configuration.enrollmentAuthority, configuration.serviceInventory))
		}
	}
	if configuration.logService != nil {
		registerAudited(mux, "/api/v1/instances/{agentID}/logs", http.MethodGet, "logs", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleSearchLogs(configuration.logService, configuration.scope))
		registerAudited(mux, "/api/v1/instances/{agentID}/logs/stream", http.MethodGet, "log_stream", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleStreamLogs(configuration.logService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			registerAudited(mux, "/api/v1/agents/logs", http.MethodPost, "logs", nil, configuration.auditTrail, configuration.scope, handleLogIngest(configuration.enrollmentAuthority, configuration.logService))
		}
	}
	if configuration.jobService != nil {
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		registerAudited(mux, "/api/v1/instances/{agentID}/jobs", http.MethodPost, "jobs", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleCreateJob(configuration.jobService, tokens, configuration.scope))
		registerAudited(mux, "/api/v1/instances/{agentID}/jobs", http.MethodGet, "jobs", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleListJobs(configuration.jobService, configuration.scope))
		registerAudited(mux, "/api/v1/instances/{agentID}/jobs/{jobID}/events", http.MethodGet, "job_events", []string{"agentID", "jobID"}, configuration.auditTrail, configuration.scope, handleJobEvents(configuration.jobService, configuration.scope))
		if configuration.enrollmentAuthority != nil {
			registerAudited(mux, "/api/v1/agents/jobs/next", http.MethodGet, "jobs", nil, configuration.auditTrail, configuration.scope, handleClaimJob(configuration.enrollmentAuthority, configuration.jobService))
			registerAudited(mux, "/api/v1/agents/jobs/{jobID}/events", http.MethodPost, "job_events", []string{"jobID"}, configuration.auditTrail, configuration.scope, handleReportJobEvent(configuration.enrollmentAuthority, configuration.jobService))
		}
	}
	if configuration.alertService != nil {
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		registerAudited(mux, "/api/v1/alert-rules", http.MethodGet, "alert_rules", nil, configuration.auditTrail, configuration.scope, handleListAlertRules(configuration.alertService, configuration.scope))
		registerAudited(mux, "/api/v1/alert-rules", http.MethodPost, "alert_rules", nil, configuration.auditTrail, configuration.scope, handleCreateAlertRule(configuration.alertService, tokens, configuration.scope))
		registerAudited(mux, "/api/v1/maintenance-windows", http.MethodGet, "maintenance_windows", nil, configuration.auditTrail, configuration.scope, handleListMaintenance(configuration.alertService, configuration.scope))
		registerAudited(mux, "/api/v1/maintenance-windows", http.MethodPost, "maintenance_windows", nil, configuration.auditTrail, configuration.scope, handleCreateMaintenance(configuration.alertService, tokens, configuration.scope))
		registerAudited(mux, "/api/v1/incidents", http.MethodGet, "incidents", nil, configuration.auditTrail, configuration.scope, handleListIncidents(configuration.alertService, configuration.scope))
		registerAudited(mux, "/api/v1/incidents/{incidentID}/events", http.MethodGet, "incident_events", []string{"incidentID"}, configuration.auditTrail, configuration.scope, handleAlertEvents(configuration.alertService, configuration.scope))
		registerAudited(mux, "/api/v1/incidents/{incidentID}/acknowledge", http.MethodPost, "incidents", []string{"incidentID"}, configuration.auditTrail, configuration.scope, handleAcknowledgeIncident(configuration.alertService, tokens, configuration.scope))
	}
	if configuration.cloudInventory != nil {
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		registerAudited(mux, "/api/v1/cloud/accounts", http.MethodGet, "cloud_accounts", nil, configuration.auditTrail, configuration.scope, handleListCloudAccounts(configuration.cloudInventory, configuration.scope))
		registerAudited(mux, "/api/v1/cloud/accounts", http.MethodPost, "cloud_accounts", nil, configuration.auditTrail, configuration.scope, handleCreateCloudAccount(configuration.cloudInventory, tokens, configuration.scope))
		registerAudited(mux, "/api/v1/cloud/instances", http.MethodGet, "cloud_instances", nil, configuration.auditTrail, configuration.scope, handleListCloudInstances(configuration.cloudInventory, configuration.scope))
		registerAudited(mux, "/api/v1/cloud/accounts/{accountID}/instances", http.MethodPut, "cloud_instances", []string{"accountID"}, configuration.auditTrail, configuration.scope, handleReconcileCloudInstances(configuration.cloudInventory, tokens, configuration.scope))
	}
	if configuration.auditService != nil {
		registerAudited(mux, "/api/v1/audit/events", http.MethodGet, "audit_timeline", nil, configuration.auditTrail, configuration.scope, handleListAuditEvents(configuration.auditService, configuration.scope))
	}
	mux.HandleFunc("/api/", func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	})
	mux.Handle("/", webui.Handler())
	return mux
```

Note `configuration.auditService` (the *existing*, unrelated spike-12 job/alert timeline feature) versus `configuration.auditTrail` (this spike's request-audit service, added in Step 5) are two different fields used side by side in the block above — this is intentional, not a typo.

- [x] **Step 7: Update the existing spike-12 audit-timeline test to pass an audit trail service**

In `internal/server/audit_test.go`, the line `handler := server.NewHandler(server.WithAudit(auditService))` becomes:

```go
	handler := server.NewHandler(server.WithAudit(auditService), server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())))
```

Add `"github.com/gokayybaz/bazusop/internal/audittrail"` to that file's imports.

- [x] **Step 8: Run the full server package test suite**

Run: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -v 2>&1 | tail -80`
Expected: every test PASSES, including the pre-existing ones (`TestHealthEndpoint`, `TestApprovedJobFlowsToAuthenticatedAgent`, `TestAuditEventsMergeJobAndAlertHistoryOrderedByRecency`, etc.) — route *behavior* is unchanged, only wrapped.

- [x] **Step 9: Wire construction into `cmd/bazusop-hub/main.go`**

Add to imports: `"github.com/gokayybaz/bazusop/internal/audittrail"`.

Near the other memory-store declarations (next to `var cloudInventoryStore cloudinventory.Store = cloudinventory.NewMemoryStore()`), add:

```go
	var auditTrailStore audittrail.Store = audittrail.NewMemoryStore()
```

Inside the `if configuration.DatabaseURL != "" { ... }` block, alongside the other `xStore = postgresStore` assignments, add:

```go
		auditTrailStore = postgresStore
```

After `auditService := audit.NewService(auditStore)`, add:

```go
	auditTrailService := audittrail.NewService(auditTrailStore)
```

In the `server.NewHandler(...)` call, add `server.WithAuditTrail(auditTrailService),` next to the existing `server.WithAudit(auditService),` line.

- [x] **Step 10: Build and run the full test suite**

Run: `go build ./... && go vet ./... && gofmt -l . 2>&1 | grep -v node_modules && GOCACHE=/tmp/bazusop-go-cache go test ./...`
Expected: build succeeds; vet/gofmt produce no output; every package `ok`

- [x] **Step 11: Commit**

```bash
git add internal/server/middleware.go internal/server/middleware_test.go internal/server/server.go internal/server/audit_test.go cmd/bazusop-hub/main.go
git commit -m "feat: record an audit trail event for every hub API request"
```

## Task 5: Manual verification against a live hub

**Files:** none (verification only).

- [x] **Step 1: Bring up a local hub with PostgreSQL**

Run: `POSTGRES_PASSWORD=verify-pw BAZUSOP_ENROLLMENT_TOKEN=verify-token BAZUSOP_OPERATOR_TOKEN=verify-operator BAZUSOP_ADMIN_TOKEN=verify-admin BAZUSOP_PORT=18091 docker compose up --build -d` and wait for `docker inspect --format '{{.State.Health.Status}}' bazusop-hub-1` to report `healthy`.

- [x] **Step 2: Generate a mix of successful and failing requests**

```bash
curl -s http://127.0.0.1:18091/api/v1/health
curl -s http://127.0.0.1:18091/api/v1/instances
curl -s -X POST http://127.0.0.1:18091/api/v1/alert-rules -d '{}'   # expect 401 — no Authorization header
curl -s -H "Authorization: Bearer wrong-token" -X POST http://127.0.0.1:18091/api/v1/alert-rules -d '{}'   # expect 401 — wrong token
```

- [x] **Step 3: Query the audit table directly and confirm the shape**

```bash
docker compose exec postgres psql -U bazusop -d bazusop -c \
  "SELECT occurred_at, actor_type, action, resource_type, outcome, error_code FROM audit_events ORDER BY occurred_at DESC LIMIT 10;"
```

Expected: one row per request above, `actor_type` is `anonymous` for the plain `/health` and `/instances` calls and `legacy_token` for the two `alert-rules` calls, `outcome`/`error_code` reflect `401` on the two failing calls, and an `UPDATE`/`DELETE` against the table from `psql` is rejected by the trigger (spot-check with `DELETE FROM audit_events;` — expect an error, not `DELETE 0`).

- [x] **Step 4: Tear down**

Run: `docker compose down -v`

## Self-Review

**1. Spec coverage.** Cross-checked against "Ortak istek bağlamı" and "Audit" sections:
- Actor/Scope common request context → Task 3 (`deriveActor`) + Task 4 (`registerAudited` reads `configuration.scope`). Human/service actor types are explicitly out of scope for 11.2 (see Global Constraints) since identity/sessions/service-accounts don't exist until 11.3/11.4/11.6 — flagged, not silently dropped.
- Server-generated `correlation_id` per request, client-supplied IDs never trusted as identity → Task 3/4 (`newCorrelationID` is always server-side; nothing in this plan reads a client-supplied request-ID header at all, which is a stricter reading of the spec than needed but avoids the spec's warned-against failure mode by construction).
- Full field list (`event_id` … `redacted_change_summary`) → Task 1 `Event` struct and Task 2 migration, 1:1.
- "Tüm okuma ve mutasyon API istekleri" audited → Task 4 wraps every route in `NewHandler` except static asset serving and the catch-all 404 (justified in Global Constraints).
- Append-only, no update/delete API → Task 2 migration trigger; Task 2 test step asserts both are rejected.
- Redaction discipline ("sonradan regex ile temizlemeye güvenilmez") → Global Constraints: this spike never captures a request/response body at all, so there is nothing to redact incorrectly.
- Same-transaction / rollback-on-audit-failure for security-critical mutations → explicitly deferred to 11.3+ in Global Constraints and the package doc comment, since no security-critical mutation (user/session/role) exists yet to require it.

**2. Placeholder scan.** No `TBD`/`TODO`/"add error handling" instances; every step has real code or a real, runnable command.

**3. Type consistency.** `audittrail.Event`, `audittrail.ActorType`, `audittrail.Outcome`, `audittrail.Store`, `audittrail.Service` are defined once in Task 1 and used with identical names/signatures in Tasks 2–4. `registerAudited`'s signature is defined once in Task 4 Step 3 and every call site in Task 4 Step 6 matches it positionally. `WithAuditTrail` is defined in Task 4 Step 5 and used in Task 4 Steps 7 and 9 with matching signatures.

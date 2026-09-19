package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var isolatedSchemaPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

type isolatedPostgres struct {
	schema    string
	scopedURL string
	admin     *pgxpool.Pool
	scoped    *pgxpool.Pool
	stores    []*Store
}

func newIsolatedPostgres(t *testing.T) *isolatedPostgres {
	t.Helper()
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL integration URL: %v", err)
	}
	admin, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatalf("open PostgreSQL integration pool: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatalf("ping PostgreSQL integration database: %v", err)
	}

	schema := newIntegrationSchema(t)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoteIsolatedIdentifier(schema)); err != nil {
		admin.Close()
		t.Fatalf("create isolated schema %q: %v", schema, err)
	}
	database := &isolatedPostgres{schema: schema, admin: admin}
	t.Cleanup(func() {
		for index := len(database.stores) - 1; index >= 0; index-- {
			database.stores[index].Close()
		}
		if database.scoped != nil {
			database.scoped.Close()
		}
		if err := dropIsolatedSchema(context.Background(), database.admin, database.schema); err != nil {
			t.Errorf("drop isolated schema %q: %v", database.schema, err)
		}
		database.admin.Close()
	})
	scopedURL := withSearchPath(t, databaseURL, schema)
	scopedConfiguration, err := pgxpool.ParseConfig(scopedURL)
	if err != nil {
		t.Fatalf("parse isolated PostgreSQL URL: %v", err)
	}
	if got := scopedConfiguration.ConnConfig.RuntimeParams["search_path"]; got != schema {
		t.Fatalf("isolated PostgreSQL search_path=%q, want %q", got, schema)
	}
	scoped, err := pgxpool.NewWithConfig(ctx, scopedConfiguration)
	if err != nil {
		t.Fatalf("open isolated PostgreSQL pool: %v", err)
	}
	if err := scoped.Ping(ctx); err != nil {
		scoped.Close()
		t.Fatalf("ping isolated PostgreSQL pool: %v", err)
	}

	database.scopedURL, database.scoped = scopedURL, scoped
	return database
}

func (database *isolatedPostgres) Open(t *testing.T) *Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := Open(ctx, database.scopedURL)
	if err != nil {
		t.Fatalf("open isolated store: %v", err)
	}
	database.stores = append(database.stores, store)
	return store
}

func newIntegrationSchema(t *testing.T) string {
	t.Helper()
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatalf("generate isolated schema suffix: %v", err)
	}
	schema := "bazusop_test_" + hex.EncodeToString(random[:])
	if !isolatedSchemaPattern.MatchString(schema) {
		t.Fatalf("generated unsafe isolated schema name %q", schema)
	}
	return schema
}

func withSearchPath(t *testing.T, databaseURL, schema string) string {
	t.Helper()
	if !isolatedSchemaPattern.MatchString(schema) {
		t.Fatalf("refusing unsafe search_path schema %q", schema)
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Scheme == "" {
		t.Fatalf("BAZUSOP_TEST_DATABASE_URL must be a PostgreSQL URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func dropIsolatedSchema(ctx context.Context, admin *pgxpool.Pool, schema string) error {
	if !isolatedSchemaPattern.MatchString(schema) {
		return fmt.Errorf("refusing unsafe isolated schema name %q", schema)
	}
	_, err := admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+quoteIsolatedIdentifier(schema)+" CASCADE")
	return err
}

func quoteIsolatedIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func applyEmbeddedMigration(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	migration, err := migrationFiles.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	transaction, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin migration %s: %v", name, err)
	}
	defer func() { _ = transaction.Rollback(t.Context()) }()
	if _, err := transaction.Exec(t.Context(), string(migration)); err != nil {
		t.Fatalf("apply migration %s: %v", name, err)
	}
	if err := transaction.Commit(t.Context()); err != nil {
		t.Fatalf("commit migration %s: %v", name, err)
	}
}

func TestOrganizationSiteMigrationBackfillsExistingData(t *testing.T) {
	database := newIsolatedPostgres(t)
	for migration := 1; migration <= 14; migration++ {
		applyEmbeddedMigration(t, database.scoped, fmt.Sprintf("%03d_%s.sql", migration, migrationName(migration)))
	}
	insertPreOrganizationSiteRows(t, database.scoped)
	applyEmbeddedMigration(t, database.scoped, "015_organization_sites.sql")
	var firstAgent string
	if err := database.scoped.QueryRow(t.Context(), `SELECT id FROM agents WHERE id='migration-agent'`).Scan(&firstAgent); err != nil {
		t.Fatal(err)
	}
	applyEmbeddedMigration(t, database.scoped, "015_organization_sites.sql")
	applyEmbeddedMigration(t, database.scoped, "016_scoped_service_log_keys.sql")
	applyEmbeddedMigration(t, database.scoped, "017_scoped_alerting_active_index.sql")

	var organizations, sites, agents int
	if err := database.scoped.QueryRow(t.Context(), `SELECT count(*) FROM organizations WHERE id='org_default'`).Scan(&organizations); err != nil {
		t.Fatal(err)
	}
	if err := database.scoped.QueryRow(t.Context(), `SELECT count(*) FROM sites WHERE id='site_default' AND organization_id='org_default'`).Scan(&sites); err != nil {
		t.Fatal(err)
	}
	if err := database.scoped.QueryRow(t.Context(), `SELECT count(*) FROM agents WHERE id='migration-agent' AND organization_id='org_default' AND site_id='site_default'`).Scan(&agents); err != nil {
		t.Fatal(err)
	}
	if organizations != 1 || sites != 1 || agents != 1 {
		t.Fatalf("replayed migration changed defaults: organizations=%d sites=%d agents=%d", organizations, sites, agents)
	}

	for _, table := range []string{
		"hosts", "enrollment_tokens", "telemetry_samples", "services", "log_entries", "jobs", "job_events",
		"alert_rules", "maintenance_windows", "alert_incidents", "alert_events", "cloud_accounts", "cloud_instances",
	} {
		var missing int
		query := fmt.Sprintf("SELECT count(*) FROM %s WHERE organization_id IS NULL OR site_id IS NULL", table)
		if err := database.scoped.QueryRow(t.Context(), query).Scan(&missing); err != nil {
			t.Fatalf("check %s scope backfill: %v", table, err)
		}
		if missing != 0 {
			t.Fatalf("%s has %d rows without scope", table, missing)
		}
	}
	var secondAgent string
	if err := database.scoped.QueryRow(t.Context(), `SELECT id FROM agents WHERE id='migration-agent'`).Scan(&secondAgent); err != nil {
		t.Fatal(err)
	}
	if firstAgent != secondAgent {
		t.Fatalf("replayed migration changed agent identity: %q != %q", firstAgent, secondAgent)
	}
}

func migrationName(migration int) string {
	names := map[int]string{
		1: "hosts", 2: "hosts_last_seen", 3: "telemetry_samples", 4: "telemetry_lookup", 5: "services",
		6: "services_lookup", 7: "log_entries", 8: "log_entries_lookup", 9: "jobs", 10: "job_events_lookup",
		11: "alerting", 12: "alerting_lookup", 13: "cloud_inventory", 14: "enrollment_state",
	}
	return names[migration]
}

func insertPreOrganizationSiteRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(t.Context(), `INSERT INTO hosts (agent_id, hostname, os_family, os_name, os_version, architecture, kernel_version, cpu_cores, memory_bytes, ip_addresses, agent_version, first_seen_at, last_seen_at) VALUES ('migration-agent', 'migration-host', 'linux', 'Linux', '1', 'amd64', 'kernel', 2, 1024, ARRAY['10.0.0.10'], 'test', $1, $1)`, at); err != nil {
		t.Fatalf("insert legacy host: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO telemetry_samples (agent_id, recorded_at, cpu_percent, memory_percent, disk_percent, network_rx_bytes, network_tx_bytes) VALUES ('migration-agent', $1, 20, 30, 40, 10, 20)`, at); err != nil {
		t.Fatalf("insert legacy telemetry: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO services (agent_id, name, display_name, state, startup_type, observed_at) VALUES ('migration-agent', 'migration.service', 'Migration service', 'running', 'automatic', $1)`, at); err != nil {
		t.Fatalf("insert legacy service: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO log_entries (id, agent_id, occurred_at, collector, source, severity, message) VALUES ('0123456789abcdef0123456789abcdef', 'migration-agent', $1, 'file', 'migration.log', 'info', 'legacy log')`, at); err != nil {
		t.Fatalf("insert legacy log: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO jobs (id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key) VALUES ('migration-job', 'migration-agent', 'service.restart', 'migration.service', 'migration-test', 'legacy job', $1, 'queued', 0, 'signature', 'public-key')`, at); err != nil {
		t.Fatalf("insert legacy job: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO job_events (job_id, sequence, type, message, actor, occurred_at) VALUES ('migration-job', 0, 'approved', 'legacy job', 'migration-test', $1)`, at); err != nil {
		t.Fatalf("insert legacy job event: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO alert_rules (id, name, kind, metric, threshold, stale_after_seconds, severity, enabled, created_at) VALUES ('migration-rule', 'Migration rule', 'metric', 'cpu', 80, 0, 'warning', TRUE, $1)`, at); err != nil {
		t.Fatalf("insert legacy alert rule: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO maintenance_windows (id, name, agent_id, starts_at, ends_at, created_by, created_at) VALUES ('migration-window', 'Migration window', 'migration-agent', $1, $2, 'migration-test', $1)`, at, at.Add(time.Hour)); err != nil {
		t.Fatalf("insert legacy maintenance window: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO alert_incidents (id, rule_id, rule_name, agent_id, severity, status, message, latest_value, opened_at, acknowledged_by) VALUES ('migration-incident', 'migration-rule', 'Migration rule', 'migration-agent', 'warning', 'open', 'legacy incident', 90, $1, '')`, at); err != nil {
		t.Fatalf("insert legacy alert incident: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO alert_events (id, incident_id, type, actor, message, occurred_at) VALUES ('migration-alert-event', 'migration-incident', 'opened', 'migration-test', 'legacy incident', $1)`, at); err != nil {
		t.Fatalf("insert legacy alert event: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO cloud_accounts (id, name, provider, external_id, status, created_at) VALUES ('migration-account', 'Migration account', 'aws', 'migration-external', 'connected', $1)`, at); err != nil {
		t.Fatalf("insert legacy cloud account: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO cloud_instances (account_id, provider_instance_id, name, region, zone, state, os_family, private_ips, public_ips, agent_id_hint, metadata, agent_id, candidate_agent_id, match_status, match_reason, discovered_at) VALUES ('migration-account', 'migration-instance', 'Migration instance', 'region', 'zone', 'running', 'linux', ARRAY['10.0.0.11'], ARRAY[]::TEXT[], 'migration-agent', '{}'::jsonb, NULL, NULL, 'verified', 'legacy match', $1)`, at); err != nil {
		t.Fatalf("insert legacy cloud instance: %v", err)
	}
	tokenHash := sha256.Sum256([]byte("migration-token"))
	if _, err := pool.Exec(t.Context(), `INSERT INTO enrollment_tokens (token_hash) VALUES ($1)`, tokenHash[:]); err != nil {
		t.Fatalf("insert legacy enrollment token: %v", err)
	}
}

func TestPostgresAgentMovePreservesScopedHistoryAndRefreshesIdentity(t *testing.T) {
	database := newIsolatedPostgres(t)
	store := database.Open(t)
	ctx := t.Context()
	suffix := hex.EncodeToString(make([]byte, 4)) + time.Now().UTC().Format("150405.000000000")
	org := "move-org-" + suffix
	otherOrg := "other-org-" + suffix
	scopeA := tenancy.Scope{OrganizationID: org, SiteID: "move-site-a-" + suffix}
	scopeB := tenancy.Scope{OrganizationID: org, SiteID: "move-site-b-" + suffix}
	scopeC := tenancy.Scope{OrganizationID: org, SiteID: "move-site-c-" + suffix}
	wrongScope := tenancy.Scope{OrganizationID: otherOrg, SiteID: "other-site-" + suffix}
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, $2), ($3, $4)`, org, "Move test", otherOrg, "Other test"); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []tenancy.Scope{scopeA, scopeB, scopeC, wrongScope} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1, $2, $1, $1)`, scope.SiteID, scope.OrganizationID); err != nil {
			t.Fatal(err)
		}
	}

	token := "move-token-" + suffix
	authority, err := enrollment.NewPersistentAuthority(ctx, token, scopeA, store)
	if err != nil {
		t.Fatalf("create persistent enrollment authority: %v", err)
	}
	identity, err := authority.EnrollContext(ctx, enrollment.Request{BootstrapToken: token, Name: "move-agent", OperatingSystem: "linux", CSRPEM: integrationCSR(t)})
	if err != nil {
		t.Fatalf("enroll move agent: %v", err)
	}
	agentA, err := store.ResolveAgent(ctx, identity.AgentID)
	if err != nil || agentA.Scope() != scopeA {
		t.Fatalf("enrolled agent scope: %#v, %v", agentA, err)
	}
	hosts := inventory.NewService(store)
	metrics := telemetry.NewService(store)
	logs := logstream.NewService(store)
	jobService, err := jobs.NewService(store, jobs.WithHostScopeChecker(hosts.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	alertService, err := alerting.NewService(store, alerting.WithHostScopeChecker(hosts.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	oldAt := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	newAt := oldAt.Add(time.Minute)
	facts := inventory.Facts{Hostname: "move-host", OSFamily: "linux", OSName: "Linux", OSVersion: "1", Architecture: "amd64", KernelVersion: "kernel", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.10"}, AgentVersion: "test"}
	if err := hosts.Report(ctx, agentA, facts); err != nil {
		t.Fatalf("report old host: %v", err)
	}
	if err := metrics.Report(ctx, agentA, telemetry.Sample{RecordedAt: oldAt, CPUPercent: 90, MemoryPercent: 30, DiskPercent: 40, NetworkRXBytes: 1, NetworkTXBytes: 2}); err != nil {
		t.Fatalf("report old telemetry: %v", err)
	}
	oldLogID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := logs.Ingest(ctx, agentA, logstream.Batch{Entries: []logstream.Entry{{ID: oldLogID, OccurredAt: oldAt, Collector: logstream.CollectorFile, Source: "move.log", Severity: logstream.SeverityWarn, Message: "site A log"}}}); err != nil {
		t.Fatalf("ingest old log: %v", err)
	}
	oldJob, err := jobService.Create(ctx, scopeA, agentA.ID, jobs.CreateRequest{Action: jobs.ActionServiceRestart, Target: "move.service", ApprovedBy: "ops", Reason: "site A job"})
	if err != nil {
		t.Fatalf("create old job: %v", err)
	}
	if _, err := jobService.ClaimNext(ctx, agentA); err != nil {
		t.Fatalf("claim old job: %v", err)
	}
	if _, err := jobService.Report(ctx, agentA, oldJob.ID, jobs.EventRequest{Sequence: 2, Type: jobs.EventSucceeded, Message: "site A complete"}); err != nil {
		t.Fatalf("record old job event: %v", err)
	}
	oldRule, err := alertService.CreateRule(ctx, scopeA, alerting.RuleRequest{Name: "move CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 80, Severity: alerting.SeverityWarning, Enabled: true})
	if err != nil {
		t.Fatalf("create old alert rule: %v", err)
	}
	if err := alertService.EvaluateTelemetry(ctx, scopeA, agentA.ID, alerting.Telemetry{CPUPercent: 90, MemoryPercent: 10, DiskPercent: 10}); err != nil {
		t.Fatalf("create old incident: %v", err)
	}
	oldIncidents, err := alertService.ListIncidents(ctx, scopeA, 10)
	if err != nil || len(oldIncidents) != 1 {
		t.Fatalf("old incidents: %#v, %v", oldIncidents, err)
	}
	oldIncidentID := oldIncidents[0].ID
	if oldIncidents[0].RuleID != oldRule.ID {
		t.Fatalf("old incident rule=%q, want %q", oldIncidents[0].RuleID, oldRule.ID)
	}

	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `UPDATE agents SET site_id=$3, moved_at=now() WHERE id=$1 AND organization_id=$2 AND site_id=$4`, agentA.ID, scopeA.OrganizationID, scopeB.SiteID, scopeA.SiteID); err != nil {
		t.Fatalf("move agent: %v", err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE hosts SET site_id=$3 WHERE agent_id=$1 AND organization_id=$2 AND site_id=$4`, agentA.ID, scopeA.OrganizationID, scopeB.SiteID, scopeA.SiteID); err != nil {
		t.Fatalf("move host: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	agentB, err := authority.AuthenticateContext(ctx, integrationCertificate(t, identity.CertificatePEM))
	if err != nil || agentB.Scope() != scopeB {
		t.Fatalf("resolve moved mTLS agent: %#v, %v", agentB, err)
	}
	if err := hosts.Report(ctx, agentB, facts); err != nil {
		t.Fatalf("report moved host: %v", err)
	}
	if err := metrics.Report(ctx, agentB, telemetry.Sample{RecordedAt: newAt, CPUPercent: 10, MemoryPercent: 20, DiskPercent: 30, NetworkRXBytes: 3, NetworkTXBytes: 4}); err != nil {
		t.Fatalf("report new telemetry: %v", err)
	}
	newLogID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := logs.Ingest(ctx, agentB, logstream.Batch{Entries: []logstream.Entry{{ID: newLogID, OccurredAt: newAt, Collector: logstream.CollectorFile, Source: "move.log", Severity: logstream.SeverityInfo, Message: "site B log"}}}); err != nil {
		t.Fatalf("ingest new log: %v", err)
	}
	newJob, err := jobService.Create(ctx, scopeB, agentB.ID, jobs.CreateRequest{Action: jobs.ActionServiceRestart, Target: "move.service", ApprovedBy: "ops", Reason: "site B job"})
	if err != nil {
		t.Fatalf("create new job: %v", err)
	}

	if values, err := metrics.History(ctx, scopeA, agentA.ID, oldAt.Add(-time.Second), newAt.Add(time.Second), 10); err != nil || len(values) != 1 || values[0].CPUPercent != 90 || values[0].SiteID != scopeA.SiteID {
		t.Fatalf("site A telemetry history: %#v, %v", values, err)
	}
	if values, err := logs.Search(ctx, logstream.Query{Scope: scopeA, AgentID: agentA.ID, From: oldAt.Add(-time.Second), To: newAt.Add(time.Second), Limit: 10}); err != nil || len(values) != 1 || values[0].ID != oldLogID || values[0].SiteID != scopeA.SiteID {
		t.Fatalf("site A log history: %#v, %v", values, err)
	}
	if values, err := jobService.List(ctx, scopeA, agentA.ID, 10); err != nil || len(values) != 1 || values[0].ID != oldJob.ID || values[0].SiteID != scopeA.SiteID {
		t.Fatalf("site A jobs: %#v, %v", values, err)
	}
	if values, err := jobService.Events(ctx, scopeA, agentA.ID, oldJob.ID); err != nil || len(values) < 3 || values[0].SiteID != scopeA.SiteID {
		t.Fatalf("site A job events: %#v, %v", values, err)
	}
	if values, err := alertService.ListIncidents(ctx, scopeA, 10); err != nil || len(values) != 1 || values[0].ID != oldIncidentID || values[0].SiteID != scopeA.SiteID {
		t.Fatalf("site A incidents: %#v, %v", values, err)
	}
	if values, err := alertService.ListEvents(ctx, scopeA, oldIncidentID); err != nil || len(values) != 1 || values[0].SiteID != scopeA.SiteID {
		t.Fatalf("site A incident events: %#v, %v", values, err)
	}

	if values, err := hosts.List(ctx, scopeB); err != nil || len(values) != 1 || values[0].AgentID != agentB.ID || values[0].SiteID != scopeB.SiteID {
		t.Fatalf("site B current host: %#v, %v", values, err)
	}
	if values, err := metrics.History(ctx, scopeB, agentB.ID, oldAt.Add(-time.Second), newAt.Add(time.Second), 10); err != nil || len(values) != 1 || values[0].CPUPercent != 10 || values[0].SiteID != scopeB.SiteID {
		t.Fatalf("site B telemetry history: %#v, %v", values, err)
	}
	if values, err := logs.Search(ctx, logstream.Query{Scope: scopeB, AgentID: agentB.ID, From: oldAt.Add(-time.Second), To: newAt.Add(time.Second), Limit: 10}); err != nil || len(values) != 1 || values[0].ID != newLogID || values[0].SiteID != scopeB.SiteID {
		t.Fatalf("site B logs: %#v, %v", values, err)
	}
	if values, err := jobService.List(ctx, scopeB, agentB.ID, 10); err != nil || len(values) != 1 || values[0].ID != newJob.ID || values[0].SiteID != scopeB.SiteID {
		t.Fatalf("site B jobs: %#v, %v", values, err)
	}
	if values, err := jobService.Events(ctx, scopeB, agentB.ID, newJob.ID); err != nil || len(values) != 1 || values[0].SiteID != scopeB.SiteID {
		t.Fatalf("site B job events: %#v, %v", values, err)
	}

	if values, err := jobService.List(ctx, scopeB, agentA.ID, 10); err != nil {
		t.Fatalf("site B old job query: %v", err)
	} else {
		for _, value := range values {
			if value.ID == oldJob.ID {
				t.Fatalf("site B read old job: %#v", value)
			}
		}
	}
	if _, err := jobService.Events(ctx, scopeB, agentA.ID, oldJob.ID); !errors.Is(err, jobs.ErrJobNotFound) {
		t.Fatalf("site B old job events error: %v", err)
	}
	if _, err := alertService.ListEvents(ctx, scopeB, oldIncidentID); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("site B old incident events error: %v", err)
	}
	if exists, err := hosts.HasHost(ctx, scopeA, agentA.ID); err != nil || exists {
		t.Fatalf("old site current host still visible: exists=%v err=%v", exists, err)
	}

	for _, wrong := range []tenancy.Scope{scopeC, wrongScope} {
		if values, err := metrics.History(ctx, wrong, agentA.ID, oldAt.Add(-time.Second), oldAt.Add(time.Second), 10); err != nil || len(values) != 0 {
			t.Fatalf("wrong scope telemetry leaked for %#v: %#v, %v", wrong, values, err)
		}
		if values, err := logs.Search(ctx, logstream.Query{Scope: wrong, AgentID: agentA.ID, From: oldAt.Add(-time.Second), To: oldAt.Add(time.Second), Limit: 10}); err != nil || len(values) != 0 {
			t.Fatalf("wrong scope logs leaked for %#v: %#v, %v", wrong, values, err)
		}
		if values, err := jobService.List(ctx, wrong, agentA.ID, 10); err != nil || len(values) != 0 {
			t.Fatalf("wrong scope jobs leaked for %#v: %#v, %v", wrong, values, err)
		}
		if values, err := alertService.ListIncidents(ctx, wrong, 10); err != nil || len(values) != 0 {
			t.Fatalf("wrong scope incidents leaked for %#v: %#v, %v", wrong, values, err)
		}
		if exists, err := hosts.HasHost(ctx, wrong, agentA.ID); err != nil || exists {
			t.Fatalf("wrong scope host leaked for %#v: exists=%v err=%v", wrong, exists, err)
		}
		if _, err := jobService.Events(ctx, wrong, agentA.ID, oldJob.ID); !errors.Is(err, jobs.ErrJobNotFound) {
			t.Fatalf("wrong scope job events error for %#v: %v", wrong, err)
		}
		if _, err := alertService.ListEvents(ctx, wrong, oldIncidentID); !errors.Is(err, alerting.ErrNotFound) {
			t.Fatalf("wrong scope incident events error for %#v: %v", wrong, err)
		}
		var ignored string
		err := store.pool.QueryRow(ctx, `SELECT id FROM agents WHERE id=$1 AND organization_id=$2 AND site_id=$3`, agentA.ID, wrong.OrganizationID, wrong.SiteID).Scan(&ignored)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("wrong scope agent query returned %q, err=%v", ignored, err)
		}
	}
}

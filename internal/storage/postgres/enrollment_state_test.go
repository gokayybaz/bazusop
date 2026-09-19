package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/tenancy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These fixtures exercise the SQL and transaction boundary without requiring a
// PostgreSQL server. Each operation checks query arguments and ordering.
type enrollmentSQLStep struct {
	sql    string
	args   []any
	values []any
	err    error
}

type enrollmentSQLDB struct {
	t *testing.T
	pgx.Tx
	steps    []enrollmentSQLStep
	beginErr error
}

func (db *enrollmentSQLDB) next(sql string, args []any) enrollmentSQLStep {
	db.t.Helper()
	if len(db.steps) == 0 {
		db.t.Fatalf("unexpected SQL operation: %s", sql)
	}
	step := db.steps[0]
	db.steps = db.steps[1:]
	if strings.Join(strings.Fields(sql), " ") != strings.Join(strings.Fields(step.sql), " ") || !reflect.DeepEqual(args, step.args) {
		db.t.Fatalf("SQL mismatch:\n got %s %v\nwant %s %v", sql, args, step.sql, step.args)
	}
	return step
}

func (db *enrollmentSQLDB) Begin(context.Context) (pgx.Tx, error) {
	db.next("BEGIN", nil)
	return db, db.beginErr
}
func (db *enrollmentSQLDB) Commit(context.Context) error   { return db.next("COMMIT", nil).err }
func (db *enrollmentSQLDB) Rollback(context.Context) error { return db.next("ROLLBACK", nil).err }
func (db *enrollmentSQLDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	step := db.next(sql, args)
	return pgconn.NewCommandTag("UPDATE 1"), step.err
}
func (db *enrollmentSQLDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	return enrollmentSQLRow{db.next(sql, args)}
}

type enrollmentSQLRow struct{ step enrollmentSQLStep }

func (row enrollmentSQLRow) Scan(dest ...any) error {
	if row.step.err != nil {
		return row.step.err
	}
	for i, value := range row.step.values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}
func newEnrollmentSQLDB(t *testing.T, steps ...enrollmentSQLStep) *enrollmentSQLDB {
	t.Helper()
	db := &enrollmentSQLDB{t: t, steps: steps}
	t.Cleanup(func() {
		if len(db.steps) != 0 {
			t.Errorf("%d SQL operations not executed: %s", len(db.steps), db.steps[0].sql)
		}
	})
	return db
}

func TestConsumeEnrollmentTokenLocksAndAtomicallyBindsAgent(t *testing.T) {
	hash := sha256.Sum256([]byte("token"))
	failure := errors.New("database failure")
	for _, scenario := range []string{"success", "missing", "revoked", "consumed", "read failure", "insert failure", "update failure", "commit failure", "begin failure"} {
		t.Run(scenario, func(t *testing.T) {
			steps := []enrollmentSQLStep{{sql: "BEGIN"}}
			wantErr := error(nil)
			if scenario == "begin failure" {
				db := newEnrollmentSQLDB(t, steps...)
				db.beginErr = failure
				if _, err := consumeEnrollmentToken(t.Context(), db, hash, "agent-1"); !errors.Is(err, failure) {
					t.Fatalf("begin error: %v", err)
				}
				return
			}
			read := enrollmentSQLStep{sql: `SELECT organization_id, site_id, consumed_at IS NOT NULL, revoked_at IS NOT NULL FROM enrollment_tokens WHERE token_hash = $1 FOR UPDATE`, args: []any{hash[:]}, values: []any{"org-a", "site-a", scenario == "consumed", scenario == "revoked"}}
			switch scenario {
			case "missing":
				read.err = pgx.ErrNoRows
				wantErr = enrollment.ErrInvalidToken
			case "revoked":
				wantErr = enrollment.ErrInvalidToken
			case "consumed":
				wantErr = enrollment.ErrTokenConsumed
			case "read failure":
				read.err = failure
				wantErr = failure
			}
			steps = append(steps, read)
			if wantErr == nil {
				insert := enrollmentSQLStep{sql: `INSERT INTO agents (id, organization_id, site_id) VALUES ($1, $2, $3)`, args: []any{"agent-1", "org-a", "site-a"}}
				if scenario == "insert failure" {
					insert.err = failure
					wantErr = failure
				}
				steps = append(steps, insert)
			}
			if wantErr == nil {
				update := enrollmentSQLStep{sql: `UPDATE enrollment_tokens SET consumed_at = now(), consumed_by_agent_id = $2 WHERE token_hash = $1`, args: []any{hash[:], "agent-1"}}
				if scenario == "update failure" {
					update.err = failure
					wantErr = failure
				}
				steps = append(steps, update)
			}
			if wantErr == nil {
				commit := enrollmentSQLStep{sql: "COMMIT"}
				if scenario == "commit failure" {
					commit.err = failure
					wantErr = failure
				}
				steps = append(steps, commit)
			}
			steps = append(steps, enrollmentSQLStep{sql: "ROLLBACK"})
			agent, err := consumeEnrollmentToken(t.Context(), newEnrollmentSQLDB(t, steps...), hash, "agent-1")
			if !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want %v", err, wantErr)
			}
			want := tenancy.Agent{ID: "agent-1", OrganizationID: "org-a", SiteID: "site-a"}
			if wantErr != nil {
				want = tenancy.Agent{}
			}
			if agent != want {
				t.Fatalf("agent = %#v, want %#v", agent, want)
			}
		})
	}
}

func TestRegisterEnrollmentTokenScopesRotation(t *testing.T) {
	hash := sha256.Sum256([]byte("new-token"))
	scope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	failure := errors.New("database failure")
	for _, scenario := range []string{"success", "already registered", "insert failure", "revoke failure", "commit failure"} {
		t.Run(scenario, func(t *testing.T) {
			insert := enrollmentSQLStep{sql: `INSERT INTO enrollment_tokens (token_hash, organization_id, site_id) VALUES ($1, $2, $3) ON CONFLICT (token_hash) DO NOTHING RETURNING token_hash`, args: []any{hash[:], "org-a", "site-a"}, values: []any{hash[:]}}
			wantErr := error(nil)
			if scenario == "already registered" {
				insert.err = pgx.ErrNoRows
			}
			if scenario == "insert failure" {
				insert.err = failure
				wantErr = failure
			}
			steps := []enrollmentSQLStep{{sql: "BEGIN"}, insert}
			if insert.err == nil {
				revoke := enrollmentSQLStep{sql: `UPDATE enrollment_tokens SET revoked_at = now() WHERE token_hash <> $1 AND organization_id = $2 AND site_id = $3 AND consumed_at IS NULL AND revoked_at IS NULL`, args: []any{hash[:], "org-a", "site-a"}}
				if scenario == "revoke failure" {
					revoke.err = failure
					wantErr = failure
				}
				steps = append(steps, revoke)
				if wantErr == nil {
					commit := enrollmentSQLStep{sql: "COMMIT"}
					if scenario == "commit failure" {
						commit.err = failure
						wantErr = failure
					}
					steps = append(steps, commit)
				}
			}
			steps = append(steps, enrollmentSQLStep{sql: "ROLLBACK"})
			if err := registerEnrollmentToken(t.Context(), newEnrollmentSQLDB(t, steps...), hash, scope); !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want %v", err, wantErr)
			}
		})
	}
}

func TestResolveAgentReadsPersistentScope(t *testing.T) {
	failure := errors.New("database failure")
	for _, rowErr := range []error{nil, pgx.ErrNoRows, failure} {
		db := newEnrollmentSQLDB(t, enrollmentSQLStep{sql: `SELECT id, organization_id, site_id FROM agents WHERE id = $1`, args: []any{"agent-1"}, values: []any{"agent-1", "org-a", "site-a"}, err: rowErr})
		agent, err := resolveAgent(t.Context(), db, "agent-1")
		wantErr := rowErr
		if rowErr == pgx.ErrNoRows {
			wantErr = tenancy.ErrNotFound
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
		want := tenancy.Agent{ID: "agent-1", OrganizationID: "org-a", SiteID: "site-a"}
		if rowErr != nil {
			want = tenancy.Agent{}
		}
		if agent != want {
			t.Fatalf("agent = %#v, want %#v", agent, want)
		}
	}
}

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/sessions"
)

func TestPostgresSessionLifecycle(t *testing.T) {
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
	orgID := "session-org-" + suffix
	userID := "session-user-" + suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Session test')`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO users (id, organization_id, email, role, password_hash, created_at) VALUES ($1,$2,'session-test@example.com','platform-admin','hash', now())`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM sessions WHERE organization_id=$1", orgID); err != nil {
			t.Errorf("cleanup sessions: %v", err)
		}
		if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM users WHERE id=$1", userID); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
		if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id=$1", orgID); err != nil {
			t.Errorf("cleanup organizations: %v", err)
		}
	}()

	now := time.Now().UTC()
	session := sessions.Session{
		ID: "session-" + suffix, UserID: userID, OrganizationID: orgID, CSRFToken: "csrf-" + suffix,
		CreatedAt: now, LastSeenAt: now, AbsoluteExpiresAt: now.Add(8 * time.Hour),
	}
	tokenHash := "token-hash-" + suffix
	if err := store.CreateSession(ctx, session, tokenHash); err != nil {
		t.Fatalf("create session: %v", err)
	}

	fetched, err := store.SessionByTokenHash(ctx, tokenHash)
	if err != nil || fetched.ID != session.ID || fetched.CSRFToken != session.CSRFToken {
		t.Fatalf("expected to read back the session, got %#v %v", fetched, err)
	}

	touchedAt := now.Add(5 * time.Minute)
	if err := store.Touch(ctx, session.ID, touchedAt); err != nil {
		t.Fatalf("touch: %v", err)
	}
	afterTouch, err := store.SessionByTokenHash(ctx, tokenHash)
	if err != nil || !afterTouch.LastSeenAt.Equal(touchedAt) {
		t.Fatalf("expected LastSeenAt to be updated, got %#v %v", afterTouch, err)
	}

	if err := store.RevokeSession(ctx, session.ID, now.Add(10*time.Minute)); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	revoked, err := store.SessionByTokenHash(ctx, tokenHash)
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("expected the session to be marked revoked, got %#v %v", revoked, err)
	}

	secondTokenHash := "second-token-hash-" + suffix
	secondSession := session
	secondSession.ID = "second-session-" + suffix
	if err := store.CreateSession(ctx, secondSession, secondTokenHash); err != nil {
		t.Fatalf("create second session: %v", err)
	}
	if err := store.RevokeAllForUser(ctx, userID, now.Add(15*time.Minute)); err != nil {
		t.Fatalf("revoke all for user: %v", err)
	}
	secondRevoked, err := store.SessionByTokenHash(ctx, secondTokenHash)
	if err != nil || secondRevoked.RevokedAt == nil {
		t.Fatalf("expected the second session to be revoked too, got %#v %v", secondRevoked, err)
	}

	thirdTokenHash := "third-token-hash-" + suffix
	thirdSession := session
	thirdSession.ID = "third-session-" + suffix
	if err := store.CreateSession(ctx, thirdSession, thirdTokenHash); err != nil {
		t.Fatalf("create third session: %v", err)
	}
	if err := store.RevokeAllForOrganization(ctx, orgID, now.Add(20*time.Minute)); err != nil {
		t.Fatalf("revoke all for organization: %v", err)
	}
	thirdRevoked, err := store.SessionByTokenHash(ctx, thirdTokenHash)
	if err != nil || thirdRevoked.RevokedAt == nil {
		t.Fatalf("expected the third session to be revoked too, got %#v %v", thirdRevoked, err)
	}
}

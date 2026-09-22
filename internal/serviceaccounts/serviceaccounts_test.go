package serviceaccounts_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
)

func newTestService(t *testing.T, now func() time.Time) *serviceaccounts.Service {
	t.Helper()
	service, err := serviceaccounts.NewService(serviceaccounts.NewMemoryStore(), "test-pepper", serviceaccounts.WithClock(now))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestCreateAccountProducesAOneTimeTokenThatValidates(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, token, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if account.Role != authorization.SiteRoleOperator || account.SiteID != "site_default" || token == "" {
		t.Fatalf("unexpected account: %#v (token=%q)", account, token)
	}

	validated, err := service.Validate(t.Context(), token, "10.0.0.1")
	if err != nil || validated.ID != account.ID {
		t.Fatalf("expected the fresh token to validate, got %#v %v", validated, err)
	}
}

func TestCreateAccountRejectsAPlatformAdminRole(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	if _, _, err := service.CreateAccount(t.Context(), "org_default", "site_default", "bad", "platform-admin", 0); !errors.Is(err, serviceaccounts.ErrInvalidRole) {
		t.Fatalf("expected ErrInvalidRole, got %v", err)
	}
}

func TestCreateAccountRejectsAnOutOfRangeExpiry(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	if _, _, err := service.CreateAccount(t.Context(), "org_default", "site_default", "bad", authorization.SiteRoleViewer, 400); !errors.Is(err, serviceaccounts.ErrInvalidExpiry) {
		t.Fatalf("expected ErrInvalidExpiry for 400 days, got %v", err)
	}
}

func TestValidateRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	if _, err := service.Validate(t.Context(), "bazusop_sat_deadbeef_deadbeef", "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestValidateRejectsAWrongSecretForARealTokenID(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	_, token, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokenID, _, ok := serviceaccounts.ParseToken(token)
	if !ok {
		t.Fatal("expected a parseable token")
	}
	forged := serviceaccounts.TokenPrefix + tokenID + "_" + "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := service.Validate(t.Context(), forged, "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenNotFound) {
		t.Fatalf("expected a forged secret to be rejected as ErrTokenNotFound, got %v", err)
	}
}

func TestValidateRejectsAnExpiredToken(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }
	service := newTestService(t, clock)
	_, token, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 1)
	if err != nil {
		t.Fatal(err)
	}
	current = current.AddDate(0, 0, 2)
	if _, err := service.Validate(t.Context(), token, "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestRotateTokenInvalidatesThePreviousOne(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, oldToken, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	newToken, err := service.RotateToken(t.Context(), "site_default", account.ID, 0)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newToken == oldToken {
		t.Fatal("expected a distinct new token")
	}
	if _, err := service.Validate(t.Context(), oldToken, "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenRevoked) {
		t.Fatalf("expected the old token to be revoked, got %v", err)
	}
	if _, err := service.Validate(t.Context(), newToken, "10.0.0.1"); err != nil {
		t.Fatalf("expected the new token to validate, got %v", err)
	}
}

func TestRevokeTokenRejectsFurtherUse(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, token, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokenID, _, _ := serviceaccounts.ParseToken(token)
	if err := service.RevokeToken(t.Context(), "site_default", tokenID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := service.Validate(t.Context(), token, "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenRevoked) {
		t.Fatalf("expected ErrTokenRevoked, got %v", err)
	}
	_ = account
}

func TestDisableAccountRejectsFurtherUseEvenWithAValidToken(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, token, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DisableAccount(t.Context(), "site_default", account.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := service.Validate(t.Context(), token, "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenRevoked) {
		t.Fatalf("expected DisableAccount to also revoke the active token (ErrTokenRevoked), got %v", err)
	}
}

func TestRotateTokenRejectsAnAccountFromAnotherSite(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, _, err := service.CreateAccount(t.Context(), "org_default", "site-a", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RotateToken(t.Context(), "site-b", account.ID, 0); !errors.Is(err, serviceaccounts.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound for a cross-site rotate attempt, got %v", err)
	}
}

func TestRevokeTokenRejectsATokenFromAnotherSite(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	_, token, err := service.CreateAccount(t.Context(), "org_default", "site-a", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokenID, _, _ := serviceaccounts.ParseToken(token)
	if err := service.RevokeToken(t.Context(), "site-b", tokenID); !errors.Is(err, serviceaccounts.ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound for a cross-site revoke attempt, got %v", err)
	}
	if _, err := service.Validate(t.Context(), token, "10.0.0.1"); err != nil {
		t.Fatalf("expected the token to remain valid after a rejected cross-site revoke, got %v", err)
	}
}

func TestDisableAccountRejectsAnAccountFromAnotherSite(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, token, err := service.CreateAccount(t.Context(), "org_default", "site-a", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DisableAccount(t.Context(), "site-b", account.ID); !errors.Is(err, serviceaccounts.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound for a cross-site disable attempt, got %v", err)
	}
	if _, err := service.Validate(t.Context(), token, "10.0.0.1"); err != nil {
		t.Fatalf("expected the account to remain active after a rejected cross-site disable, got %v", err)
	}
}

func TestListForSiteReturnsOnlyThatSitesAccounts(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	if _, _, err := service.CreateAccount(t.Context(), "org_default", "site-a", "a-bot", authorization.SiteRoleViewer, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateAccount(t.Context(), "org_default", "site-b", "b-bot", authorization.SiteRoleViewer, 0); err != nil {
		t.Fatal(err)
	}
	accounts, err := service.ListForSite(t.Context(), "site-a")
	if err != nil || len(accounts) != 1 || accounts[0].Name != "a-bot" {
		t.Fatalf("expected exactly one site-a account, got %#v %v", accounts, err)
	}
}

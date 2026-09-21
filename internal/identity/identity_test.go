package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/identity"
)

func newTestService() *identity.Service {
	return identity.NewService(identity.NewMemoryStore(), "test-totp-encryption-key")
}

func TestBootstrapCreatesThePlatformAdminExactlyOnce(t *testing.T) {
	t.Parallel()
	service := newTestService()

	user, enrollment, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if user.Role != identity.RolePlatformAdmin || user.Email != "admin@example.com" {
		t.Fatalf("unexpected bootstrapped user: %#v", user)
	}
	if enrollment.ProvisioningURI == "" || len(enrollment.RecoveryCodes) != 10 {
		t.Fatalf("expected a provisioning URI and 10 recovery codes, got %#v", enrollment)
	}

	if _, _, err := service.Bootstrap(t.Context(), "org_default", "second-admin@example.com", "another password entirely"); !errors.Is(err, identity.ErrAlreadyBootstrapped) {
		t.Fatalf("expected ErrAlreadyBootstrapped on a second call, got %v", err)
	}
}

func TestInviteLifecycleCreateConsumeConfirmTOTP(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}

	invite, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-admin@example.com", identity.RolePlatformAdmin, nil)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if invite.Email != "new-admin@example.com" || invite.Role != identity.RolePlatformAdmin || token == "" {
		t.Fatalf("unexpected invite: %#v (token=%q)", invite, token)
	}

	user, err := service.ConsumeInvite(t.Context(), token, "a brand new password")
	if err != nil {
		t.Fatalf("consume invite: %v", err)
	}
	if user.Email != "new-admin@example.com" || user.TOTPConfirmedAt != nil {
		t.Fatalf("expected an unconfirmed new user, got %#v", user)
	}

	if _, err := service.ConsumeInvite(t.Context(), token, "trying again"); err == nil {
		t.Fatal("expected a second consumption of the same token to fail")
	}

	now := time.Now().UTC()
	enrollment, err := service.ConfirmTOTP(t.Context(), user.ID, func(secret []byte) string { return identity.GenerateTOTPCode(secret, now) }, now)
	if err != nil {
		t.Fatalf("confirm TOTP: %v", err)
	}
	if len(enrollment.RecoveryCodes) != 10 {
		t.Fatalf("expected 10 recovery codes, got %#v", enrollment)
	}

	if _, err := service.ConfirmTOTP(t.Context(), user.ID, func([]byte) string { return "000000" }, now); !errors.Is(err, identity.ErrTOTPAlreadyConfirmed) {
		t.Fatalf("expected ErrTOTPAlreadyConfirmed on a second confirm, got %v", err)
	}
}

func TestVerifyCredentialsRequiresPasswordAndTOTP(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}

	if _, err := service.VerifyCredentials(t.Context(), "org_default", "admin@example.com", "wrong password", "000000"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for a wrong password, got %v", err)
	}
	if _, err := service.VerifyCredentials(t.Context(), "org_default", "admin@example.com", "correct horse battery staple", "000000"); !errors.Is(err, identity.ErrInvalidTOTPCode) {
		t.Fatalf("expected ErrInvalidTOTPCode for a wrong TOTP code, got %v", err)
	}
}

func TestVerifyCredentialsWithRecoveryCodeConsumesItOnce(t *testing.T) {
	t.Parallel()
	service := newTestService()
	_, enrollment, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	recoveryCode := enrollment.RecoveryCodes[0]

	if _, err := service.VerifyCredentialsWithRecoveryCode(t.Context(), "org_default", "admin@example.com", "correct horse battery staple", recoveryCode); err != nil {
		t.Fatalf("expected the recovery code to work once: %v", err)
	}
	if _, err := service.VerifyCredentialsWithRecoveryCode(t.Context(), "org_default", "admin@example.com", "correct horse battery staple", recoveryCode); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("expected the same recovery code to be rejected the second time, got %v", err)
	}
}

func TestUserByIDReturnsTheStoredUser(t *testing.T) {
	t.Parallel()
	service := newTestService()
	created, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	fetched, err := service.UserByID(t.Context(), created.ID)
	if err != nil || fetched.Email != "admin@example.com" {
		t.Fatalf("expected to read back the bootstrapped user, got %#v %v", fetched, err)
	}
}

func TestIsUserActiveReflectsTheStoredDisabledAtField(t *testing.T) {
	t.Parallel()
	store := identity.NewMemoryStore()
	service := identity.NewService(store, "test-totp-encryption-key")
	user, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	active, err := service.IsUserActive(t.Context(), user.ID)
	if err != nil || !active {
		t.Fatalf("expected a freshly bootstrapped user to be active, got %v %v", active, err)
	}

	disabledAt := time.Now().UTC()
	user.DisabledAt = &disabledAt
	if err := store.UpdateUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	active, err = service.IsUserActive(t.Context(), user.ID)
	if err != nil || active {
		t.Fatalf("expected a disabled user to be inactive, got %v %v", active, err)
	}
}

func TestIsUserActiveReturnsFalseForAnUnknownUserWithoutError(t *testing.T) {
	t.Parallel()
	service := newTestService()
	active, err := service.IsUserActive(t.Context(), "does-not-exist")
	if err != nil {
		t.Fatalf("expected no error for an unknown user, got %v", err)
	}
	if active {
		t.Fatal("expected an unknown user to be reported inactive")
	}
}

func TestIsPlatformAdminReflectsTheUsersRole(t *testing.T) {
	t.Parallel()
	service := newTestService()
	admin, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	isAdmin, err := service.IsPlatformAdmin(t.Context(), admin.ID)
	if err != nil || !isAdmin {
		t.Fatalf("expected the bootstrapped user to be a platform admin, got %v %v", isAdmin, err)
	}
	isAdmin, err = service.IsPlatformAdmin(t.Context(), "does-not-exist")
	if err != nil || isAdmin {
		t.Fatalf("expected an unknown user to not be a platform admin, got %v %v", isAdmin, err)
	}
}

func TestCreateInviteRejectsSiteRoleGrantsForAPlatformAdminInvite(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	_, _, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-admin@example.com", identity.RolePlatformAdmin, []identity.SiteRoleGrant{{SiteID: "site_default", Role: "site-admin"}})
	if !errors.Is(err, identity.ErrInvalidInviteRole) {
		t.Fatalf("expected ErrInvalidInviteRole, got %v", err)
	}
}

func TestCreateInviteRejectsASiteRoleInviteWithNoGrants(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	_, _, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-operator@example.com", "", nil)
	if !errors.Is(err, identity.ErrInvalidInviteRole) {
		t.Fatalf("expected ErrInvalidInviteRole, got %v", err)
	}
}

func TestConsumeInviteWithSiteRoleGrantsInvokesTheGrantor(t *testing.T) {
	t.Parallel()
	var grantedUserID string
	var grantedGrants []identity.SiteRoleGrant
	grantor := func(_ context.Context, userID string, grants []identity.SiteRoleGrant) error {
		grantedUserID, grantedGrants = userID, grants
		return nil
	}
	service := identity.NewService(identity.NewMemoryStore(), "test-totp-encryption-key", identity.WithSiteRoleGrantor(grantor))
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	grants := []identity.SiteRoleGrant{{SiteID: "site_default", Role: "operator"}}
	_, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-operator@example.com", "", grants)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	user, err := service.ConsumeInvite(t.Context(), token, "a brand new password")
	if err != nil {
		t.Fatalf("consume invite: %v", err)
	}
	if user.Role != "" {
		t.Fatalf("expected an empty identity role for a site-role invite, got %q", user.Role)
	}
	if grantedUserID != user.ID || len(grantedGrants) != 1 || grantedGrants[0] != grants[0] {
		t.Fatalf("expected the grantor to be invoked with the new user and grants, got %q %#v", grantedUserID, grantedGrants)
	}
}

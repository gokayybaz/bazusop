package identity_test

import (
	"context"
	"encoding/base32"
	"errors"
	"net/url"
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

	invite, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-admin@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeLocal)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if invite.Email != "new-admin@example.com" || invite.Role != identity.RolePlatformAdmin || token == "" {
		t.Fatalf("unexpected invite: %#v (token=%q)", invite, token)
	}

	user, consumeEnrollment, err := service.ConsumeInvite(t.Context(), token, "a brand new password")
	if err != nil {
		t.Fatalf("consume invite: %v", err)
	}
	if user.Email != "new-admin@example.com" || user.TOTPConfirmedAt != nil {
		t.Fatalf("expected an unconfirmed new user, got %#v", user)
	}
	if consumeEnrollment.ProvisioningURI == "" {
		t.Fatal("expected ConsumeInvite to return a non-empty provisioning URI")
	}

	if _, _, err := service.ConsumeInvite(t.Context(), token, "trying again"); err == nil {
		t.Fatal("expected a second consumption of the same token to fail")
	}

	now := time.Now().UTC()
	enrollment, err := service.ConfirmTOTP(t.Context(), user.ID, func(secret []byte) string { return identity.GenerateTOTPCode(secret, now) }, now)
	if err != nil {
		t.Fatalf("confirm TOTP: %v", err)
	}
	if len(enrollment.RecoveryCodes) != 10 || enrollment.ProvisioningURI == "" {
		t.Fatalf("expected 10 recovery codes and a non-empty provisioning URI, got %#v", enrollment)
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
	_, _, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-admin@example.com", identity.RolePlatformAdmin, []identity.SiteRoleGrant{{SiteID: "site_default", Role: "site-admin"}}, identity.IdentityTypeLocal)
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
	_, _, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-operator@example.com", "", nil, identity.IdentityTypeLocal)
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
	_, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-operator@example.com", "", grants, identity.IdentityTypeLocal)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	user, _, err := service.ConsumeInvite(t.Context(), token, "a brand new password")
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

func TestLoginOrLinkOIDCUserCreatesUserFromPendingInvite(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateInvite(t.Context(), "admin", "org_default", "sso-user@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeOIDC); err != nil {
		t.Fatalf("create invite: %v", err)
	}

	user, err := service.LoginOrLinkOIDCUser(t.Context(), "org_default", "https://idp.example.com", "subject-123", "sso-user@example.com")
	if err != nil {
		t.Fatalf("login or link: %v", err)
	}
	if user.Email != "sso-user@example.com" || user.OIDCIssuer != "https://idp.example.com" || user.OIDCSubject != "subject-123" {
		t.Fatalf("unexpected linked user: %#v", user)
	}
	if user.PasswordHash != "" {
		t.Fatalf("expected no password hash for an OIDC user, got %q", user.PasswordHash)
	}

	again, err := service.LoginOrLinkOIDCUser(t.Context(), "org_default", "https://idp.example.com", "subject-123", "sso-user@example.com")
	if err != nil || again.ID != user.ID {
		t.Fatalf("expected the second login to return the same linked user, got %#v %v", again, err)
	}
}

func TestLoginOrLinkOIDCUserRejectsWithoutAPendingInvite(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, err := service.LoginOrLinkOIDCUser(t.Context(), "org_default", "https://idp.example.com", "subject-999", "unknown@example.com"); !errors.Is(err, identity.ErrInviteNotFound) {
		t.Fatalf("expected ErrInviteNotFound, got %v", err)
	}
}

func TestLoginOrLinkOIDCUserRejectsALocalTypeInvite(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateInvite(t.Context(), "admin", "org_default", "local-user@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeLocal); err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := service.LoginOrLinkOIDCUser(t.Context(), "org_default", "https://idp.example.com", "subject-1", "local-user@example.com"); !errors.Is(err, identity.ErrInviteWrongIdentityType) {
		t.Fatalf("expected ErrInviteWrongIdentityType, got %v", err)
	}
}

func TestConsumeInviteRejectsAnOIDCTypeInvite(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	_, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "sso-user@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeOIDC)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, _, err := service.ConsumeInvite(t.Context(), token, "a password"); !errors.Is(err, identity.ErrInviteWrongIdentityType) {
		t.Fatalf("expected ErrInviteWrongIdentityType, got %v", err)
	}
}

func TestSetAndReadOIDCConfigurationRedactsTheClientSecret(t *testing.T) {
	t.Parallel()
	service := newTestService()
	saved, err := service.SetOIDCConfiguration(t.Context(), "org_default", "https://idp.example.com/.well-known/openid-configuration", "https://idp.example.com", "client-id", "super-secret", "https://hub.example.com/api/v1/oidc/callback", "admin")
	if err != nil {
		t.Fatalf("set oidc configuration: %v", err)
	}
	if saved.ClientSecretEncrypted == nil {
		t.Fatal("expected SetOIDCConfiguration's return value to carry the encrypted secret internally")
	}

	read, err := service.OIDCConfiguration(t.Context(), "org_default")
	if err != nil {
		t.Fatalf("read oidc configuration: %v", err)
	}
	if read.ClientSecretEncrypted != nil {
		t.Fatalf("expected the redacted read path to omit the client secret, got %#v", read.ClientSecretEncrypted)
	}
	if read.DiscoveryURL != "https://idp.example.com/.well-known/openid-configuration" || read.ClientID != "client-id" {
		t.Fatalf("unexpected configuration: %#v", read)
	}

	_, secret, err := service.OIDCConfigurationWithSecret(t.Context(), "org_default")
	if err != nil || secret != "super-secret" {
		t.Fatalf("expected the internal read path to decrypt the client secret, got %q %v", secret, err)
	}
}

func TestOIDCConfigurationByOrganizationReturnsErrOIDCNotConfigured(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, err := service.OIDCConfiguration(t.Context(), "org_default"); !errors.Is(err, identity.ErrOIDCNotConfigured) {
		t.Fatalf("expected ErrOIDCNotConfigured, got %v", err)
	}
}

func TestConsumeInviteReturnsAProvisioningURIThatAnIndependentClientCanUse(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	_, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-user@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeLocal)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	user, enrollment, err := service.ConsumeInvite(t.Context(), token, "a brand new password")
	if err != nil {
		t.Fatalf("consume invite: %v", err)
	}

	// Simulate a real authenticator app: parse the secret out of the
	// provisioning URI exactly as a QR scanner would, with no access to
	// any Go-internal state — this is the actual bug being fixed.
	parsed, err := url.Parse(enrollment.ProvisioningURI)
	if err != nil {
		t.Fatalf("parse provisioning URI: %v", err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(parsed.Query().Get("secret"))
	if err != nil {
		t.Fatalf("decode provisioning URI secret: %v", err)
	}

	now := time.Now().UTC()
	independentlyComputedCode := identity.GenerateTOTPCode(secret, now)
	if _, err := service.ConfirmTOTP(t.Context(), user.ID, func([]byte) string { return independentlyComputedCode }, now); err != nil {
		t.Fatalf("expected a code computed only from the provisioning URI's secret to be accepted, got %v", err)
	}
}

func TestUsersForOrganizationReturnsOnlyThatOrganizationsUsersSortedByEmail(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "zed@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	otherOrgService := newTestService()
	if _, _, err := otherOrgService.Bootstrap(t.Context(), "other_org", "other@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	_, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "alice@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeLocal)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ConsumeInvite(t.Context(), token, "a brand new password"); err != nil {
		t.Fatal(err)
	}

	users, err := service.UsersForOrganization(t.Context(), "org_default")
	if err != nil {
		t.Fatalf("users for organization: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected exactly 2 users in org_default, got %#v", users)
	}
	if users[0].Email != "alice@example.com" || users[1].Email != "zed@example.com" {
		t.Fatalf("expected users sorted by email, got %#v", users)
	}
}

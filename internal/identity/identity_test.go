package identity_test

import (
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

	invite, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-admin@example.com")
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

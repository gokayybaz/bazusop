package identity_test

import (
	"strings"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/identity"
)

func TestGenerateAndVerifyTOTPCodeRoundTrips(t *testing.T) {
	t.Parallel()
	secret, err := identity.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	code := identity.GenerateTOTPCode(secret, now)
	if len(code) != 6 {
		t.Fatalf("expected a 6-digit code, got %q", code)
	}
	if !identity.VerifyTOTPCode(secret, code, now) {
		t.Fatal("expected the freshly generated code to verify at the same instant")
	}
}

func TestVerifyTOTPCodeToleratesOneStepOfClockSkew(t *testing.T) {
	t.Parallel()
	secret, err := identity.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	code := identity.GenerateTOTPCode(secret, now)
	if !identity.VerifyTOTPCode(secret, code, now.Add(30*time.Second)) {
		t.Fatal("expected a 1-step-old code to still verify")
	}
	if identity.VerifyTOTPCode(secret, code, now.Add(90*time.Second)) {
		t.Fatal("expected a 3-step-old code to be rejected")
	}
}

func TestVerifyTOTPCodeRejectsWrongCode(t *testing.T) {
	t.Parallel()
	secret, err := identity.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if identity.VerifyTOTPCode(secret, "000000", now) {
		t.Fatal("expected an arbitrary wrong code to be rejected (astronomically unlikely to collide)")
	}
}

func TestTOTPProvisioningURIIsAValidOtpauthURI(t *testing.T) {
	t.Parallel()
	secret, err := identity.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	uri := identity.TOTPProvisioningURI("bazUSOP", "ops@example.com", secret)
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("expected an otpauth:// URI, got %q", uri)
	}
	if !strings.Contains(uri, "secret=") || !strings.Contains(uri, "issuer=bazUSOP") {
		t.Fatalf("expected secret and issuer parameters, got %q", uri)
	}
}

func TestEncryptDecryptTOTPSecretRoundTrips(t *testing.T) {
	t.Parallel()
	secret, err := identity.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := identity.EncryptTOTPSecret(secret, "a runtime encryption key")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if string(ciphertext) == string(secret) {
		t.Fatal("expected the ciphertext to differ from the plaintext secret")
	}
	decrypted, err := identity.DecryptTOTPSecret(ciphertext, "a runtime encryption key")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != string(secret) {
		t.Fatal("expected decryption with the same key to recover the original secret")
	}
	if _, err := identity.DecryptTOTPSecret(ciphertext, "the wrong key"); err == nil {
		t.Fatal("expected decryption with the wrong key to fail")
	}
}

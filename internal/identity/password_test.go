package identity_test

import (
	"strings"
	"testing"

	"github.com/gokayybaz/bazusop/internal/identity"
)

func TestHashPasswordProducesAVerifiablePHCEncodedHash(t *testing.T) {
	t.Parallel()
	encoded, err := identity.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Fatalf("expected a PHC-encoded argon2id hash, got %q", encoded)
	}
	ok, err := identity.VerifyPassword(encoded, "correct horse battery staple")
	if err != nil {
		t.Fatalf("verify password: %v", err)
	}
	if !ok {
		t.Fatal("expected the correct password to verify")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	t.Parallel()
	encoded, err := identity.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := identity.VerifyPassword(encoded, "wrong password")
	if err != nil {
		t.Fatalf("verify password: %v", err)
	}
	if ok {
		t.Fatal("expected the wrong password to be rejected")
	}
}

func TestHashPasswordProducesDistinctSaltsForTheSamePassword(t *testing.T) {
	t.Parallel()
	first, err := identity.HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := identity.HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected distinct hashes (distinct salts) for two calls with the same password")
	}
}

func TestVerifyPasswordRejectsMalformedEncodedHash(t *testing.T) {
	t.Parallel()
	if _, err := identity.VerifyPassword("not-a-real-hash", "anything"); err == nil {
		t.Fatal("expected an error for a malformed encoded hash")
	}
}

package identity_test

import (
	"testing"

	"github.com/gokayybaz/bazusop/internal/identity"
)

func TestGenerateRecoveryCodesProducesTheRequestedCountOfDistinctCodes(t *testing.T) {
	t.Parallel()
	codes, err := identity.GenerateRecoveryCodes(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 {
		t.Fatalf("expected 10 codes, got %d", len(codes))
	}
	seen := make(map[string]bool, len(codes))
	for _, code := range codes {
		if seen[code] {
			t.Fatalf("expected distinct codes, saw %q twice", code)
		}
		seen[code] = true
		if len(code) == 0 {
			t.Fatal("expected a non-empty code")
		}
	}
}

func TestHashRecoveryCodeIsDeterministicAndCaseSensitive(t *testing.T) {
	t.Parallel()
	first := identity.HashRecoveryCode("abcd1234")
	second := identity.HashRecoveryCode("abcd1234")
	if first != second {
		t.Fatal("expected hashing the same code twice to produce the same hash")
	}
	if identity.HashRecoveryCode("ABCD1234") == first {
		t.Fatal("expected hashing to be case-sensitive")
	}
}

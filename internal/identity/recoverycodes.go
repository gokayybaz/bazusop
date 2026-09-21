package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateRecoveryCodes returns count single-use, human-typeable codes.
// Callers show them to the user exactly once; only HashRecoveryCode's
// output is ever persisted.
func GenerateRecoveryCodes(count int) ([]string, error) {
	codes := make([]string, count)
	for index := range codes {
		value := make([]byte, 8)
		if _, err := rand.Read(value); err != nil {
			return nil, fmt.Errorf("generate recovery code: %w", err)
		}
		codes[index] = hex.EncodeToString(value)
	}
	return codes, nil
}

func HashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

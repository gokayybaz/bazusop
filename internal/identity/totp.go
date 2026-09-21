package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 mandates HMAC-SHA1; HMAC-SHA1 remains sound even though SHA1 collision resistance is broken elsewhere.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

const totpStep = 30 * time.Second

// GenerateTOTPSecret returns a 160-bit shared secret, the size RFC 4226 (the
// HOTP base RFC 6238 builds on) recommends for HMAC-SHA1.
func GenerateTOTPSecret() ([]byte, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate TOTP secret: %w", err)
	}
	return secret, nil
}

func GenerateTOTPCode(secret []byte, at time.Time) string {
	return hotpCode(secret, uint64(at.Unix())/uint64(totpStep.Seconds()))
}

// VerifyTOTPCode accepts the code for the current step and one step on
// either side (a 90-second window), the standard tolerance for authenticator
// clock skew.
func VerifyTOTPCode(secret []byte, code string, at time.Time) bool {
	counter := uint64(at.Unix()) / uint64(totpStep.Seconds())
	for _, candidate := range []uint64{counter - 1, counter, counter + 1} {
		if subtle.ConstantTimeCompare([]byte(hotpCode(secret, candidate)), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

func hotpCode(secret []byte, counter uint64) string {
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(counterBytes)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", truncated%1_000_000)
}

// TOTPProvisioningURI builds the otpauth:// URI authenticator apps consume
// (typically rendered as a QR code by the UI spike 11.8 adds); this spike
// only produces the string.
func TOTPProvisioningURI(issuer, accountEmail string, secret []byte) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(accountEmail)
	query := url.Values{
		"secret": {base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)},
		"issuer": {issuer},
		"digits": {"6"},
		"period": {strconv.Itoa(int(totpStep.Seconds()))},
	}
	return "otpauth://totp/" + label + "?" + query.Encode()
}

const totpKeyVersion = 0x01

// EncryptTOTPSecret encrypts secret with AES-256-GCM under a key derived
// (SHA-256) from encryptionKey, so operators can supply a key of any length
// rather than an exact 32-byte value. The ciphertext is prefixed with a
// one-byte key version for forward compatibility with key rotation, which
// this spike does not otherwise implement (see Global Constraints).
func EncryptTOTPSecret(secret []byte, encryptionKey string) ([]byte, error) {
	block, err := aesCipher(encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build AES-GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, secret, nil)
	return append([]byte{totpKeyVersion}, sealed...), nil
}

func DecryptTOTPSecret(ciphertext []byte, encryptionKey string) ([]byte, error) {
	if len(ciphertext) < 1 || ciphertext[0] != totpKeyVersion {
		return nil, fmt.Errorf("unsupported TOTP secret key version")
	}
	block, err := aesCipher(encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build AES-GCM: %w", err)
	}
	sealed := ciphertext[1:]
	if len(sealed) < gcm.NonceSize() {
		return nil, fmt.Errorf("truncated TOTP ciphertext")
	}
	nonce, encrypted := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	secret, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt TOTP secret: %w", err)
	}
	return secret, nil
}

func aesCipher(encryptionKey string) (cipher.Block, error) {
	key := sha256.Sum256([]byte(encryptionKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("build AES cipher: %w", err)
	}
	return block, nil
}

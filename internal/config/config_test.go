package config_test

import (
	"testing"

	"github.com/gokayybaz/bazusop/internal/config"
)

func TestHTTPAddress(t *testing.T) {
	t.Setenv("BAZUSOP_HTTP_ADDR", "127.0.0.1:18080")

	if address := config.Load().HTTPAddress; address != "127.0.0.1:18080" {
		t.Fatalf("expected configured HTTP address, got %q", address)
	}
}

func TestHTTPAddressDefaultsTo8080(t *testing.T) {
	t.Setenv("BAZUSOP_HTTP_ADDR", "")

	if address := config.Load().HTTPAddress; address != ":8080" {
		t.Fatalf("expected default HTTP address, got %q", address)
	}
}

func TestEnrollmentToken(t *testing.T) {
	t.Setenv("BAZUSOP_ENROLLMENT_TOKEN", "bootstrap-secret")

	if token := config.Load().EnrollmentToken; token != "bootstrap-secret" {
		t.Fatalf("expected configured enrollment token, got %q", token)
	}
}

func TestTLSFiles(t *testing.T) {
	t.Setenv("BAZUSOP_TLS_CERT_FILE", "/run/secrets/hub.crt")
	t.Setenv("BAZUSOP_TLS_KEY_FILE", "/run/secrets/hub.key")

	configuration := config.Load()
	if configuration.TLSCertificate != "/run/secrets/hub.crt" {
		t.Fatalf("expected configured TLS certificate, got %q", configuration.TLSCertificate)
	}
	if configuration.TLSPrivateKey != "/run/secrets/hub.key" {
		t.Fatalf("expected configured TLS private key, got %q", configuration.TLSPrivateKey)
	}
}

func TestDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://bazusop:secret@db/bazusop")

	if databaseURL := config.Load().DatabaseURL; databaseURL != "postgres://bazusop:secret@db/bazusop" {
		t.Fatalf("expected configured database URL, got %q", databaseURL)
	}
}

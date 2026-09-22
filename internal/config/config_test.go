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

func TestBootstrapSecret(t *testing.T) {
	t.Setenv("BAZUSOP_BOOTSTRAP_SECRET", "bootstrap-secret")

	if secret := config.Load().BootstrapSecret; secret != "bootstrap-secret" {
		t.Fatalf("expected configured bootstrap secret, got %q", secret)
	}
}

func TestTOTPEncryptionKey(t *testing.T) {
	t.Setenv("BAZUSOP_TOTP_ENCRYPTION_KEY", "totp-key")

	if key := config.Load().TOTPEncryptionKey; key != "totp-key" {
		t.Fatalf("expected configured TOTP encryption key, got %q", key)
	}
}

func TestTrustedOrigins(t *testing.T) {
	t.Setenv("BAZUSOP_TRUSTED_ORIGINS", "https://ui.example, https://admin.example")

	origins := config.Load().TrustedOrigins
	if len(origins) != 2 || origins[0] != "https://ui.example" || origins[1] != "https://admin.example" {
		t.Fatalf("expected two trimmed trusted origins, got %#v", origins)
	}
}

func TestTrustedOriginsDefaultsToEmpty(t *testing.T) {
	t.Setenv("BAZUSOP_TRUSTED_ORIGINS", "")

	if origins := config.Load().TrustedOrigins; len(origins) != 0 {
		t.Fatalf("expected no trusted origins by default, got %#v", origins)
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

func TestTimescaleEnabled(t *testing.T) {
	t.Setenv("BAZUSOP_TIMESCALE_ENABLED", "true")

	if !config.Load().TimescaleEnabled {
		t.Fatal("expected TimescaleDB to be enabled")
	}
}

func TestRetentionDefaults(t *testing.T) {
	t.Setenv("BAZUSOP_TELEMETRY_RETENTION_DAYS", "")
	t.Setenv("BAZUSOP_LOG_RETENTION_DAYS", "")

	configuration := config.Load()
	if configuration.TelemetryRetentionDays != 30 || configuration.LogRetentionDays != 14 {
		t.Fatalf("unexpected retention defaults: telemetry=%d logs=%d", configuration.TelemetryRetentionDays, configuration.LogRetentionDays)
	}
	if err := configuration.Validate(); err != nil {
		t.Fatalf("validate defaults: %v", err)
	}
}

func TestConfiguredRetentionDays(t *testing.T) {
	t.Setenv("BAZUSOP_TELEMETRY_RETENTION_DAYS", "90")
	t.Setenv("BAZUSOP_LOG_RETENTION_DAYS", "21")

	configuration := config.Load()
	if configuration.TelemetryRetentionDays != 90 || configuration.LogRetentionDays != 21 {
		t.Fatalf("unexpected configured retention: telemetry=%d logs=%d", configuration.TelemetryRetentionDays, configuration.LogRetentionDays)
	}
}

func TestInvalidRetentionIsRejected(t *testing.T) {
	for _, variable := range []string{"BAZUSOP_TELEMETRY_RETENTION_DAYS", "BAZUSOP_LOG_RETENTION_DAYS"} {
		for _, value := range []string{"invalid", "0", "3651"} {
			t.Run(variable+"/"+value, func(t *testing.T) {
				t.Setenv(variable, value)
				if err := config.Load().Validate(); err == nil {
					t.Fatalf("expected %s=%q to be rejected", variable, value)
				}
			})
		}
	}
}

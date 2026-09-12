package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddress            string
	EnrollmentToken        string
	OperatorToken          string
	AdminToken             string
	TLSCertificate         string
	TLSPrivateKey          string
	DatabaseURL            string
	TimescaleEnabled       bool
	TelemetryRetentionDays int
	LogRetentionDays       int
}

func Load() Config {
	httpAddress := os.Getenv("BAZUSOP_HTTP_ADDR")
	if httpAddress == "" {
		httpAddress = ":8080"
	}

	return Config{
		HTTPAddress:            httpAddress,
		EnrollmentToken:        os.Getenv("BAZUSOP_ENROLLMENT_TOKEN"),
		OperatorToken:          os.Getenv("BAZUSOP_OPERATOR_TOKEN"),
		AdminToken:             os.Getenv("BAZUSOP_ADMIN_TOKEN"),
		TLSCertificate:         os.Getenv("BAZUSOP_TLS_CERT_FILE"),
		TLSPrivateKey:          os.Getenv("BAZUSOP_TLS_KEY_FILE"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		TimescaleEnabled:       enabled(os.Getenv("BAZUSOP_TIMESCALE_ENABLED")),
		TelemetryRetentionDays: retentionDays("BAZUSOP_TELEMETRY_RETENTION_DAYS", 30),
		LogRetentionDays:       retentionDays("BAZUSOP_LOG_RETENTION_DAYS", 14),
	}
}

func (configuration Config) Validate() error {
	if configuration.TelemetryRetentionDays < 1 || configuration.TelemetryRetentionDays > 3650 {
		return fmt.Errorf("BAZUSOP_TELEMETRY_RETENTION_DAYS must be between 1 and 3650")
	}
	if configuration.LogRetentionDays < 1 || configuration.LogRetentionDays > 3650 {
		return fmt.Errorf("BAZUSOP_LOG_RETENTION_DAYS must be between 1 and 3650")
	}
	return nil
}

func retentionDays(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	days, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return days
}

func enabled(value string) bool {
	switch value {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	default:
		return false
	}
}

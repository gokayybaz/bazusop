package config

import "os"

type Config struct {
	HTTPAddress      string
	EnrollmentToken  string
	OperatorToken    string
	TLSCertificate   string
	TLSPrivateKey    string
	DatabaseURL      string
	TimescaleEnabled bool
}

func Load() Config {
	httpAddress := os.Getenv("BAZUSOP_HTTP_ADDR")
	if httpAddress == "" {
		httpAddress = ":8080"
	}

	return Config{
		HTTPAddress:      httpAddress,
		EnrollmentToken:  os.Getenv("BAZUSOP_ENROLLMENT_TOKEN"),
		OperatorToken:    os.Getenv("BAZUSOP_OPERATOR_TOKEN"),
		TLSCertificate:   os.Getenv("BAZUSOP_TLS_CERT_FILE"),
		TLSPrivateKey:    os.Getenv("BAZUSOP_TLS_KEY_FILE"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		TimescaleEnabled: enabled(os.Getenv("BAZUSOP_TIMESCALE_ENABLED")),
	}
}

func enabled(value string) bool {
	switch value {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	default:
		return false
	}
}

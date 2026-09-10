package config

import "os"

type Config struct {
	HTTPAddress     string
	EnrollmentToken string
	TLSCertificate  string
	TLSPrivateKey   string
}

func Load() Config {
	httpAddress := os.Getenv("BAZUSOP_HTTP_ADDR")
	if httpAddress == "" {
		httpAddress = ":8080"
	}

	return Config{
		HTTPAddress:     httpAddress,
		EnrollmentToken: os.Getenv("BAZUSOP_ENROLLMENT_TOKEN"),
		TLSCertificate:  os.Getenv("BAZUSOP_TLS_CERT_FILE"),
		TLSPrivateKey:   os.Getenv("BAZUSOP_TLS_KEY_FILE"),
	}
}

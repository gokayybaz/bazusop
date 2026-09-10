package config

import "os"

type Config struct {
	HTTPAddress string
}

func Load() Config {
	httpAddress := os.Getenv("BAZUSOP_HTTP_ADDR")
	if httpAddress == "" {
		httpAddress = ":8080"
	}

	return Config{HTTPAddress: httpAddress}
}


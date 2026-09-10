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


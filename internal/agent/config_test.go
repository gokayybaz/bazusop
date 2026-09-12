package agent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigFromEnvironment(t *testing.T) {
	t.Setenv("BAZUSOP_AGENT_HUB_URL", "https://hub.example.test:8443/")
	t.Setenv("BAZUSOP_AGENT_ENROLLMENT_TOKEN", "bootstrap-secret")
	t.Setenv("BAZUSOP_AGENT_STATE_DIR", filepath.Join(t.TempDir(), "state"))
	t.Setenv("BAZUSOP_AGENT_SERVER_CA_FILE", "/run/secrets/hub-ca.crt")
	t.Setenv("BAZUSOP_AGENT_REPORT_INTERVAL", "45s")

	configuration := LoadConfig()
	if configuration.HubURL != "https://hub.example.test:8443" {
		t.Fatalf("unexpected hub URL: %q", configuration.HubURL)
	}
	if configuration.EnrollmentToken != "bootstrap-secret" || configuration.ServerCAFile != "/run/secrets/hub-ca.crt" {
		t.Fatal("agent secrets were not loaded")
	}
	if configuration.ReportInterval != 45*time.Second {
		t.Fatalf("unexpected report interval: %s", configuration.ReportInterval)
	}
	if err := configuration.Validate(); err != nil {
		t.Fatalf("validate configuration: %v", err)
	}
}

func TestConfigRejectsInsecureRemoteHub(t *testing.T) {
	configuration := Config{HubURL: "http://hub.example.test", StateDir: t.TempDir(), ReportInterval: time.Minute}
	if err := configuration.Validate(); err == nil {
		t.Fatal("expected remote HTTP hub to be rejected")
	}
}

func TestConfigAllowsLoopbackHTTPForDevelopment(t *testing.T) {
	configuration := Config{HubURL: "http://127.0.0.1:18080", StateDir: t.TempDir(), ReportInterval: time.Minute}
	if err := configuration.Validate(); err != nil {
		t.Fatalf("expected loopback HTTP to be accepted: %v", err)
	}
}

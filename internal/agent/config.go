package agent

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultReportInterval = 30 * time.Second

type Config struct {
	HubURL          string
	EnrollmentToken string
	StateDir        string
	ServerCAFile    string
	ReportInterval  time.Duration
}

func LoadConfig() Config {
	interval := defaultReportInterval
	if value := strings.TrimSpace(os.Getenv("BAZUSOP_AGENT_REPORT_INTERVAL")); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			interval = 0
		} else {
			interval = parsed
		}
	}
	return Config{
		HubURL:          strings.TrimRight(strings.TrimSpace(os.Getenv("BAZUSOP_AGENT_HUB_URL")), "/"),
		EnrollmentToken: os.Getenv("BAZUSOP_AGENT_ENROLLMENT_TOKEN"),
		StateDir:        configuredStateDir(),
		ServerCAFile:    strings.TrimSpace(os.Getenv("BAZUSOP_AGENT_SERVER_CA_FILE")),
		ReportInterval:  interval,
	}
}

func (configuration Config) Validate() error {
	parsed, err := url.Parse(configuration.HubURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("BAZUSOP_AGENT_HUB_URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if parsed.Scheme == "http" && !loopbackHost(parsed.Hostname()) {
		return fmt.Errorf("BAZUSOP_AGENT_HUB_URL must use HTTPS outside loopback development")
	}
	if strings.TrimSpace(configuration.StateDir) == "" {
		return fmt.Errorf("BAZUSOP_AGENT_STATE_DIR is required")
	}
	if configuration.ReportInterval < 10*time.Second || configuration.ReportInterval > time.Hour {
		return fmt.Errorf("BAZUSOP_AGENT_REPORT_INTERVAL must be between 10s and 1h")
	}
	return nil
}

func configuredStateDir() string {
	if value := strings.TrimSpace(os.Getenv("BAZUSOP_AGENT_STATE_DIR")); value != "" {
		return value
	}
	if runtime.GOOS == "windows" {
		if programData := strings.TrimSpace(os.Getenv("ProgramData")); programData != "" {
			return filepath.Join(programData, "bazUSOP", "agent")
		}
		return filepath.Join(`C:\ProgramData`, "bazUSOP", "agent")
	}
	return "/var/lib/bazusop-agent"
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

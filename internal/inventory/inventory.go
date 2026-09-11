package inventory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strings"
	"time"
)

var ErrInvalidFacts = errors.New("invalid host facts")

type Status string

const (
	StatusConnected Status = "connected"
	StatusStale     Status = "stale"
	connectedWindow        = 2 * time.Minute
)

type Facts struct {
	Hostname      string   `json:"hostname"`
	OSFamily      string   `json:"os_family"`
	OSName        string   `json:"os_name"`
	OSVersion     string   `json:"os_version"`
	Architecture  string   `json:"architecture"`
	KernelVersion string   `json:"kernel_version"`
	CPUCores      int      `json:"cpu_cores"`
	MemoryBytes   uint64   `json:"memory_bytes"`
	IPAddresses   []string `json:"ip_addresses"`
	AgentVersion  string   `json:"agent_version"`
}

type Host struct {
	AgentID       string    `json:"agent_id"`
	Hostname      string    `json:"hostname"`
	OSFamily      string    `json:"os_family"`
	OSName        string    `json:"os_name"`
	OSVersion     string    `json:"os_version"`
	Architecture  string    `json:"architecture"`
	KernelVersion string    `json:"kernel_version"`
	CPUCores      int       `json:"cpu_cores"`
	MemoryBytes   uint64    `json:"memory_bytes"`
	IPAddresses   []string  `json:"ip_addresses"`
	AgentVersion  string    `json:"agent_version"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	Status        Status    `json:"status"`
}

type Store interface {
	Upsert(context.Context, Host) error
	List(context.Context) ([]Host, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option {
	return func(service *Service) {
		service.now = clock
	}
}

func NewService(store Store, options ...Option) *Service {
	service := &Service{store: store, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

func (service *Service) Report(ctx context.Context, agentID string, facts Facts) error {
	host, err := normalize(agentID, facts, service.now().UTC())
	if err != nil {
		return err
	}
	return service.store.Upsert(ctx, host)
}

func (service *Service) List(ctx context.Context) ([]Host, error) {
	hosts, err := service.store.List(ctx)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	for index := range hosts {
		hosts[index].Status = StatusStale
		if now.Sub(hosts[index].LastSeenAt) <= connectedWindow {
			hosts[index].Status = StatusConnected
		}
	}
	sort.Slice(hosts, func(left, right int) bool {
		return hosts[left].Hostname < hosts[right].Hostname
	})
	return hosts, nil
}

func normalize(agentID string, facts Facts, observedAt time.Time) (Host, error) {
	agentID = strings.TrimSpace(agentID)
	hostname := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(facts.Hostname), "."))
	osFamily := strings.ToLower(strings.TrimSpace(facts.OSFamily))
	architecture := normalizeArchitecture(facts.Architecture)
	if agentID == "" || hostname == "" || len(hostname) > 253 || !supportedOS(osFamily) || architecture == "" {
		return Host{}, ErrInvalidFacts
	}
	if facts.CPUCores < 1 || facts.CPUCores > 4096 || facts.MemoryBytes == 0 || facts.MemoryBytes > math.MaxInt64 {
		return Host{}, ErrInvalidFacts
	}

	return Host{
		AgentID:       agentID,
		Hostname:      hostname,
		OSFamily:      osFamily,
		OSName:        bounded(strings.TrimSpace(facts.OSName), 128),
		OSVersion:     bounded(strings.TrimSpace(facts.OSVersion), 128),
		Architecture:  architecture,
		KernelVersion: bounded(strings.TrimSpace(facts.KernelVersion), 256),
		CPUCores:      facts.CPUCores,
		MemoryBytes:   facts.MemoryBytes,
		IPAddresses:   normalizeAddresses(facts.IPAddresses),
		AgentVersion:  bounded(strings.TrimSpace(facts.AgentVersion), 64),
		FirstSeenAt:   observedAt,
		LastSeenAt:    observedAt,
	}, nil
}

func supportedOS(value string) bool {
	return value == "linux" || value == "windows"
}

func normalizeArchitecture(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "amd64", "x86_64", "x64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	case "386", "i386", "i686", "x86":
		return "386"
	default:
		return ""
	}
}

func normalizeAddresses(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	addresses := make([]string, 0, len(values))
	for _, value := range values {
		address := net.ParseIP(strings.TrimSpace(value))
		if address == nil {
			continue
		}
		normalized := address.String()
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		addresses = append(addresses, normalized)
	}
	return addresses
}

func bounded(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (host Host) ValidateStored() error {
	if host.AgentID == "" || host.Hostname == "" || !supportedOS(host.OSFamily) || normalizeArchitecture(host.Architecture) == "" {
		return fmt.Errorf("%w: stored host is incomplete", ErrInvalidFacts)
	}
	return nil
}

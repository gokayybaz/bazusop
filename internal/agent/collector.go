package agent

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"sort"

	"github.com/gokayybaz/bazusop/internal/inventory"
)

type PlatformFacts struct {
	OSFamily      string
	OSName        string
	OSVersion     string
	KernelVersion string
	MemoryBytes   uint64
}

type Collector struct {
	Hostname     func() (string, error)
	Interfaces   func() ([]string, error)
	Platform     func() (PlatformFacts, error)
	Architecture string
	CPUCores     int
	AgentVersion string
}

func NewCollector(agentVersion string) Collector {
	return Collector{
		Hostname:     os.Hostname,
		Interfaces:   interfaceAddresses,
		Platform:     platformFacts,
		Architecture: runtime.GOARCH,
		CPUCores:     runtime.NumCPU(),
		AgentVersion: agentVersion,
	}
}

func (collector Collector) Collect() (inventory.Facts, error) {
	hostname, err := collector.Hostname()
	if err != nil {
		return inventory.Facts{}, fmt.Errorf("read hostname: %w", err)
	}
	addresses, err := collector.Interfaces()
	if err != nil {
		return inventory.Facts{}, fmt.Errorf("read network interfaces: %w", err)
	}
	platform, err := collector.Platform()
	if err != nil {
		return inventory.Facts{}, err
	}
	return inventory.Facts{
		Hostname: hostname, OSFamily: platform.OSFamily, OSName: platform.OSName,
		OSVersion: platform.OSVersion, Architecture: collector.Architecture,
		KernelVersion: platform.KernelVersion, CPUCores: collector.CPUCores,
		MemoryBytes: platform.MemoryBytes, IPAddresses: routableAddresses(addresses),
		AgentVersion: collector.AgentVersion,
	}, nil
}

func interfaceAddresses() ([]string, error) {
	values, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	addresses := make([]string, 0, len(values))
	for _, value := range values {
		if network, _, parseErr := net.ParseCIDR(value.String()); parseErr == nil {
			addresses = append(addresses, network.String())
		}
	}
	return addresses, nil
}

func routableAddresses(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		address := net.ParseIP(value)
		if address == nil || address.IsLoopback() || address.IsUnspecified() || address.IsMulticast() {
			continue
		}
		unique[address.String()] = struct{}{}
	}
	addresses := make([]string, 0, len(unique))
	for address := range unique {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	return addresses
}

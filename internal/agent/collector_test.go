package agent

import (
	"reflect"
	"testing"
)

func TestCollectorBuildsNormalizedInventory(t *testing.T) {
	collector := Collector{
		Hostname: func() (string, error) { return "WEB-01.example.com", nil },
		Interfaces: func() ([]string, error) {
			return []string{"127.0.0.1", "10.0.0.8", "::1", "10.0.0.8"}, nil
		},
		Platform: func() (PlatformFacts, error) {
			return PlatformFacts{OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", KernelVersion: "6.8.0", MemoryBytes: 16 << 30}, nil
		},
		Architecture: "amd64",
		CPUCores:     8,
		AgentVersion: "0.4.0",
	}

	facts, err := collector.Collect()
	if err != nil {
		t.Fatalf("collect inventory: %v", err)
	}
	if facts.Hostname != "WEB-01.example.com" || facts.MemoryBytes != 16<<30 || facts.CPUCores != 8 {
		t.Fatalf("unexpected facts: %#v", facts)
	}
	if !reflect.DeepEqual(facts.IPAddresses, []string{"10.0.0.8"}) {
		t.Fatalf("unexpected addresses: %#v", facts.IPAddresses)
	}
}

func TestParseLinuxMemory(t *testing.T) {
	memory, err := parseLinuxMemory("MemTotal:       16384256 kB\nMemFree:         1000 kB\n")
	if err != nil {
		t.Fatalf("parse memory: %v", err)
	}
	if memory != 16384256*1024 {
		t.Fatalf("unexpected memory size: %d", memory)
	}
}

func TestParseOSRelease(t *testing.T) {
	name, release := parseOSRelease("NAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\n")
	if name != "Ubuntu" || release != "24.04" {
		t.Fatalf("unexpected OS release: %q %q", name, release)
	}
}

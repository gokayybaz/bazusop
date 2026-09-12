//go:build linux

package agent

import (
	"fmt"
	"os"
	"strings"
)

func platformFacts() (PlatformFacts, error) {
	memoryContents, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return PlatformFacts{}, fmt.Errorf("read Linux memory: %w", err)
	}
	memory, err := parseLinuxMemory(string(memoryContents))
	if err != nil {
		return PlatformFacts{}, err
	}
	osContents, _ := os.ReadFile("/etc/os-release")
	name, version := parseOSRelease(string(osContents))
	kernelContents, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return PlatformFacts{}, fmt.Errorf("read Linux kernel: %w", err)
	}
	return PlatformFacts{OSFamily: "linux", OSName: name, OSVersion: version, KernelVersion: strings.TrimSpace(string(kernelContents)), MemoryBytes: memory}, nil
}

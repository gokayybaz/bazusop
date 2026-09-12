//go:build !linux && !windows

package agent

func networkCounters() (uint64, uint64, error) { return 0, 0, nil }

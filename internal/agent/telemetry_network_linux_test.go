//go:build linux

package agent

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseLinuxNetworkCountersExcludesLoopback(t *testing.T) {
	input := "Inter-| Receive | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\n lo: 100 1 0 0 0 0 0 0 200 2 0 0 0 0 0 0\n eth0: 300 3 0 0 0 0 0 0 400 4 0 0 0 0 0 0\n"
	received, transmitted, err := parseLinuxNetworkCounters(bufio.NewScanner(strings.NewReader(input)))
	if err != nil {
		t.Fatalf("parse network counters: %v", err)
	}
	if received != 300 || transmitted != 400 {
		t.Fatalf("unexpected counters: rx=%d tx=%d", received, transmitted)
	}
}

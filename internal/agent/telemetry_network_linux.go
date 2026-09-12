//go:build linux

package agent

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func networkCounters() (uint64, uint64, error) {
	contents, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer contents.Close()
	return parseLinuxNetworkCounters(bufio.NewScanner(contents))
}

func parseLinuxNetworkCounters(scanner *bufio.Scanner) (uint64, uint64, error) {
	var receiveBytes, transmitBytes uint64
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		separator := strings.IndexByte(line, ':')
		if separator < 0 || strings.TrimSpace(line[:separator]) == "lo" {
			continue
		}
		fields := strings.Fields(line[separator+1:])
		if len(fields) < 9 {
			return 0, 0, fmt.Errorf("invalid /proc/net/dev row")
		}
		received, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse received bytes: %w", err)
		}
		transmitted, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse transmitted bytes: %w", err)
		}
		receiveBytes += received
		transmitBytes += transmitted
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	return receiveBytes, transmitBytes, nil
}

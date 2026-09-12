//go:build windows

package agent

import (
	"net"

	gopsutilnet "github.com/shirou/gopsutil/v4/net"
)

func networkCounters() (uint64, uint64, error) {
	counters, err := gopsutilnet.IOCounters(true)
	if err != nil {
		return 0, 0, err
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return 0, 0, err
	}
	loopback := make(map[string]bool, len(interfaces))
	for _, networkInterface := range interfaces {
		loopback[networkInterface.Name] = networkInterface.Flags&net.FlagLoopback != 0
	}
	var receiveBytes, transmitBytes uint64
	for _, counter := range counters {
		if loopback[counter.Name] {
			continue
		}
		receiveBytes += counter.BytesRecv
		transmitBytes += counter.BytesSent
	}
	return receiveBytes, transmitBytes, nil
}

//go:build windows

package agent

import (
	"fmt"
	"syscall"
	"unsafe"
)

type memoryStatusEx struct {
	Length                   uint32
	MemoryLoad               uint32
	TotalPhysical            uint64
	AvailablePhysical        uint64
	TotalPageFile            uint64
	AvailablePageFile        uint64
	TotalVirtual             uint64
	AvailableVirtual         uint64
	AvailableExtendedVirtual uint64
}

func platformFacts() (PlatformFacts, error) {
	status := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	result, _, callErr := kernel32.NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		return PlatformFacts{}, fmt.Errorf("read Windows memory: %w", callErr)
	}
	version, err := syscall.GetVersion()
	if err != nil {
		return PlatformFacts{}, fmt.Errorf("read Windows version: %w", err)
	}
	major := byte(version)
	minor := uint8(version >> 8)
	build := uint16(version >> 16)
	release := fmt.Sprintf("%d.%d.%d", major, minor, build)
	return PlatformFacts{OSFamily: "windows", OSName: "Microsoft Windows", OSVersion: release, KernelVersion: release, MemoryBytes: status.TotalPhysical}, nil
}

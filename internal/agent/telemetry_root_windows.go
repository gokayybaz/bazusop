//go:build windows

package agent

import "os"

func rootDiskPath() string {
	if drive := os.Getenv("SystemDrive"); drive != "" {
		return drive + `\`
	}
	return `C:\`
}

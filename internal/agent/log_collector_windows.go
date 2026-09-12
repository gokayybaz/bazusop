//go:build windows

package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/gokayybaz/bazusop/internal/logstream"
)

func collectPlatformLogs(parent context.Context, since, until time.Time, limit int) ([]logstream.Entry, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	script := "$start=[DateTimeOffset]::FromUnixTimeMilliseconds(" + strconv.FormatInt(since.UnixMilli(), 10) + ").UtcDateTime; " +
		"$end=[DateTimeOffset]::FromUnixTimeMilliseconds(" + strconv.FormatInt(until.UnixMilli(), 10) + ").UtcDateTime; " +
		"Get-WinEvent -FilterHashtable @{LogName='System','Application';StartTime=$start;EndTime=$end} -ErrorAction Stop | " +
		"Sort-Object TimeCreated | Select-Object -First " + strconv.Itoa(limit) + " | ForEach-Object { " +
		"[pscustomobject]@{TimeCreated=$_.TimeCreated.ToUniversalTime().ToString('o');ProviderName=$_.ProviderName;Level=$_.Level;LevelDisplayName=$_.LevelDisplayName;Message=$_.Message;RecordId=$_.RecordId} | ConvertTo-Json -Compress }"
	output, err := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return nil, fmt.Errorf("Get-WinEvent: %w", err)
	}
	return parseWindowsEventEntries(output)
}

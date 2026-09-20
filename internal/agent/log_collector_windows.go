//go:build windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gokayybaz/bazusop/internal/logstream"
)

func collectPlatformLogs(parent context.Context, since, until time.Time, limit int) ([]logstream.Entry, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	// Get-WinEvent throws (FullyQualifiedErrorId "NoMatchingEventsFound") when the filter
	// matches zero events, which is the common case for a short report window on a quiet
	// host; treat that specific, locale-independent error id as an empty result instead of
	// a collection failure, and let any other error still propagate.
	script := "$start=[DateTimeOffset]::FromUnixTimeMilliseconds(" + strconv.FormatInt(since.UnixMilli(), 10) + ").UtcDateTime; " +
		"$end=[DateTimeOffset]::FromUnixTimeMilliseconds(" + strconv.FormatInt(until.UnixMilli(), 10) + ").UtcDateTime; " +
		"try { Get-WinEvent -FilterHashtable @{LogName='System','Application';StartTime=$start;EndTime=$end} -ErrorAction Stop | " +
		"Sort-Object TimeCreated | Select-Object -First " + strconv.Itoa(limit) + " | ForEach-Object { " +
		"[pscustomobject]@{TimeCreated=$_.TimeCreated.ToUniversalTime().ToString('o');ProviderName=$_.ProviderName;Level=$_.Level;LevelDisplayName=$_.LevelDisplayName;Message=$_.Message;RecordId=$_.RecordId} | ConvertTo-Json -Compress } } " +
		"catch { if ($_.FullyQualifiedErrorId -notmatch 'NoMatchingEventsFound') { throw } }"
	output, err := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		// Output()'s underlying ExitError carries captured stderr; surface it so an
		// unexpected PowerShell failure is diagnosable from the agent's own JSON log
		// instead of a bare "exit status 1".
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && len(exitError.Stderr) > 0 {
			return nil, fmt.Errorf("Get-WinEvent: %w: %s", err, strings.TrimSpace(string(exitError.Stderr)))
		}
		return nil, fmt.Errorf("Get-WinEvent: %w", err)
	}
	return parseWindowsEventEntries(output)
}

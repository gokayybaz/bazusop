//go:build linux

package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/gokayybaz/bazusop/internal/logstream"
)

func collectPlatformLogs(parent context.Context, since, until time.Time, limit int) ([]logstream.Entry, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "journalctl", "--output=json", "--quiet", "--no-pager",
		"--since=@"+strconv.FormatInt(since.Unix(), 10), "--until=@"+strconv.FormatInt(until.Unix(), 10), "--lines="+strconv.Itoa(limit))
	command.Env = append(os.Environ(), "LC_ALL=C")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("journalctl: %w", err)
	}
	return parseJournalEntries(output)
}

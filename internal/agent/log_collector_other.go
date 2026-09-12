//go:build !linux && !windows

package agent

import (
	"context"
	"time"

	"github.com/gokayybaz/bazusop/internal/logstream"
)

func collectPlatformLogs(context.Context, time.Time, time.Time, int) ([]logstream.Entry, error) {
	return []logstream.Entry{}, nil
}

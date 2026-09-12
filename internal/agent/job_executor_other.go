//go:build !linux && !windows

package agent

import (
	"context"
	"errors"

	"github.com/gokayybaz/bazusop/internal/jobs"
)

func executePlatformJob(context.Context, jobs.Job) (string, error) {
	return "", errors.New("remote jobs are unsupported on this platform")
}

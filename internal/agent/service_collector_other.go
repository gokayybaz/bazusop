//go:build !linux && !windows

package agent

import (
	"context"
	"errors"

	"github.com/gokayybaz/bazusop/internal/serviceinventory"
)

func collectPlatformServices(context.Context) ([]serviceinventory.Fact, error) {
	return nil, errors.New("service collection is supported only on Linux and Windows")
}

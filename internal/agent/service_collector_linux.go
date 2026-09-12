//go:build linux

package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/gokayybaz/bazusop/internal/serviceinventory"
)

func collectPlatformServices(ctx context.Context) ([]serviceinventory.Fact, error) {
	units, err := runSystemctl(ctx, "list-units", "--type=service", "--all", "--plain", "--no-legend", "--no-pager")
	if err != nil {
		return nil, err
	}
	unitFiles, err := runSystemctl(ctx, "list-unit-files", "--type=service", "--no-legend", "--no-pager")
	if err != nil {
		return nil, err
	}
	return parseSystemdServices(string(units), string(unitFiles))
}

func runSystemctl(parent context.Context, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "systemctl", arguments...)
	command.Env = append(os.Environ(), "LC_ALL=C")
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("systemctl %s: %w: %s", arguments[0], err, output)
	}
	return output, nil
}

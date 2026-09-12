package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
)

type HubClient interface {
	EnsureIdentity(context.Context, string, string) (Identity, error)
	ReportInventory(context.Context, Identity, inventory.Facts) error
}

type FactCollector interface {
	Collect() (inventory.Facts, error)
}

type Runner struct {
	Hub             HubClient
	Collector       FactCollector
	ReportInterval  time.Duration
	Logger          *slog.Logger
	Hostname        string
	OperatingSystem string
}

func (runner Runner) Run(ctx context.Context) error {
	if runner.Hub == nil || runner.Collector == nil || runner.Logger == nil || runner.ReportInterval <= 0 {
		return fmt.Errorf("agent runner is incomplete")
	}
	if err := runner.report(ctx); err != nil {
		runner.Logger.Error("initial inventory report failed; retrying", "error", err)
	}
	ticker := time.NewTicker(runner.ReportInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := runner.report(ctx); err != nil {
				runner.Logger.Error("inventory report failed", "error", err)
			}
		}
	}
}

func (runner Runner) report(ctx context.Context) error {
	identity, err := runner.Hub.EnsureIdentity(ctx, runner.Hostname, runner.OperatingSystem)
	if err != nil {
		return fmt.Errorf("ensure agent identity: %w", err)
	}
	facts, err := runner.Collector.Collect()
	if err != nil {
		return fmt.Errorf("collect host inventory: %w", err)
	}
	if err := runner.Hub.ReportInventory(ctx, identity, facts); err != nil {
		return err
	}
	runner.Logger.Info("inventory reported", "agent_id", identity.AgentID, "hostname", facts.Hostname)
	return nil
}

package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/telemetry"
)

type HubClient interface {
	EnsureIdentity(context.Context, string, string) (Identity, error)
	ReportInventory(context.Context, Identity, inventory.Facts) error
	ReportTelemetry(context.Context, Identity, telemetry.Sample) error
}

type FactCollector interface {
	Collect() (inventory.Facts, error)
}

type MetricCollector interface {
	Collect() (telemetry.Sample, error)
}

type Runner struct {
	Hub             HubClient
	Collector       FactCollector
	Telemetry       MetricCollector
	ReportInterval  time.Duration
	Logger          *slog.Logger
	Hostname        string
	OperatingSystem string
}

func (runner Runner) Run(ctx context.Context) error {
	if runner.Hub == nil || runner.Collector == nil || runner.Telemetry == nil || runner.Logger == nil || runner.ReportInterval <= 0 {
		return fmt.Errorf("agent runner is incomplete")
	}
	if err := runner.report(ctx); err != nil {
		runner.Logger.Error("initial agent report failed; retrying", "error", err)
	}
	ticker := time.NewTicker(runner.ReportInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := runner.report(ctx); err != nil {
				runner.Logger.Error("agent report failed", "error", err)
			}
		}
	}
}

func (runner Runner) report(ctx context.Context) error {
	identity, err := runner.Hub.EnsureIdentity(ctx, runner.Hostname, runner.OperatingSystem)
	if err != nil {
		return fmt.Errorf("ensure agent identity: %w", err)
	}
	var reportErrors []error
	facts, inventoryErr := runner.Collector.Collect()
	if inventoryErr != nil {
		reportErrors = append(reportErrors, fmt.Errorf("collect host inventory: %w", inventoryErr))
	} else if err := runner.Hub.ReportInventory(ctx, identity, facts); err != nil {
		reportErrors = append(reportErrors, err)
	} else {
		runner.Logger.Info("inventory reported", "agent_id", identity.AgentID, "hostname", facts.Hostname)
	}
	sample, telemetryErr := runner.Telemetry.Collect()
	if telemetryErr != nil {
		reportErrors = append(reportErrors, fmt.Errorf("collect host telemetry: %w", telemetryErr))
	} else if err := runner.Hub.ReportTelemetry(ctx, identity, sample); err != nil {
		reportErrors = append(reportErrors, err)
	} else {
		runner.Logger.Info("telemetry reported", "agent_id", identity.AgentID, "recorded_at", sample.RecordedAt)
	}
	return errors.Join(reportErrors...)
}

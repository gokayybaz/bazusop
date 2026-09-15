package telemetry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestHistoryIsIsolatedByStoredSite(t *testing.T) {
	service := telemetry.NewService(telemetry.NewMemoryStore())
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	agents := []tenancy.Agent{
		{ID: "agent-1", OrganizationID: "org-a", SiteID: "site-old"},
		{ID: "agent-1", OrganizationID: "org-a", SiteID: "site-new"},
		{ID: "agent-1", OrganizationID: "org-b", SiteID: "site-old"},
	}
	for i, agent := range agents {
		// Equal timestamps catch an upsert that overwrites another scope.
		if err := service.Report(t.Context(), agent, telemetry.Sample{AgentID: "forged", OrganizationID: "forged", SiteID: "forged", RecordedAt: at, CPUPercent: float64(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	for i, agent := range agents {
		history, err := service.History(t.Context(), agent.Scope(), agent.ID, at.Add(-time.Second), at.Add(time.Hour), 10)
		if err != nil || len(history) != 1 || history[0].CPUPercent != float64(i+1) || history[0].AgentID != agent.ID || history[0].OrganizationID != agent.OrganizationID || history[0].SiteID != agent.SiteID {
			t.Fatalf("history crossed scope: %#v, %v", history, err)
		}
	}
}

func TestTelemetryRejectsMissingScope(t *testing.T) {
	service := telemetry.NewService(telemetry.NewMemoryStore())
	if err := service.Report(t.Context(), tenancy.Agent{ID: "agent-1"}, telemetry.Sample{RecordedAt: time.Now()}); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("report missing scope: %v", err)
	}
	if _, err := service.History(t.Context(), tenancy.Scope{}, "agent-1", time.Now(), time.Now().Add(time.Hour), 10); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("history missing scope: %v", err)
	}
}

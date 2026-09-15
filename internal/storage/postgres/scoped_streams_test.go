package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestStreamStoresRejectInvalidScopeBeforeDatabaseAccess(t *testing.T) {
	store := &Store{}
	checks := []struct {
		name string
		run  func() error
	}{
		{"telemetry append", func() error { return store.Append(t.Context(), telemetry.Sample{AgentID: "agent-1"}) }},
		{"telemetry history", func() error {
			_, err := store.History(t.Context(), tenancy.Scope{}, "agent-1", time.Now(), time.Now().Add(time.Hour), 10)
			return err
		}},
		{"service replace", func() error { return store.ReplaceServices(t.Context(), tenancy.Scope{}, "agent-1", nil) }},
		{"service list", func() error {
			_, err := store.ListServices(t.Context(), tenancy.Scope{}, "agent-1", serviceinventory.Filter{})
			return err
		}},
		{"log append", func() error {
			_, err := store.AppendLogs(t.Context(), []logstream.Entry{{AgentID: "agent-1"}})
			return err
		}},
		{"log search", func() error { _, err := store.SearchLogs(t.Context(), logstream.Query{}); return err }},
		{"log subscribe", func() error { _, err := store.SubscribeLogs(t.Context(), tenancy.Scope{}, "agent-1"); return err }},
		{"mismatched services", func() error {
			return store.ReplaceServices(t.Context(), tenancy.DefaultScope(), "agent-1", []serviceinventory.Service{{AgentID: "agent-1", OrganizationID: "other", SiteID: "site_default"}})
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, tenancy.ErrInvalidScope) {
				t.Fatalf("scope not rejected: %v", err)
			}
		})
	}
}

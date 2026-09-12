package agent

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/serviceinventory"
)

func TestServiceCollectorBuildsTimestampedSortedSnapshot(t *testing.T) {
	observedAt := time.Date(2026, time.September, 12, 18, 0, 0, 0, time.FixedZone("TRT", 3*60*60))
	collector := &ServiceCollector{
		collect: func(context.Context) ([]serviceinventory.Fact, error) {
			return []serviceinventory.Fact{
				{Name: "zeta.service", State: "stopped", StartupType: "manual"},
				{Name: "alpha.service", State: "running", StartupType: "automatic"},
			}, nil
		},
		now: func() time.Time { return observedAt },
	}
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect services: %v", err)
	}
	if snapshot.ObservedAt.Location() != time.UTC || !reflect.DeepEqual([]string{snapshot.Services[0].Name, snapshot.Services[1].Name}, []string{"alpha.service", "zeta.service"}) {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestParseSystemdServicesMergesRuntimeAndStartupState(t *testing.T) {
	units := "nginx.service loaded active running NGINX Web Server\nfailed.service loaded failed failed Broken Worker\n"
	unitFiles := "nginx.service enabled enabled\nfailed.service disabled enabled\nidle.service static -\n"
	services, err := parseSystemdServices(units, unitFiles)
	if err != nil {
		t.Fatalf("parse systemd services: %v", err)
	}
	want := []serviceinventory.Fact{
		{Name: "failed.service", DisplayName: "Broken Worker", State: "failed", StartupType: "disabled"},
		{Name: "idle.service", State: "stopped", StartupType: "manual"},
		{Name: "nginx.service", DisplayName: "NGINX Web Server", State: "running", StartupType: "automatic"},
	}
	if !reflect.DeepEqual(services, want) {
		t.Fatalf("unexpected services:\n got: %#v\nwant: %#v", services, want)
	}
}

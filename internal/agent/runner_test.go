package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
)

func TestRunnerEnrollsAndReportsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	hub := &fakeHubClient{identity: testIdentity(t, "agent-test"), reported: make(chan inventory.Facts, 1)}
	collector := fakeCollector{facts: inventory.Facts{Hostname: "web-01"}}
	runner := Runner{Hub: hub, Collector: collector, ReportInterval: time.Hour, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Hostname: "web-01", OperatingSystem: "linux"}

	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case facts := <-hub.reported:
		if facts.Hostname != "web-01" || hub.enrollName != "web-01" || hub.enrollOS != "linux" {
			t.Fatalf("unexpected report or enrollment: %#v %#v", facts, hub)
		}
		cancel()
	case <-time.After(time.Second):
		t.Fatal("runner did not report immediately")
	}
	if err := <-done; err != nil {
		t.Fatalf("runner shutdown: %v", err)
	}
}

func TestRunnerRetriesAfterInitialHubFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := &fakeHubClient{identity: testIdentity(t, "agent-test"), reported: make(chan inventory.Facts, 1), failures: 1}
	runner := Runner{
		Hub: hub, Collector: fakeCollector{facts: inventory.Facts{Hostname: "web-01"}},
		ReportInterval: 5 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hostname: "web-01", OperatingSystem: "linux",
	}
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case <-hub.reported:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("runner did not recover after initial hub failure")
	}
	if err := <-done; err != nil {
		t.Fatalf("runner shutdown: %v", err)
	}
}

type fakeHubClient struct {
	identity   Identity
	enrollName string
	enrollOS   string
	reported   chan inventory.Facts
	failures   int
}

func (client *fakeHubClient) EnsureIdentity(_ context.Context, name, operatingSystem string) (Identity, error) {
	client.enrollName, client.enrollOS = name, operatingSystem
	if client.failures > 0 {
		client.failures--
		return Identity{}, errors.New("hub unavailable")
	}
	return client.identity, nil
}

func (client *fakeHubClient) ReportInventory(_ context.Context, _ Identity, facts inventory.Facts) error {
	client.reported <- facts
	return nil
}

type fakeCollector struct {
	facts inventory.Facts
	err   error
}

func (collector fakeCollector) Collect() (inventory.Facts, error) {
	return collector.facts, collector.err
}

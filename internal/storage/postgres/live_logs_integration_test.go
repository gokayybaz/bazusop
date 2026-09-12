package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/logstream"
)

func TestPostgresDeliversLiveLogsAcrossStores(t *testing.T) {
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	first, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	defer first.Close()
	second, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open second store: %v", err)
	}
	defer second.Close()

	agentID := "live-test-" + time.Now().UTC().Format("20060102150405.000000000")
	now := time.Now().UTC()
	if err := first.Upsert(ctx, inventory.Host{
		AgentID: agentID, Hostname: agentID, OSFamily: "linux", OSName: "Test",
		OSVersion: "1", Architecture: "amd64", CPUCores: 1, MemoryBytes: 1024,
		IPAddresses: []string{"127.0.0.1"}, AgentVersion: "test", FirstSeenAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatalf("seed integration host: %v", err)
	}
	defer func() { _, _ = first.pool.Exec(context.Background(), "DELETE FROM hosts WHERE agent_id=$1", agentID) }()

	streamingReplica := logstream.NewService(second)
	stream, err := streamingReplica.Subscribe(ctx, agentID)
	if err != nil {
		t.Fatalf("subscribe on second store: %v", err)
	}
	ingestingReplica := logstream.NewService(first)
	if err := ingestingReplica.Ingest(ctx, agentID, logstream.Batch{Entries: []logstream.Entry{{
		OccurredAt: now, Collector: logstream.CollectorFile, Source: "integration.log",
		Severity: logstream.SeverityInfo, Message: "cross-store delivery",
	}}}); err != nil {
		t.Fatalf("ingest on first store: %v", err)
	}

	select {
	case entry := <-stream:
		if entry.AgentID != agentID || entry.Message != "cross-store delivery" {
			t.Fatalf("unexpected live entry: %#v", entry)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for PostgreSQL live log delivery")
	}
}

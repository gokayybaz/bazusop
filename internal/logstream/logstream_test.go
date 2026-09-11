package logstream

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIngestNormalizesAndSearchesBoundedLogs(t *testing.T) {
	t.Parallel()
	manager := NewService(NewMemoryStore())
	base := time.Date(2026, 9, 11, 4, 0, 0, 0, time.UTC)
	err := manager.Ingest(context.Background(), " agent-01 ", Batch{Entries: []Entry{
		{OccurredAt: base, Collector: "JOURNALD", Source: " nginx.service ", Severity: "WARNING", Message: " upstream timeout "},
		{OccurredAt: base.Add(time.Minute), Collector: "file", Source: "/var/log/nginx/error.log", Severity: "error", Message: "connection refused"},
		{OccurredAt: base.Add(2 * time.Minute), Collector: "windows_event", Source: "System", Severity: "info", Message: "service recovered"},
	}})
	if err != nil {
		t.Fatalf("ingest logs: %v", err)
	}

	entries, err := manager.Search(context.Background(), Query{
		AgentID: "agent-01", From: base.Add(-time.Minute), To: base.Add(3 * time.Minute),
		Severity: SeverityError, Text: "refused", Limit: 50,
	})
	if err != nil {
		t.Fatalf("search logs: %v", err)
	}
	if len(entries) != 1 || entries[0].Collector != CollectorFile || entries[0].Message != "connection refused" {
		t.Fatalf("expected normalized matching log, got %#v", entries)
	}
	if entries[0].ID == "" {
		t.Fatal("expected generated log id")
	}

	entries, err = manager.Search(context.Background(), Query{AgentID: "agent-01", From: base.Add(-time.Minute), To: base.Add(3 * time.Minute), Limit: 2})
	if err != nil || len(entries) != 2 || !entries[0].OccurredAt.Equal(base.Add(time.Minute)) || !entries[1].OccurredAt.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("expected latest two logs in chronological order, got %#v, %v", entries, err)
	}
}

func TestSubscribersReceiveOnlyTheirAgentLogs(t *testing.T) {
	t.Parallel()
	manager := NewService(NewMemoryStore())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := manager.Subscribe(ctx, "agent-01")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	now := time.Now().UTC()
	if err := manager.Ingest(context.Background(), "agent-02", Batch{Entries: []Entry{{OccurredAt: now, Collector: "file", Source: "app.log", Severity: "info", Message: "ignore"}}}); err != nil {
		t.Fatalf("ingest other agent: %v", err)
	}
	if err := manager.Ingest(context.Background(), "agent-01", Batch{Entries: []Entry{{OccurredAt: now, Collector: "file", Source: "app.log", Severity: "info", Message: "deliver"}}}); err != nil {
		t.Fatalf("ingest subscribed agent: %v", err)
	}
	select {
	case entry := <-stream:
		if entry.Message != "deliver" || entry.AgentID != "agent-01" {
			t.Fatalf("unexpected streamed entry: %#v", entry)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live log")
	}
}

func TestIngestAndSearchRejectInvalidInputs(t *testing.T) {
	t.Parallel()
	manager := NewService(NewMemoryStore())
	if err := manager.Ingest(context.Background(), "agent-01", Batch{Entries: []Entry{{Collector: "syslog", Severity: "loud"}}}); !errors.Is(err, ErrInvalidLogs) {
		t.Fatalf("expected invalid batch, got %v", err)
	}
	if _, err := manager.Search(context.Background(), Query{AgentID: "agent-01", From: time.Now(), To: time.Now().Add(-time.Minute), Limit: 10}); !errors.Is(err, ErrInvalidLogs) {
		t.Fatalf("expected invalid query, got %v", err)
	}
}

package agent

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/logstream"
)

func TestLogCollectorAdvancesCursorOnlyAfterSuccessfulCollection(t *testing.T) {
	start := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	now := start.Add(time.Minute)
	var calls []time.Time
	collector := &LogCollector{
		cursor: start,
		now:    func() time.Time { return now },
		collect: func(_ context.Context, since, until time.Time, limit int) ([]logstream.Entry, error) {
			calls = append(calls, since)
			if limit != logstream.MaxBatchEntries || !until.Equal(now) {
				t.Fatalf("unexpected collection window: %s %s %d", since, until, limit)
			}
			return []logstream.Entry{{OccurredAt: now.Add(-time.Second), Collector: "journald", Source: "sshd.service", Severity: "info", Message: "session opened"}}, nil
		},
	}
	batch, err := collector.Collect(context.Background())
	if err != nil || len(batch.Entries) != 1 {
		t.Fatalf("collect logs: %#v %v", batch, err)
	}
	now = now.Add(time.Minute)
	retry, _ := collector.Collect(context.Background())
	if len(retry.Entries) != 1 || len(calls) != 1 {
		t.Fatalf("uncommitted batch was not retried: batch=%#v calls=%#v", retry, calls)
	}
	collector.Commit()
	_, _ = collector.Collect(context.Background())
	if !reflect.DeepEqual(calls, []time.Time{start, start.Add(time.Minute)}) {
		t.Fatalf("unexpected cursors: %#v", calls)
	}
}

func TestParseJournalEntriesNormalizesPriorityAndSource(t *testing.T) {
	input := "{\"__REALTIME_TIMESTAMP\":\"1789214400000000\",\"_SYSTEMD_UNIT\":\"nginx.service\",\"SYSLOG_IDENTIFIER\":\"nginx\",\"PRIORITY\":\"3\",\"MESSAGE\":\"upstream timeout\"}\n" +
		"{\"__REALTIME_TIMESTAMP\":\"1789214401000000\",\"SYSLOG_IDENTIFIER\":\"kernel\",\"PRIORITY\":\"7\",\"MESSAGE\":\"debug detail\"}\n"
	entries, err := parseJournalEntries([]byte(input))
	if err != nil {
		t.Fatalf("parse journal: %v", err)
	}
	want := []logstream.Entry{
		{OccurredAt: time.Unix(1789214400, 0).UTC(), Collector: "journald", Source: "nginx.service", Severity: "error", Message: "upstream timeout"},
		{OccurredAt: time.Unix(1789214401, 0).UTC(), Collector: "journald", Source: "kernel", Severity: "debug", Message: "debug detail"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("unexpected entries:\n got: %#v\nwant: %#v", entries, want)
	}
}

func TestParseWindowsEventEntriesNormalizesLevel(t *testing.T) {
	input := "{\"TimeCreated\":\"2026-09-12T12:00:00Z\",\"ProviderName\":\"Service Control Manager\",\"LevelDisplayName\":\"Warning\",\"Message\":\"service restarted\"}\n"
	entries, err := parseWindowsEventEntries([]byte(input))
	if err != nil {
		t.Fatalf("parse Windows events: %v", err)
	}
	if len(entries) != 1 || entries[0].Collector != "windows_event" || entries[0].Severity != "warn" || entries[0].Source != "Service Control Manager" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gokayybaz/bazusop/internal/logstream"
)

type LogCollector struct {
	mu          sync.Mutex
	collect     func(context.Context, time.Time, time.Time, int) ([]logstream.Entry, error)
	now         func() time.Time
	cursor      time.Time
	pending     time.Time
	batch       logstream.Batch
	store       LogCheckpointStore
	initialized bool
}

func NewLogCollector(lookback time.Duration, stores ...LogCheckpointStore) *LogCollector {
	if lookback <= 0 {
		lookback = time.Minute
	}
	now := time.Now().UTC()
	collector := &LogCollector{collect: collectPlatformLogs, now: time.Now, cursor: now.Add(-lookback)}
	if len(stores) > 0 {
		collector.store = stores[0]
	}
	return collector
}

func (collector *LogCollector) Collect(ctx context.Context) (logstream.Batch, error) {
	if collector == nil || collector.collect == nil || collector.now == nil {
		return logstream.Batch{}, errors.New("log collector is incomplete")
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if err := collector.initialize(); err != nil {
		return logstream.Batch{}, err
	}
	if collector.cursor.IsZero() {
		return logstream.Batch{}, errors.New("log collector is incomplete")
	}
	if !collector.pending.IsZero() {
		return collector.batch, nil
	}
	until := collector.now().UTC()
	if !collector.cursor.Before(until) {
		return logstream.Batch{Entries: []logstream.Entry{}}, nil
	}
	entries, err := collector.collect(ctx, collector.cursor, until, logstream.MaxBatchEntries)
	if err != nil {
		return logstream.Batch{}, err
	}
	filtered := entries[:0]
	for _, entry := range entries {
		entry.OccurredAt = entry.OccurredAt.UTC()
		if entry.OccurredAt.After(collector.cursor) && !entry.OccurredAt.After(until) {
			filtered = append(filtered, entry)
		}
	}
	entries = filtered
	sort.SliceStable(entries, func(left, right int) bool { return entries[left].OccurredAt.Before(entries[right].OccurredAt) })
	if len(entries) > logstream.MaxBatchEntries {
		entries = entries[len(entries)-logstream.MaxBatchEntries:]
	}
	collector.pending = until
	collector.batch = logstream.Batch{Entries: entries}
	return collector.batch, nil
}

func (collector *LogCollector) Commit() error {
	if collector == nil {
		return nil
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if collector.pending.IsZero() {
		return nil
	}
	if collector.store != nil {
		if err := collector.store.Save(collector.pending); err != nil {
			return fmt.Errorf("persist log checkpoint: %w", err)
		}
	}
	collector.cursor = collector.pending
	collector.pending = time.Time{}
	collector.batch = logstream.Batch{}
	return nil
}

func (collector *LogCollector) initialize() error {
	if collector.initialized {
		return nil
	}
	collector.initialized = true
	if collector.store == nil {
		return nil
	}
	cursor, err := collector.store.Load()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		collector.initialized = false
		return fmt.Errorf("load log checkpoint: %w", err)
	}
	if cursor.After(collector.now().UTC()) {
		collector.initialized = false
		return errors.New("stored log checkpoint is in the future")
	}
	collector.cursor = cursor
	return nil
}

type journalRecord struct {
	Cursor     string          `json:"__CURSOR"`
	Timestamp  string          `json:"__REALTIME_TIMESTAMP"`
	Unit       string          `json:"_SYSTEMD_UNIT"`
	Identifier string          `json:"SYSLOG_IDENTIFIER"`
	Command    string          `json:"_COMM"`
	Priority   string          `json:"PRIORITY"`
	Message    json.RawMessage `json:"MESSAGE"`
}

func parseJournalEntries(output []byte) ([]logstream.Entry, error) {
	entries := make([]logstream.Entry, 0)
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var record journalRecord
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("decode journal entry: %w", err)
		}
		micros, err := strconv.ParseInt(record.Timestamp, 10, 64)
		if err != nil {
			continue
		}
		message := decodeJournalMessage(record.Message)
		if message == "" {
			continue
		}
		source := firstNonEmpty(record.Unit, record.Identifier, record.Command, "journald")
		entries = append(entries, logstream.Entry{
			OccurredAt: time.Unix(0, micros*int64(time.Microsecond)).UTC(), Collector: logstream.CollectorJournald,
			Source: truncateUTF8(source, 512), Severity: syslogSeverity(record.Priority), Message: truncateUTF8(message, 64*1024), SourceID: record.Cursor,
		})
	}
	return entries, nil
}

func decodeJournalMessage(raw json.RawMessage) string {
	var message string
	if json.Unmarshal(raw, &message) == nil {
		return strings.TrimSpace(message)
	}
	var encoded []byte
	if json.Unmarshal(raw, &encoded) == nil {
		return strings.TrimSpace(string(encoded))
	}
	return ""
}

func syslogSeverity(priority string) logstream.Severity {
	value, err := strconv.Atoi(priority)
	if err != nil {
		return logstream.SeverityInfo
	}
	switch {
	case value <= 2:
		return logstream.SeverityCritical
	case value == 3:
		return logstream.SeverityError
	case value == 4:
		return logstream.SeverityWarn
	case value == 7:
		return logstream.SeverityDebug
	default:
		return logstream.SeverityInfo
	}
}

type windowsEventRecord struct {
	TimeCreated      time.Time `json:"TimeCreated"`
	ProviderName     string    `json:"ProviderName"`
	Level            int       `json:"Level"`
	LevelDisplayName string    `json:"LevelDisplayName"`
	Message          string    `json:"Message"`
	RecordID         int64     `json:"RecordId"`
}

func parseWindowsEventEntries(output []byte) ([]logstream.Entry, error) {
	entries := make([]logstream.Entry, 0)
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var record windowsEventRecord
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("decode Windows event: %w", err)
		}
		message := strings.TrimSpace(record.Message)
		if record.TimeCreated.IsZero() || message == "" {
			continue
		}
		entries = append(entries, logstream.Entry{
			OccurredAt: record.TimeCreated.UTC(), Collector: logstream.CollectorWindowsEvent,
			Source:   truncateUTF8(firstNonEmpty(record.ProviderName, "Windows Event Log"), 512),
			Severity: windowsEventSeverity(record.Level, record.LevelDisplayName), Message: truncateUTF8(message, 64*1024), SourceID: strconv.FormatInt(record.RecordID, 10),
		})
	}
	return entries, nil
}

func windowsEventSeverity(level int, display string) logstream.Severity {
	switch level {
	case 1:
		return logstream.SeverityCritical
	case 2:
		return logstream.SeverityError
	case 3:
		return logstream.SeverityWarn
	case 5:
		return logstream.SeverityDebug
	case 4:
		return logstream.SeverityInfo
	}
	switch strings.ToLower(strings.TrimSpace(display)) {
	case "critical", "kritik":
		return logstream.SeverityCritical
	case "error", "hata":
		return logstream.SeverityError
	case "warning", "warn", "uyarı":
		return logstream.SeverityWarn
	case "debug", "verbose":
		return logstream.SeverityDebug
	default:
		return logstream.SeverityInfo
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

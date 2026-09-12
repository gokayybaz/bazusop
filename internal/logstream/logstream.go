package logstream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrInvalidLogs = errors.New("invalid logs")

const (
	MaxBatchEntries      = 1000
	LiveSubscriberBuffer = MaxBatchEntries
)

type Collector string

const (
	CollectorJournald     Collector = "journald"
	CollectorFile         Collector = "file"
	CollectorWindowsEvent Collector = "windows_event"
)

type Severity string

const (
	SeverityDebug    Severity = "debug"
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

type Entry struct {
	ID         string    `json:"id,omitempty"`
	AgentID    string    `json:"agent_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
	Collector  Collector `json:"collector"`
	Source     string    `json:"source"`
	Severity   Severity  `json:"severity"`
	Message    string    `json:"message"`
}

type Batch struct {
	Entries []Entry `json:"entries"`
}

type Query struct {
	AgentID   string
	From      time.Time
	To        time.Time
	Collector Collector
	Severity  Severity
	Source    string
	Text      string
	Limit     int
}

type Store interface {
	AppendLogs(context.Context, []Entry) error
	SearchLogs(context.Context, Query) ([]Entry, error)
}

// SharedLiveStore lets multiple hub processes share live log delivery through
// their common durable store while the default memory store stays process-local.
type SharedLiveStore interface {
	SubscribeLogs(context.Context, string) (<-chan Entry, error)
}

type Service struct {
	store       Store
	mu          sync.RWMutex
	subscribers map[string]map[chan Entry]struct{}
}

func NewService(store Store) *Service {
	return &Service{store: store, subscribers: make(map[string]map[chan Entry]struct{})}
}

func (service *Service) Ingest(ctx context.Context, agentID string, batch Batch) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || len(batch.Entries) == 0 || len(batch.Entries) > MaxBatchEntries {
		return ErrInvalidLogs
	}
	entries := make([]Entry, 0, len(batch.Entries))
	for _, candidate := range batch.Entries {
		collector, collectorValid := normalizeCollector(string(candidate.Collector))
		severity, severityValid := normalizeSeverity(string(candidate.Severity))
		source := strings.TrimSpace(candidate.Source)
		message := strings.TrimSpace(candidate.Message)
		if candidate.OccurredAt.IsZero() || !collectorValid || !severityValid || source == "" || len(source) > 512 || message == "" || len(message) > 64*1024 {
			return ErrInvalidLogs
		}
		id, err := newID()
		if err != nil {
			return err
		}
		entries = append(entries, Entry{
			ID: id, AgentID: agentID, OccurredAt: candidate.OccurredAt.UTC(),
			Collector: collector, Source: source, Severity: severity, Message: message,
		})
	}
	if err := service.store.AppendLogs(ctx, entries); err != nil {
		return err
	}
	if _, shared := service.store.(SharedLiveStore); !shared {
		for _, entry := range entries {
			service.publish(entry)
		}
	}
	return nil
}

func (service *Service) Search(ctx context.Context, query Query) ([]Entry, error) {
	query.AgentID = strings.TrimSpace(query.AgentID)
	query.Source = strings.TrimSpace(query.Source)
	query.Text = strings.TrimSpace(query.Text)
	if query.AgentID == "" || query.From.IsZero() || query.To.IsZero() || !query.From.Before(query.To) || query.Limit < 1 || query.Limit > 500 || len(query.Source) > 512 || len(query.Text) > 512 {
		return nil, ErrInvalidLogs
	}
	if query.Collector != "" {
		collector, valid := normalizeCollector(string(query.Collector))
		if !valid {
			return nil, ErrInvalidLogs
		}
		query.Collector = collector
	}
	if query.Severity != "" {
		severity, valid := normalizeSeverity(string(query.Severity))
		if !valid {
			return nil, ErrInvalidLogs
		}
		query.Severity = severity
	}
	query.From = query.From.UTC()
	query.To = query.To.UTC()
	return service.store.SearchLogs(ctx, query)
}

func (service *Service) Subscribe(ctx context.Context, agentID string) (<-chan Entry, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, ErrInvalidLogs
	}
	if shared, ok := service.store.(SharedLiveStore); ok {
		return shared.SubscribeLogs(ctx, agentID)
	}
	stream := make(chan Entry, LiveSubscriberBuffer)
	service.mu.Lock()
	if service.subscribers[agentID] == nil {
		service.subscribers[agentID] = make(map[chan Entry]struct{})
	}
	service.subscribers[agentID][stream] = struct{}{}
	service.mu.Unlock()
	go func() {
		<-ctx.Done()
		service.mu.Lock()
		delete(service.subscribers[agentID], stream)
		if len(service.subscribers[agentID]) == 0 {
			delete(service.subscribers, agentID)
		}
		service.mu.Unlock()
	}()
	return stream, nil
}

func (service *Service) publish(entry Entry) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	for stream := range service.subscribers[entry.AgentID] {
		select {
		case stream <- entry:
		default:
		}
	}
}

func normalizeCollector(value string) (Collector, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "journald":
		return CollectorJournald, true
	case "file":
		return CollectorFile, true
	case "windows_event", "windows-event", "eventlog":
		return CollectorWindowsEvent, true
	default:
		return "", false
	}
}

func normalizeSeverity(value string) (Severity, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return SeverityDebug, true
	case "info", "information":
		return SeverityInfo, true
	case "warn", "warning":
		return SeverityWarn, true
	case "error", "err":
		return SeverityError, true
	case "critical", "fatal":
		return SeverityCritical, true
	default:
		return "", false
	}
}

func newID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string][]Entry
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{entries: make(map[string][]Entry)} }

func (store *MemoryStore) AppendLogs(_ context.Context, entries []Entry) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	affected := make(map[string]struct{})
	for _, entry := range entries {
		store.entries[entry.AgentID] = append(store.entries[entry.AgentID], entry)
		affected[entry.AgentID] = struct{}{}
	}
	for agentID := range affected {
		values := store.entries[agentID]
		sort.Slice(values, func(left, right int) bool {
			if values[left].OccurredAt.Equal(values[right].OccurredAt) {
				return values[left].ID < values[right].ID
			}
			return values[left].OccurredAt.Before(values[right].OccurredAt)
		})
		if len(values) > 10000 {
			values = values[len(values)-10000:]
		}
		store.entries[agentID] = values
	}
	return nil
}

func (store *MemoryStore) SearchLogs(_ context.Context, query Query) ([]Entry, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	result := make([]Entry, 0)
	for _, entry := range store.entries[query.AgentID] {
		if entry.OccurredAt.Before(query.From) || entry.OccurredAt.After(query.To) {
			continue
		}
		if query.Collector != "" && entry.Collector != query.Collector {
			continue
		}
		if query.Severity != "" && entry.Severity != query.Severity {
			continue
		}
		if query.Source != "" && !strings.Contains(strings.ToLower(entry.Source), strings.ToLower(query.Source)) {
			continue
		}
		if query.Text != "" && !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(query.Text)) {
			continue
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].OccurredAt.Equal(result[right].OccurredAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].OccurredAt.Before(result[right].OccurredAt)
	})
	if len(result) > query.Limit {
		result = result[len(result)-query.Limit:]
	}
	return append(make([]Entry, 0, len(result)), result...), nil
}

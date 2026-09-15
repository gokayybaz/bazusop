package logstream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

var ErrInvalidLogs = errors.New("invalid logs")
var entryIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

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
	OrganizationID string    `json:"organization_id"`
	SiteID         string    `json:"site_id"`
	ID             string    `json:"id,omitempty"`
	AgentID        string    `json:"agent_id,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
	Collector      Collector `json:"collector"`
	Source         string    `json:"source"`
	Severity       Severity  `json:"severity"`
	Message        string    `json:"message"`
	SourceID       string    `json:"-"`
}

type Batch struct {
	Entries []Entry `json:"entries"`
}

type Query struct {
	Scope     tenancy.Scope
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
	AppendLogs(context.Context, []Entry) ([]Entry, error)
	SearchLogs(context.Context, Query) ([]Entry, error)
}

// SharedLiveStore lets multiple hub processes share live log delivery through
// their common durable store while the default memory store stays process-local.
type SharedLiveStore interface {
	SubscribeLogs(context.Context, tenancy.Scope, string) (<-chan Entry, error)
}

type Service struct {
	store       Store
	mu          sync.RWMutex
	subscribers map[tenancy.Agent]map[chan Entry]struct{}
}

func NewService(store Store) *Service {
	return &Service{store: store, subscribers: make(map[tenancy.Agent]map[chan Entry]struct{})}
}

func (service *Service) Ingest(ctx context.Context, agent tenancy.Agent, batch Batch) error {
	if err := agent.Validate(); err != nil {
		return err
	}
	agentID := strings.TrimSpace(agent.ID)
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
		id := strings.TrimSpace(candidate.ID)
		if id == "" {
			var err error
			id, err = newID()
			if err != nil {
				return err
			}
		} else if !entryIDPattern.MatchString(id) {
			return ErrInvalidLogs
		}
		entries = append(entries, Entry{
			OrganizationID: agent.OrganizationID, SiteID: agent.SiteID,
			ID: id, AgentID: agentID, OccurredAt: candidate.OccurredAt.UTC(),
			Collector: collector, Source: source, Severity: severity, Message: message,
		})
	}
	stored, err := service.store.AppendLogs(ctx, entries)
	if err != nil {
		return err
	}
	if _, shared := service.store.(SharedLiveStore); !shared {
		for _, entry := range stored {
			service.publish(entry)
		}
	}
	return nil
}

func (service *Service) Search(ctx context.Context, query Query) ([]Entry, error) {
	if err := query.Scope.Validate(); err != nil {
		return nil, err
	}
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

func (service *Service) Subscribe(ctx context.Context, scope tenancy.Scope, agentID string) (<-chan Entry, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, ErrInvalidLogs
	}
	if shared, ok := service.store.(SharedLiveStore); ok {
		return shared.SubscribeLogs(ctx, scope, agentID)
	}
	key := tenancy.Agent{ID: agentID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}
	stream := make(chan Entry, LiveSubscriberBuffer)
	service.mu.Lock()
	if service.subscribers[key] == nil {
		service.subscribers[key] = make(map[chan Entry]struct{})
	}
	service.subscribers[key][stream] = struct{}{}
	service.mu.Unlock()
	go func() {
		<-ctx.Done()
		service.mu.Lock()
		delete(service.subscribers[key], stream)
		if len(service.subscribers[key]) == 0 {
			delete(service.subscribers, key)
		}
		service.mu.Unlock()
	}()
	return stream, nil
}

func (service *Service) publish(entry Entry) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	for stream := range service.subscribers[tenancy.Agent{ID: entry.AgentID, OrganizationID: entry.OrganizationID, SiteID: entry.SiteID}] {
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
	entries map[tenancy.Agent][]Entry
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{entries: make(map[tenancy.Agent][]Entry)} }

func (store *MemoryStore) AppendLogs(_ context.Context, entries []Entry) ([]Entry, error) {
	for _, entry := range entries {
		if err := (tenancy.Agent{ID: entry.AgentID, OrganizationID: entry.OrganizationID, SiteID: entry.SiteID}).Validate(); err != nil {
			return nil, err
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	affected := make(map[tenancy.Agent]struct{})
	type logKey struct {
		Scope tenancy.Scope
		ID    string
		At    time.Time
	}
	existingKeys := make(map[logKey]struct{})
	for _, values := range store.entries {
		for _, entry := range values {
			existingKeys[logKey{tenancy.Scope{OrganizationID: entry.OrganizationID, SiteID: entry.SiteID}, entry.ID, entry.OccurredAt.UTC()}] = struct{}{}
		}
	}
	stored := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		key := logKey{tenancy.Scope{OrganizationID: entry.OrganizationID, SiteID: entry.SiteID}, entry.ID, entry.OccurredAt.UTC()}
		if _, duplicate := existingKeys[key]; duplicate {
			continue
		}
		existingKeys[key] = struct{}{}
		agent := tenancy.Agent{ID: entry.AgentID, OrganizationID: entry.OrganizationID, SiteID: entry.SiteID}
		store.entries[agent] = append(store.entries[agent], entry)
		stored = append(stored, entry)
		affected[agent] = struct{}{}
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
	return stored, nil
}

func (store *MemoryStore) SearchLogs(_ context.Context, query Query) ([]Entry, error) {
	if err := query.Scope.Validate(); err != nil {
		return nil, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	result := make([]Entry, 0)
	for _, entry := range store.entries[tenancy.Agent{ID: query.AgentID, OrganizationID: query.Scope.OrganizationID, SiteID: query.Scope.SiteID}] {
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

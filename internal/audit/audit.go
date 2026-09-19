// Package audit merges the job and alert lifecycle trails into a single, site-scoped,
// time-ordered feed. Job events (approved/claimed/output/succeeded/failed) and alert events
// (opened/acknowledged/resolved) are the only two append-only, actor-attributed event logs in
// the system; this package does not introduce a new one, it reads the existing two together.
package audit

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

var ErrInvalidAudit = errors.New("invalid audit query")

type Source string

const (
	SourceJob   Source = "job"
	SourceAlert Source = "alert"
)

// Event is the common shape a job event or an alert event is projected into. ReferenceID is
// the parent job or incident ID; the full per-parent record remains reachable through the
// existing job/incident audit endpoints.
type Event struct {
	OrganizationID string    `json:"organization_id"`
	SiteID         string    `json:"site_id"`
	Source         Source    `json:"source"`
	ReferenceID    string    `json:"reference_id"`
	AgentID        string    `json:"agent_id"`
	Type           string    `json:"type"`
	Actor          string    `json:"actor"`
	Message        string    `json:"message"`
	OccurredAt     time.Time `json:"occurred_at"`
}

type Store interface {
	ListAuditEvents(ctx context.Context, scope tenancy.Scope, limit int) ([]Event, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (service *Service) List(ctx context.Context, scope tenancy.Scope, limit int) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		return nil, ErrInvalidAudit
	}
	return service.store.ListAuditEvents(ctx, scope, limit)
}

// MemoryStore merges the job and alert event trails held by the in-process development
// stores into a single scoped, time-ordered feed. It exists because those stores keep private
// in-memory state with no shared table to query, unlike storage/postgres, which unions
// job_events and alert_events directly in SQL.
type MemoryStore struct {
	jobs   *jobs.MemoryStore
	alerts *alerting.MemoryStore
}

func NewMemoryStore(jobStore *jobs.MemoryStore, alertStore *alerting.MemoryStore) *MemoryStore {
	return &MemoryStore{jobs: jobStore, alerts: alertStore}
}

func (store *MemoryStore) ListAuditEvents(ctx context.Context, scope tenancy.Scope, limit int) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	jobRecords, err := store.jobs.AllEvents(ctx, scope)
	if err != nil {
		return nil, err
	}
	alertRecords, err := store.alerts.AllEvents(ctx, scope)
	if err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(jobRecords)+len(alertRecords))
	for _, record := range jobRecords {
		events = append(events, Event{
			OrganizationID: record.Event.OrganizationID,
			SiteID:         record.Event.SiteID,
			Source:         SourceJob,
			ReferenceID:    record.Event.JobID,
			AgentID:        record.AgentID,
			Type:           string(record.Event.Type),
			Actor:          record.Event.Actor,
			Message:        record.Event.Message,
			OccurredAt:     record.Event.OccurredAt,
		})
	}
	for _, record := range alertRecords {
		events = append(events, Event{
			OrganizationID: record.Event.OrganizationID,
			SiteID:         record.Event.SiteID,
			Source:         SourceAlert,
			ReferenceID:    record.Event.IncidentID,
			AgentID:        record.AgentID,
			Type:           string(record.Event.Type),
			Actor:          record.Event.Actor,
			Message:        record.Event.Message,
			OccurredAt:     record.Event.OccurredAt,
		})
	}
	sort.Slice(events, func(left, right int) bool {
		if events[left].OccurredAt.Equal(events[right].OccurredAt) {
			if events[left].Source != events[right].Source {
				return events[left].Source < events[right].Source
			}
			return events[left].ReferenceID < events[right].ReferenceID
		}
		return events[left].OccurredAt.After(events[right].OccurredAt)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

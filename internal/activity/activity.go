// Package activity merges human-meaningful successful state changes from
// every domain into a single, site-scoped, time-ordered feed: job and alert
// lifecycle events (approved/claimed/succeeded, opened/acknowledged/resolved)
// plus identity, site-role and service-account events recorded directly by
// internal/server's HTTP handlers after a mutation succeeds. This is the
// "Activity" concept from the design spec — human-readable successes only,
// failures and reads stay in internal/audittrail. This package was renamed
// from internal/audit in spike 11.8: the old name collided with the spec's
// separate, security-focused "Audit" concept internal/audittrail implements.
package activity

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

var ErrInvalidActivity = errors.New("invalid activity query")

type Source string

const (
	SourceJob            Source = "job"
	SourceAlert          Source = "alert"
	SourceIdentity       Source = "identity"
	SourceSiteRole       Source = "site_role"
	SourceServiceAccount Source = "service_account"
)

// Event is the common shape every source is projected into. ReferenceID is
// the parent job/incident/invite/user/service-account ID; the full
// per-parent record remains reachable through that domain's own endpoints
// where one exists (jobs, alerts).
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
	ListActivityEvents(ctx context.Context, scope tenancy.Scope, limit int) ([]Event, error)
	RecordActivityEvent(ctx context.Context, event Event) error
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
		return nil, ErrInvalidActivity
	}
	return service.store.ListActivityEvents(ctx, scope, limit)
}

// Record stores a single identity/site-role/service-account activity event.
// It is called directly by internal/server's HTTP handlers, best-effort,
// after the mutation it describes has already succeeded — the same
// fire-and-forget posture internal/audittrail uses: a write failure here
// must never take the API down or roll back the mutation it describes.
func (service *Service) Record(ctx context.Context, event Event) error {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	return service.store.RecordActivityEvent(ctx, event)
}

// MemoryStore merges the job and alert event trails held by the in-process
// development stores with identity/site-role/service-account events recorded
// directly through Record, into a single scoped, time-ordered feed. It
// exists because those stores keep private in-memory state with no shared
// table to query, unlike storage/postgres, which unions job_events,
// alert_events and activity_events directly in SQL.
type MemoryStore struct {
	jobs     *jobs.MemoryStore
	alerts   *alerting.MemoryStore
	mu       sync.Mutex
	recorded []Event
}

func NewMemoryStore(jobStore *jobs.MemoryStore, alertStore *alerting.MemoryStore) *MemoryStore {
	return &MemoryStore{jobs: jobStore, alerts: alertStore}
}

func (store *MemoryStore) RecordActivityEvent(_ context.Context, event Event) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.recorded = append(store.recorded, event)
	return nil
}

func (store *MemoryStore) ListActivityEvents(ctx context.Context, scope tenancy.Scope, limit int) ([]Event, error) {
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
			OrganizationID: record.Event.OrganizationID, SiteID: record.Event.SiteID,
			Source: SourceJob, ReferenceID: record.Event.JobID, AgentID: record.AgentID,
			Type: string(record.Event.Type), Actor: record.Event.Actor,
			Message: record.Event.Message, OccurredAt: record.Event.OccurredAt,
		})
	}
	for _, record := range alertRecords {
		events = append(events, Event{
			OrganizationID: record.Event.OrganizationID, SiteID: record.Event.SiteID,
			Source: SourceAlert, ReferenceID: record.Event.IncidentID, AgentID: record.AgentID,
			Type: string(record.Event.Type), Actor: record.Event.Actor,
			Message: record.Event.Message, OccurredAt: record.Event.OccurredAt,
		})
	}
	store.mu.Lock()
	for _, event := range store.recorded {
		if event.OrganizationID == scope.OrganizationID && event.SiteID == scope.SiteID {
			events = append(events, event)
		}
	}
	store.mu.Unlock()
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

package activity_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func seedStores(t *testing.T, scope, otherScope tenancy.Scope, now time.Time) (*jobs.MemoryStore, *alerting.MemoryStore) {
	t.Helper()
	jobStore := jobs.NewMemoryStore()
	job := jobs.Job{ID: "job-1", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, AgentID: "agent-1", Action: jobs.ActionServiceRestart, Target: "nginx.service", ApprovedBy: "gokay", Reason: "config değişikliği", RequestedAt: now, Status: jobs.StatusQueued}
	jobEvent := jobs.Event{JobID: job.ID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, Sequence: 0, Type: jobs.EventApproved, Message: job.Reason, Actor: job.ApprovedBy, OccurredAt: now}
	if err := jobStore.CreateJob(t.Context(), job, jobEvent); err != nil {
		t.Fatalf("seed job event: %v", err)
	}
	otherJob := jobs.Job{ID: "job-2", OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, AgentID: "agent-2", Action: jobs.ActionHostReboot, ApprovedBy: "someone", Reason: "patch", RequestedAt: now, Status: jobs.StatusQueued}
	otherJobEvent := jobs.Event{JobID: otherJob.ID, OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, Sequence: 0, Type: jobs.EventApproved, Message: otherJob.Reason, Actor: otherJob.ApprovedBy, OccurredAt: now}
	if err := jobStore.CreateJob(t.Context(), otherJob, otherJobEvent); err != nil {
		t.Fatalf("seed other-scope job event: %v", err)
	}

	alertStore := alerting.NewMemoryStore()
	incident := alerting.Incident{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "incident-1", RuleID: "rule-1", RuleName: "Yüksek CPU", AgentID: "agent-1", Severity: alerting.SeverityCritical, Status: alerting.StatusOpen, Message: "CPU %96", LatestValue: 96, OpenedAt: now.Add(time.Minute)}
	incidentEvent := alerting.Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "event-1", IncidentID: incident.ID, Type: alerting.EventOpened, Actor: "hub", Message: "CPU %96", OccurredAt: now.Add(time.Minute)}
	if _, _, err := alertStore.EnsureIncident(t.Context(), scope, incident, incidentEvent); err != nil {
		t.Fatalf("seed alert event: %v", err)
	}
	otherIncident := alerting.Incident{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "incident-2", RuleID: "rule-2", RuleName: "Disk kritik", AgentID: "agent-2", Severity: alerting.SeverityCritical, Status: alerting.StatusOpen, Message: "Disk %98", LatestValue: 98, OpenedAt: now.Add(time.Minute)}
	otherIncidentEvent := alerting.Event{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "event-2", IncidentID: otherIncident.ID, Type: alerting.EventOpened, Actor: "hub", Message: "Disk %98", OccurredAt: now.Add(time.Minute)}
	if _, _, err := alertStore.EnsureIncident(t.Context(), otherScope, otherIncident, otherIncidentEvent); err != nil {
		t.Fatalf("seed other-scope alert event: %v", err)
	}

	return jobStore, alertStore
}

func TestMemoryStoreMergesJobAndAlertEventsInScopeOrderedByRecency(t *testing.T) {
	t.Parallel()
	scope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	otherScope := tenancy.Scope{OrganizationID: "org-b", SiteID: "site-b"}
	now := time.Now().UTC().Truncate(time.Microsecond)
	jobStore, alertStore := seedStores(t, scope, otherScope, now)

	store := activity.NewMemoryStore(jobStore, alertStore)
	events, err := store.ListActivityEvents(t.Context(), scope, 10)
	if err != nil {
		t.Fatalf("list activity events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 in-scope events, got %d: %#v", len(events), events)
	}
	if events[0].Source != activity.SourceAlert || events[0].ReferenceID != "incident-1" || events[0].AgentID != "agent-1" {
		t.Fatalf("expected the more recent alert event first, got %#v", events[0])
	}
	if events[1].Source != activity.SourceJob || events[1].ReferenceID != "job-1" || events[1].AgentID != "agent-1" {
		t.Fatalf("expected the older job event second, got %#v", events[1])
	}
	for _, event := range events {
		if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
			t.Fatalf("leaked event from another scope: %#v", event)
		}
	}
}

func TestMemoryStoreRespectsLimit(t *testing.T) {
	t.Parallel()
	scope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	otherScope := tenancy.Scope{OrganizationID: "org-b", SiteID: "site-b"}
	now := time.Now().UTC().Truncate(time.Microsecond)
	jobStore, alertStore := seedStores(t, scope, otherScope, now)

	store := activity.NewMemoryStore(jobStore, alertStore)
	events, err := store.ListActivityEvents(t.Context(), scope, 1)
	if err != nil {
		t.Fatalf("list activity events: %v", err)
	}
	if len(events) != 1 || events[0].Source != activity.SourceAlert {
		t.Fatalf("expected exactly the most recent event, got %#v", events)
	}
}

func TestMemoryStoreRejectsInvalidScope(t *testing.T) {
	t.Parallel()
	store := activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore())
	if _, err := store.ListActivityEvents(t.Context(), tenancy.Scope{}, 10); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}
}

func TestServiceRejectsOutOfRangeLimit(t *testing.T) {
	t.Parallel()
	service := activity.NewService(activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore()))
	scope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	if _, err := service.List(t.Context(), scope, 0); !errors.Is(err, activity.ErrInvalidActivity) {
		t.Fatalf("expected ErrInvalidActivity for zero limit, got %v", err)
	}
	if _, err := service.List(t.Context(), scope, 501); !errors.Is(err, activity.ErrInvalidActivity) {
		t.Fatalf("expected ErrInvalidActivity for oversized limit, got %v", err)
	}
}

func TestServiceValidatesScopeBeforeLimit(t *testing.T) {
	t.Parallel()
	service := activity.NewService(activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore()))
	if _, err := service.List(t.Context(), tenancy.Scope{}, 10); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}
}

func TestServiceRecordMergesIntoScopedList(t *testing.T) {
	t.Parallel()
	store := activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore())
	service := activity.NewService(store)
	scope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	otherScope := tenancy.Scope{OrganizationID: "org-b", SiteID: "site-b"}

	if err := service.Record(t.Context(), activity.Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, Source: activity.SourceIdentity, ReferenceID: "invite-1", Type: "invite_created", Actor: "admin", Message: "someone@example.com davet edildi"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := service.Record(t.Context(), activity.Event{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, Source: activity.SourceIdentity, ReferenceID: "invite-2", Type: "invite_created", Actor: "admin", Message: "other-scope invite"}); err != nil {
		t.Fatalf("record other scope: %v", err)
	}

	events, err := service.List(t.Context(), scope, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 || events[0].Source != activity.SourceIdentity || events[0].ReferenceID != "invite-1" {
		t.Fatalf("expected exactly the in-scope recorded event, got %#v", events)
	}
	if events[0].OccurredAt.IsZero() {
		t.Fatal("expected Record to fill OccurredAt when unset")
	}
}

package jobs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestJobsAreIsolatedByStoredScope(t *testing.T) {
	service, err := NewService(NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	old := tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_old"}
	moved := tenancy.Agent{ID: old.ID, OrganizationID: "org_default", SiteID: "site_new"}
	request := CreateRequest{Action: ActionHostReboot, ApprovedBy: "ops", Reason: "maintenance"}

	oldJob, err := service.Create(t.Context(), old.Scope(), old.ID, request)
	if err != nil {
		t.Fatalf("create old-site job: %v", err)
	}
	if jobs, err := service.List(t.Context(), moved.Scope(), moved.ID, 10); err != nil || len(jobs) != 0 {
		t.Fatalf("new site listed old job: %#v %v", jobs, err)
	}
	if events, err := service.Events(t.Context(), moved.Scope(), moved.ID, oldJob.ID); !errors.Is(err, ErrJobNotFound) || events != nil {
		t.Fatalf("new site read old job events: %#v %v", events, err)
	}
	if job, err := service.ClaimNext(t.Context(), moved); err != nil || job != nil {
		t.Fatalf("new site claimed old job: %#v %v", job, err)
	}

	claimed, err := service.ClaimNext(t.Context(), old)
	if err != nil || claimed == nil || claimed.ID != oldJob.ID {
		t.Fatalf("old site claim: %#v %v", claimed, err)
	}
	if _, err := service.Report(t.Context(), old, oldJob.ID, EventRequest{Sequence: 2, Type: EventSucceeded, Message: "rebooted"}); err != nil {
		t.Fatalf("old site report: %v", err)
	}

	newJob, err := service.Create(t.Context(), moved.Scope(), moved.ID, request)
	if err != nil {
		t.Fatalf("create new-site job: %v", err)
	}
	if jobs, err := service.List(t.Context(), moved.Scope(), moved.ID, 10); err != nil || len(jobs) != 1 || jobs[0].ID != newJob.ID {
		t.Fatalf("new-site jobs: %#v %v", jobs, err)
	}
}

func TestJobSignatureCoversScope(t *testing.T) {
	service, err := NewService(NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.Create(t.Context(), tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}, "agent-01", CreateRequest{Action: ActionHostReboot, ApprovedBy: "ops", Reason: "maintenance"})
	if err != nil || !Verify(job) {
		t.Fatalf("create signed scoped job: %#v %v", job, err)
	}
	job.SiteID = "site-b"
	if Verify(job) {
		t.Fatal("scope tampering verified")
	}
}

func TestServiceUsesConfiguredSigningKey(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewMemoryStore(), WithSigningKey(privateKey))
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.Create(context.Background(), tenancy.DefaultScope(), "agent-01", CreateRequest{Action: ActionHostReboot, ApprovedBy: "ops", Reason: "patching"})
	if err != nil {
		t.Fatal(err)
	}
	if job.SigningPublicKey != base64.StdEncoding.EncodeToString(publicKey) || !Verify(job) {
		t.Fatalf("job did not use configured key: %#v", job)
	}
}

func TestServiceRejectsUnsafeServiceTarget(t *testing.T) {
	service, err := NewService(NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"--no-block", "nginx.service;reboot", "name with spaces"} {
		if _, err := service.Create(context.Background(), tenancy.DefaultScope(), "agent-01", CreateRequest{Action: ActionServiceRestart, Target: target, ApprovedBy: "ops", Reason: "test"}); !errors.Is(err, ErrInvalidJob) {
			t.Fatalf("expected unsafe target %q to be rejected, got %v", target, err)
		}
	}
}

func TestApprovedJobIsSignedClaimedAndAudited(t *testing.T) {
	t.Parallel()
	clock := time.Date(2026, 9, 11, 6, 0, 0, 123456789, time.UTC)
	service, err := NewService(NewMemoryStore(), WithClock(func() time.Time { return clock }))
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	job, err := service.Create(context.Background(), tenancy.DefaultScope(), "agent-01", CreateRequest{
		Action: ActionServiceRestart, Target: "nginx.service", ApprovedBy: "gokay", Reason: "Yeni yapılandırmayı etkinleştir",
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if job.Status != StatusQueued || job.Signature == "" || job.SigningPublicKey == "" || !Verify(job) {
		t.Fatalf("expected signed queued job, got %#v", job)
	}
	persisted := job
	persisted.RequestedAt = persisted.RequestedAt.Truncate(time.Microsecond)
	if !Verify(persisted) {
		t.Fatal("expected signature to survive PostgreSQL timestamp precision")
	}

	clock = clock.Add(time.Minute)
	claimed, err := service.ClaimNext(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})
	if err != nil {
		t.Fatalf("claim job: %v", err)
	}
	if claimed == nil || claimed.ID != job.ID || claimed.Status != StatusRunning || claimed.LastSequence != 1 {
		t.Fatalf("expected running claimed job, got %#v", claimed)
	}

	clock = clock.Add(time.Minute)
	if _, err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}, job.ID, EventRequest{Sequence: 2, Type: EventOutput, Message: "restarting nginx"}); err != nil {
		t.Fatalf("report output: %v", err)
	}
	completed, err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}, job.ID, EventRequest{Sequence: 3, Type: EventSucceeded, Message: "nginx restarted"})
	if err != nil {
		t.Fatalf("complete job: %v", err)
	}
	if completed.Status != StatusSucceeded {
		t.Fatalf("expected succeeded job, got %#v", completed)
	}

	events, err := service.Events(context.Background(), tenancy.DefaultScope(), "agent-01", job.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	want := []EventType{EventApproved, EventClaimed, EventOutput, EventSucceeded}
	if len(events) != len(want) {
		t.Fatalf("expected complete audit trail, got %#v", events)
	}
	for index := range want {
		if events[index].Type != want[index] || events[index].Sequence != index {
			t.Fatalf("unexpected event %d: %#v", index, events[index])
		}
	}
}

func TestJobsAreAllowlistedAndSequenced(t *testing.T) {
	t.Parallel()
	service, err := NewService(NewMemoryStore())
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	invalid := []CreateRequest{
		{Action: "shell.exec", Target: "rm", ApprovedBy: "ops", Reason: "test"},
		{Action: ActionServiceRestart, ApprovedBy: "ops", Reason: "test"},
		{Action: ActionHostReboot, Target: "unexpected", ApprovedBy: "ops", Reason: "test"},
		{Action: ActionHostReboot, Reason: "missing approver"},
	}
	for _, request := range invalid {
		if _, err := service.Create(context.Background(), tenancy.DefaultScope(), "agent-01", request); !errors.Is(err, ErrInvalidJob) {
			t.Fatalf("expected invalid request for %#v, got %v", request, err)
		}
	}

	job, err := service.Create(context.Background(), tenancy.DefaultScope(), "agent-01", CreateRequest{Action: ActionHostReboot, ApprovedBy: "ops", Reason: "kernel update"})
	if err != nil {
		t.Fatalf("create reboot: %v", err)
	}
	if _, err := service.ClaimNext(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}); err != nil {
		t.Fatalf("claim reboot: %v", err)
	}
	if _, err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}, job.ID, EventRequest{Sequence: 3, Type: EventOutput, Message: "out of order"}); !errors.Is(err, ErrJobConflict) {
		t.Fatalf("expected sequence conflict, got %v", err)
	}
}

func TestClaimReturnsNilWhenQueueIsEmpty(t *testing.T) {
	t.Parallel()
	service, err := NewService(NewMemoryStore())
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	job, err := service.ClaimNext(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})
	if err != nil || job != nil {
		t.Fatalf("expected empty queue, got %#v, %v", job, err)
	}
}

func TestClaimResumesRunningJobWithoutDuplicateClaimEvent(t *testing.T) {
	clock := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	service, err := NewService(NewMemoryStore(), WithClock(func() time.Time { return clock }), WithJobLease(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Create(context.Background(), tenancy.DefaultScope(), "agent-01", CreateRequest{Action: ActionServiceRestart, Target: "nginx.service", ApprovedBy: "ops", Reason: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.ClaimNext(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})
	if err != nil {
		t.Fatal(err)
	}
	if active, err := service.ClaimNext(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}); err != nil || active != nil {
		t.Fatalf("active lease should not be resumed: %#v %v", active, err)
	}
	clock = clock.Add(2 * time.Minute)
	resumed, err := service.ClaimNext(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})
	if err != nil || resumed == nil || resumed.ID != first.ID || resumed.LastSequence != 1 || !resumed.Resumed {
		t.Fatalf("expected running job to resume: %#v %v", resumed, err)
	}
	events, err := service.Events(context.Background(), tenancy.DefaultScope(), "agent-01", created.ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("claim event was duplicated: %#v %v", events, err)
	}
}

func TestDuplicateJobEventIsIdempotent(t *testing.T) {
	service, err := NewService(NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	created, _ := service.Create(context.Background(), tenancy.DefaultScope(), "agent-01", CreateRequest{Action: ActionServiceRestart, Target: "nginx.service", ApprovedBy: "ops", Reason: "deploy"})
	_, _ = service.ClaimNext(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})
	event := EventRequest{Sequence: 2, Type: EventOutput, Message: "restarted"}
	if _, err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}, created.ID, event); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}, created.ID, event); err != nil {
		t.Fatalf("identical retry should be idempotent: %v", err)
	}
	if _, err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}, created.ID, EventRequest{Sequence: 2, Type: EventOutput, Message: "different"}); !errors.Is(err, ErrJobConflict) {
		t.Fatalf("different duplicate should conflict: %v", err)
	}
	terminal := EventRequest{Sequence: 3, Type: EventSucceeded, Message: "completed"}
	if _, err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}, created.ID, terminal); err != nil {
		t.Fatal(err)
	}
	if job, err := service.Report(context.Background(), tenancy.Agent{ID: "agent-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}, created.ID, terminal); err != nil || job.Status != StatusSucceeded {
		t.Fatalf("terminal retry should be idempotent: %#v %v", job, err)
	}
}

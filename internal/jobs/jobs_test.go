package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestApprovedJobIsSignedClaimedAndAudited(t *testing.T) {
	t.Parallel()
	clock := time.Date(2026, 9, 11, 6, 0, 0, 123456789, time.UTC)
	service, err := NewService(NewMemoryStore(), WithClock(func() time.Time { return clock }))
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	job, err := service.Create(context.Background(), "agent-01", CreateRequest{
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
	claimed, err := service.ClaimNext(context.Background(), "agent-01")
	if err != nil {
		t.Fatalf("claim job: %v", err)
	}
	if claimed == nil || claimed.ID != job.ID || claimed.Status != StatusRunning || claimed.LastSequence != 1 {
		t.Fatalf("expected running claimed job, got %#v", claimed)
	}

	clock = clock.Add(time.Minute)
	if _, err := service.Report(context.Background(), "agent-01", job.ID, EventRequest{Sequence: 2, Type: EventOutput, Message: "restarting nginx"}); err != nil {
		t.Fatalf("report output: %v", err)
	}
	completed, err := service.Report(context.Background(), "agent-01", job.ID, EventRequest{Sequence: 3, Type: EventSucceeded, Message: "nginx restarted"})
	if err != nil {
		t.Fatalf("complete job: %v", err)
	}
	if completed.Status != StatusSucceeded {
		t.Fatalf("expected succeeded job, got %#v", completed)
	}

	events, err := service.Events(context.Background(), "agent-01", job.ID)
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
		if _, err := service.Create(context.Background(), "agent-01", request); !errors.Is(err, ErrInvalidJob) {
			t.Fatalf("expected invalid request for %#v, got %v", request, err)
		}
	}

	job, err := service.Create(context.Background(), "agent-01", CreateRequest{Action: ActionHostReboot, ApprovedBy: "ops", Reason: "kernel update"})
	if err != nil {
		t.Fatalf("create reboot: %v", err)
	}
	if _, err := service.ClaimNext(context.Background(), "agent-01"); err != nil {
		t.Fatalf("claim reboot: %v", err)
	}
	if _, err := service.Report(context.Background(), "agent-01", job.ID, EventRequest{Sequence: 3, Type: EventOutput, Message: "out of order"}); !errors.Is(err, ErrJobConflict) {
		t.Fatalf("expected sequence conflict, got %v", err)
	}
}

func TestClaimReturnsNilWhenQueueIsEmpty(t *testing.T) {
	t.Parallel()
	service, err := NewService(NewMemoryStore())
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	job, err := service.ClaimNext(context.Background(), "agent-01")
	if err != nil || job != nil {
		t.Fatalf("expected empty queue, got %#v, %v", job, err)
	}
}

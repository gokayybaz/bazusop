package audittrail_test

import (
	"context"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/audittrail"
)

func TestMemoryStoreRecordsEventsInOrder(t *testing.T) {
	t.Parallel()
	store := audittrail.NewMemoryStore()
	first := audittrail.Event{CorrelationID: "corr-1", ActorType: audittrail.ActorAgent, ActorID: "agent-1", OrganizationID: "org_default", SiteID: "site_default", Action: "GET", ResourceType: "health", Outcome: audittrail.OutcomeSuccess}
	second := audittrail.Event{CorrelationID: "corr-2", ActorType: audittrail.ActorLegacyToken, ActorID: "bearer", OrganizationID: "org_default", SiteID: "site_default", Action: "POST", ResourceType: "jobs", Outcome: audittrail.OutcomeFailure, ErrorCode: "401"}

	if err := store.Record(context.Background(), first); err != nil {
		t.Fatalf("record first: %v", err)
	}
	if err := store.Record(context.Background(), second); err != nil {
		t.Fatalf("record second: %v", err)
	}

	events := store.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].CorrelationID != "corr-1" || events[1].CorrelationID != "corr-2" {
		t.Fatalf("unexpected order: %#v", events)
	}
	if events[1].Outcome != audittrail.OutcomeFailure || events[1].ErrorCode != "401" {
		t.Fatalf("failure event not recorded correctly: %#v", events[1])
	}
}

func TestServiceFillsEventIDAndOccurredAtWhenUnset(t *testing.T) {
	t.Parallel()
	store := audittrail.NewMemoryStore()
	service := audittrail.NewService(store)

	if err := service.Record(context.Background(), audittrail.Event{CorrelationID: "corr-3", ActorType: audittrail.ActorAnonymous, OrganizationID: "org_default", SiteID: "site_default", Action: "GET", ResourceType: "health", Outcome: audittrail.OutcomeSuccess}); err != nil {
		t.Fatalf("record: %v", err)
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventID == "" {
		t.Fatal("expected Service to fill EventID")
	}
	if events[0].OccurredAt.IsZero() {
		t.Fatal("expected Service to fill OccurredAt")
	}
	if time.Since(events[0].OccurredAt) > time.Minute {
		t.Fatalf("OccurredAt looks stale: %v", events[0].OccurredAt)
	}
}

func TestServiceDoesNotOverwriteCallerSuppliedEventIDOrOccurredAt(t *testing.T) {
	t.Parallel()
	store := audittrail.NewMemoryStore()
	service := audittrail.NewService(store)
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := service.Record(context.Background(), audittrail.Event{EventID: "explicit-id", OccurredAt: fixed, CorrelationID: "corr-4", ActorType: audittrail.ActorAnonymous, OrganizationID: "org_default", SiteID: "site_default", Action: "GET", ResourceType: "health", Outcome: audittrail.OutcomeSuccess}); err != nil {
		t.Fatalf("record: %v", err)
	}

	events := store.Events()
	if events[0].EventID != "explicit-id" {
		t.Fatalf("expected explicit EventID to be preserved, got %q", events[0].EventID)
	}
	if !events[0].OccurredAt.Equal(fixed) {
		t.Fatalf("expected explicit OccurredAt to be preserved, got %v", events[0].OccurredAt)
	}
}

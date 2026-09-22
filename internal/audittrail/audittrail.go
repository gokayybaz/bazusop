// Package audittrail records one append-only event per API request: who
// (best-effort actor), what (resource/action), and the outcome. It is the
// substrate spike 11.3+ (identity, sessions, RBAC) builds on. Writes in this
// package happen after a request's handler has already returned, in a
// separate statement — NOT inside the handler's own database transaction.
// The spec's same-transaction / rollback-on-audit-failure guarantee applies
// to the security-critical mutations later spikes introduce (user creation,
// role changes, session revocation); those call Service.Record directly
// inside their own transaction instead of going through the HTTP-layer
// wrapper in internal/server this package's caller uses today.
package audittrail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/gokayybaz/bazusop/internal/tenancy"
)

var ErrInvalidQuery = errors.New("invalid audit trail query")

type ActorType string

const (
	ActorAgent          ActorType = "agent"
	ActorHuman          ActorType = "human"
	ActorServiceAccount ActorType = "service_account"
	ActorLegacyToken    ActorType = "legacy_token"
	ActorAnonymous      ActorType = "anonymous"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

// Event is intentionally flat (no nested structs) so every field maps to
// exactly one audit_events column with no ambiguity about how to store it.
// JSON tags exist for spike 11.8's GET /api/v1/audit/events search endpoint;
// this type was write-only (never serialized) before that.
type Event struct {
	EventID          string    `json:"event_id"`
	OccurredAt       time.Time `json:"occurred_at"`
	CorrelationID    string    `json:"correlation_id"`
	ActorType        ActorType `json:"actor_type"`
	ActorID          string    `json:"actor_id"`
	SessionOrTokenID string    `json:"session_or_token_id"`
	OrganizationID   string    `json:"organization_id"`
	SiteID           string    `json:"site_id"`
	Action           string    `json:"action"`
	Permission       string    `json:"permission"`
	ResourceType     string    `json:"resource_type"`
	ResourceID       string    `json:"resource_id"`
	Outcome          Outcome   `json:"outcome"`
	ErrorCode        string    `json:"error_code"`
	SourceIP         string    `json:"source_ip"`
	UserAgent        string    `json:"user_agent"`
	ChangeSummary    string    `json:"change_summary"`
}

type Filter struct {
	ActorType    ActorType
	ResourceType string
	Outcome      Outcome
	Since        time.Time
	Until        time.Time
}

type Store interface {
	Record(ctx context.Context, event Event) error
	ListAuditTrail(ctx context.Context, scope tenancy.Scope, filter Filter, limit int) ([]Event, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (service *Service) Record(ctx context.Context, event Event) error {
	if event.EventID == "" {
		id, err := newEventID()
		if err != nil {
			return err
		}
		event.EventID = id
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	return service.store.Record(ctx, event)
}

func (service *Service) List(ctx context.Context, scope tenancy.Scope, filter Filter, limit int) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		return nil, ErrInvalidQuery
	}
	return service.store.ListAuditTrail(ctx, scope, filter, limit)
}

func newEventID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

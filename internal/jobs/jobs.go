package jobs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidJob  = errors.New("invalid job")
	ErrJobConflict = errors.New("job conflict")
	ErrJobNotFound = errors.New("job not found")
)

var serviceTargetPattern = regexp.MustCompile(`^[A-Za-z0-9_.@:-]+$`)

type Action string

const (
	ActionServiceRestart Action = "service.restart"
	ActionHostReboot     Action = "host.reboot"
)

const defaultJobLease = 2 * time.Minute

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

type EventType string

const (
	EventApproved  EventType = "approved"
	EventClaimed   EventType = "claimed"
	EventOutput    EventType = "output"
	EventSucceeded EventType = "succeeded"
	EventFailed    EventType = "failed"
)

type CreateRequest struct {
	Action     Action `json:"action"`
	Target     string `json:"target"`
	ApprovedBy string `json:"approved_by"`
	Reason     string `json:"reason"`
}

type Job struct {
	ID               string    `json:"id"`
	AgentID          string    `json:"agent_id"`
	Action           Action    `json:"action"`
	Target           string    `json:"target"`
	ApprovedBy       string    `json:"approved_by"`
	Reason           string    `json:"reason"`
	RequestedAt      time.Time `json:"requested_at"`
	Status           Status    `json:"status"`
	LastSequence     int       `json:"last_sequence"`
	Signature        string    `json:"signature"`
	SigningPublicKey string    `json:"signing_public_key"`
	Resumed          bool      `json:"resumed,omitempty"`
}

type EventRequest struct {
	Sequence int       `json:"sequence"`
	Type     EventType `json:"type"`
	Message  string    `json:"message"`
}

type Event struct {
	JobID      string    `json:"job_id"`
	Sequence   int       `json:"sequence"`
	Type       EventType `json:"type"`
	Message    string    `json:"message"`
	Actor      string    `json:"actor"`
	OccurredAt time.Time `json:"occurred_at"`
}

type Store interface {
	CreateJob(context.Context, Job, Event) error
	ClaimNextJob(context.Context, string, time.Time, time.Time) (*Job, error)
	RecordJobEvent(context.Context, string, string, EventRequest, time.Time) (Job, Event, error)
	ListJobs(context.Context, string, int) ([]Job, error)
	ListJobEvents(context.Context, string, string) ([]Event, error)
}

type Service struct {
	store      Store
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	now        func() time.Time
	jobLease   time.Duration
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option { return func(service *Service) { service.now = clock } }
func WithJobLease(duration time.Duration) Option {
	return func(service *Service) { service.jobLease = duration }
}
func WithSigningKey(privateKey ed25519.PrivateKey) Option {
	return func(service *Service) {
		service.privateKey = append(ed25519.PrivateKey(nil), privateKey...)
		if len(privateKey) == ed25519.PrivateKeySize {
			service.publicKey = append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...)
		}
	}
}

func NewService(store Store, options ...Option) (*Service, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	service := &Service{store: store, privateKey: privateKey, publicKey: publicKey, now: time.Now, jobLease: defaultJobLease}
	for _, option := range options {
		option(service)
	}
	if len(service.privateKey) != ed25519.PrivateKeySize || len(service.publicKey) != ed25519.PublicKeySize || service.jobLease <= 0 {
		return nil, errors.New("job signing key is invalid")
	}
	return service, nil
}

func (service *Service) Create(ctx context.Context, agentID string, request CreateRequest) (Job, error) {
	agentID = strings.TrimSpace(agentID)
	request.Target = strings.TrimSpace(request.Target)
	request.ApprovedBy = strings.TrimSpace(request.ApprovedBy)
	request.Reason = strings.TrimSpace(request.Reason)
	if agentID == "" || request.ApprovedBy == "" || request.Reason == "" || len(request.ApprovedBy) > 128 || len(request.Reason) > 1024 || len(request.Target) > 256 || !validActionTarget(request.Action, request.Target) {
		return Job{}, ErrInvalidJob
	}
	id, err := newID()
	if err != nil {
		return Job{}, err
	}
	job := Job{
		ID: id, AgentID: agentID, Action: request.Action, Target: request.Target,
		ApprovedBy: request.ApprovedBy, Reason: request.Reason, RequestedAt: service.now().UTC().Truncate(time.Microsecond),
		Status: StatusQueued, LastSequence: 0, SigningPublicKey: base64.StdEncoding.EncodeToString(service.publicKey),
	}
	payload, err := signingPayload(job)
	if err != nil {
		return Job{}, err
	}
	job.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(service.privateKey, payload))
	event := Event{JobID: job.ID, Sequence: 0, Type: EventApproved, Message: job.Reason, Actor: job.ApprovedBy, OccurredAt: job.RequestedAt}
	if err := service.store.CreateJob(ctx, job, event); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (service *Service) ClaimNext(ctx context.Context, agentID string) (*Job, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, ErrInvalidJob
	}
	now := service.now().UTC()
	return service.store.ClaimNextJob(ctx, agentID, now, now.Add(-service.jobLease))
}

func (service *Service) Report(ctx context.Context, agentID, jobID string, request EventRequest) (Job, error) {
	agentID, jobID = strings.TrimSpace(agentID), strings.TrimSpace(jobID)
	request.Message = strings.TrimSpace(request.Message)
	if agentID == "" || jobID == "" || request.Sequence < 2 || request.Message == "" || len(request.Message) > 32*1024 || (request.Type != EventOutput && request.Type != EventSucceeded && request.Type != EventFailed) {
		return Job{}, ErrInvalidJob
	}
	job, _, err := service.store.RecordJobEvent(ctx, agentID, jobID, request, service.now().UTC())
	return job, err
}

func (service *Service) List(ctx context.Context, agentID string, limit int) ([]Job, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || limit < 1 || limit > 200 {
		return nil, ErrInvalidJob
	}
	return service.store.ListJobs(ctx, agentID, limit)
}

func (service *Service) Events(ctx context.Context, agentID, jobID string) ([]Event, error) {
	agentID, jobID = strings.TrimSpace(agentID), strings.TrimSpace(jobID)
	if agentID == "" || jobID == "" {
		return nil, ErrInvalidJob
	}
	return service.store.ListJobEvents(ctx, agentID, jobID)
}

func Verify(job Job) bool {
	publicKey, err := base64.StdEncoding.DecodeString(job.SigningPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	signature, err := base64.StdEncoding.DecodeString(job.Signature)
	if err != nil {
		return false
	}
	payload, err := signingPayload(job)
	return err == nil && ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature)
}

func signingPayload(job Job) ([]byte, error) {
	return json.Marshal(struct {
		Version     int    `json:"version"`
		ID          string `json:"id"`
		AgentID     string `json:"agent_id"`
		Action      Action `json:"action"`
		Target      string `json:"target"`
		ApprovedBy  string `json:"approved_by"`
		Reason      string `json:"reason"`
		RequestedAt string `json:"requested_at"`
	}{1, job.ID, job.AgentID, job.Action, job.Target, job.ApprovedBy, job.Reason, job.RequestedAt.UTC().Format(time.RFC3339Nano)})
}

func validActionTarget(action Action, target string) bool {
	return (action == ActionServiceRestart && serviceTargetPattern.MatchString(target) && !strings.HasPrefix(target, "-")) || (action == ActionHostReboot && target == "")
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

type MemoryStore struct {
	mu     sync.Mutex
	jobs   map[string]Job
	events map[string][]Event
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{jobs: make(map[string]Job), events: make(map[string][]Event)}
}

func (store *MemoryStore) CreateJob(_ context.Context, job Job, event Event) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.jobs[job.ID] = job
	store.events[job.ID] = []Event{event}
	return nil
}

func (store *MemoryStore) ClaimNextJob(_ context.Context, agentID string, occurredAt, resumeBefore time.Time) (*Job, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var selected *Job
	for _, candidate := range store.jobs {
		if candidate.AgentID != agentID || candidate.Status != StatusRunning {
			continue
		}
		if selected == nil || candidate.RequestedAt.Before(selected.RequestedAt) || (candidate.RequestedAt.Equal(selected.RequestedAt) && candidate.ID < selected.ID) {
			copy := candidate
			selected = &copy
		}
	}
	if selected != nil {
		for _, event := range store.events[selected.ID] {
			if event.Type == EventClaimed && event.OccurredAt.After(resumeBefore) {
				return nil, nil
			}
		}
		selected.Resumed = true
		return selected, nil
	}
	for _, candidate := range store.jobs {
		if candidate.AgentID != agentID || candidate.Status != StatusQueued {
			continue
		}
		if selected == nil || candidate.RequestedAt.Before(selected.RequestedAt) || (candidate.RequestedAt.Equal(selected.RequestedAt) && candidate.ID < selected.ID) {
			copy := candidate
			selected = &copy
		}
	}
	if selected == nil {
		return nil, nil
	}
	selected.Status, selected.LastSequence = StatusRunning, 1
	store.jobs[selected.ID] = *selected
	store.events[selected.ID] = append(store.events[selected.ID], Event{JobID: selected.ID, Sequence: 1, Type: EventClaimed, Message: "agent claimed job", Actor: "agent:" + agentID, OccurredAt: occurredAt})
	return selected, nil
}

func (store *MemoryStore) RecordJobEvent(_ context.Context, agentID, jobID string, request EventRequest, occurredAt time.Time) (Job, Event, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	job, exists := store.jobs[jobID]
	if !exists || job.AgentID != agentID {
		return Job{}, Event{}, ErrJobNotFound
	}
	if request.Sequence <= job.LastSequence {
		for _, event := range store.events[jobID] {
			if event.Sequence == request.Sequence && event.Type == request.Type && event.Message == request.Message {
				return job, event, nil
			}
		}
		return Job{}, Event{}, ErrJobConflict
	}
	if job.Status != StatusRunning || request.Sequence != job.LastSequence+1 {
		return Job{}, Event{}, ErrJobConflict
	}
	if request.Type == EventSucceeded {
		job.Status = StatusSucceeded
	}
	if request.Type == EventFailed {
		job.Status = StatusFailed
	}
	job.LastSequence = request.Sequence
	event := Event{JobID: jobID, Sequence: request.Sequence, Type: request.Type, Message: request.Message, Actor: "agent:" + agentID, OccurredAt: occurredAt}
	store.jobs[jobID] = job
	store.events[jobID] = append(store.events[jobID], event)
	return job, event, nil
}

func (store *MemoryStore) ListJobs(_ context.Context, agentID string, limit int) ([]Job, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]Job, 0)
	for _, job := range store.jobs {
		if job.AgentID == agentID {
			result = append(result, job)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].RequestedAt.After(result[right].RequestedAt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return append(make([]Job, 0, len(result)), result...), nil
}

func (store *MemoryStore) ListJobEvents(_ context.Context, agentID, jobID string) ([]Event, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	job, exists := store.jobs[jobID]
	if !exists || job.AgentID != agentID {
		return nil, ErrJobNotFound
	}
	return append(make([]Event, 0, len(store.events[jobID])), store.events[jobID]...), nil
}

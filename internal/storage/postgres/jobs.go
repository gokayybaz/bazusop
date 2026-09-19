package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func (store *Store) CreateJob(ctx context.Context, job jobs.Job, event jobs.Event) error {
	scope := tenancy.Scope{OrganizationID: job.OrganizationID, SiteID: job.SiteID}
	if err := scope.Validate(); err != nil || event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
		return tenancy.ErrInvalidScope
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin job creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var hostExists int
	if err := transaction.QueryRow(ctx, "SELECT 1 FROM hosts WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3 FOR UPDATE", scope.OrganizationID, scope.SiteID, job.AgentID).Scan(&hostExists); errors.Is(err, pgx.ErrNoRows) {
		return jobs.ErrJobNotFound
	} else if err != nil {
		return fmt.Errorf("lock job target host: %w", err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO jobs (id, organization_id, site_id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, job.ID, job.OrganizationID, job.SiteID, job.AgentID, job.Action, job.Target, job.ApprovedBy, job.Reason, job.RequestedAt, job.Status, job.LastSequence, job.Signature, job.SigningPublicKey); err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	if err := insertJobEvent(ctx, transaction, event); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit job creation: %w", err)
	}
	return nil
}

func (store *Store) ClaimNextJob(ctx context.Context, agent tenancy.Agent, occurredAt, resumeBefore time.Time) (*jobs.Job, error) {
	if err := agent.Validate(); err != nil {
		return nil, err
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin job claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(json_build_array($1::text, $2::text, $3::text)::text, 0))", agent.OrganizationID, agent.SiteID, agent.ID); err != nil {
		return nil, fmt.Errorf("lock agent job queue: %w", err)
	}
	running, err := scanJob(transaction.QueryRow(ctx, `SELECT id, organization_id, site_id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key FROM jobs WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3 AND status='running' ORDER BY requested_at, id FOR UPDATE LIMIT 1`, agent.OrganizationID, agent.SiteID, agent.ID))
	if err == nil {
		var claimedAt time.Time
		if err := transaction.QueryRow(ctx, "SELECT occurred_at FROM job_events WHERE organization_id=$1 AND site_id=$2 AND job_id=$3 AND type='claimed' ORDER BY sequence DESC LIMIT 1", agent.OrganizationID, agent.SiteID, running.ID).Scan(&claimedAt); err != nil {
			return nil, fmt.Errorf("read running job lease: %w", err)
		}
		if claimedAt.After(resumeBefore) {
			if err := transaction.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit active job lease check: %w", err)
			}
			return nil, nil
		}
		running.Resumed = true
		if err := transaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit running job resume: %w", err)
		}
		return &running, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("select running job: %w", err)
	}
	job, err := scanJob(transaction.QueryRow(ctx, `SELECT id, organization_id, site_id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key FROM jobs WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3 AND status='queued' ORDER BY requested_at, id FOR UPDATE SKIP LOCKED LIMIT 1`, agent.OrganizationID, agent.SiteID, agent.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select queued job: %w", err)
	}
	job.Status, job.LastSequence = jobs.StatusRunning, 1
	if _, err := transaction.Exec(ctx, "UPDATE jobs SET status=$4, last_sequence=$5 WHERE id=$3 AND organization_id=$1 AND site_id=$2", agent.OrganizationID, agent.SiteID, job.ID, job.Status, job.LastSequence); err != nil {
		return nil, fmt.Errorf("claim queued job: %w", err)
	}
	event := jobs.Event{JobID: job.ID, OrganizationID: agent.OrganizationID, SiteID: agent.SiteID, Sequence: 1, Type: jobs.EventClaimed, Message: "agent claimed job", Actor: "agent:" + agent.ID, OccurredAt: occurredAt}
	if err := insertJobEvent(ctx, transaction, event); err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit job claim: %w", err)
	}
	return &job, nil
}

func (store *Store) RecordJobEvent(ctx context.Context, agent tenancy.Agent, jobID string, request jobs.EventRequest, occurredAt time.Time) (jobs.Job, jobs.Event, error) {
	if err := agent.Validate(); err != nil {
		return jobs.Job{}, jobs.Event{}, err
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("begin job event: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	job, err := scanJob(transaction.QueryRow(ctx, `SELECT id, organization_id, site_id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key FROM jobs WHERE id=$1 AND organization_id=$2 AND site_id=$3 AND agent_id=$4 FOR UPDATE`, jobID, agent.OrganizationID, agent.SiteID, agent.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return jobs.Job{}, jobs.Event{}, jobs.ErrJobNotFound
	}
	if err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("lock job event: %w", err)
	}
	if request.Sequence <= job.LastSequence {
		var event jobs.Event
		err := transaction.QueryRow(ctx, `SELECT job_id,organization_id,site_id,sequence,type,message,actor,occurred_at FROM job_events WHERE job_id=$1 AND organization_id=$2 AND site_id=$3 AND sequence=$4`, jobID, agent.OrganizationID, agent.SiteID, request.Sequence).Scan(&event.JobID, &event.OrganizationID, &event.SiteID, &event.Sequence, &event.Type, &event.Message, &event.Actor, &event.OccurredAt)
		if err == nil && event.Type == request.Type && event.Message == request.Message {
			if err := transaction.Commit(ctx); err != nil {
				return jobs.Job{}, jobs.Event{}, fmt.Errorf("commit duplicate job event: %w", err)
			}
			return job, event, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return jobs.Job{}, jobs.Event{}, fmt.Errorf("read duplicate job event: %w", err)
		}
		return jobs.Job{}, jobs.Event{}, jobs.ErrJobConflict
	}
	if job.Status != jobs.StatusRunning || request.Sequence != job.LastSequence+1 {
		return jobs.Job{}, jobs.Event{}, jobs.ErrJobConflict
	}
	if request.Type == jobs.EventSucceeded {
		job.Status = jobs.StatusSucceeded
	}
	if request.Type == jobs.EventFailed {
		job.Status = jobs.StatusFailed
	}
	job.LastSequence = request.Sequence
	if _, err := transaction.Exec(ctx, "UPDATE jobs SET status=$4, last_sequence=$5 WHERE id=$3 AND organization_id=$1 AND site_id=$2", agent.OrganizationID, agent.SiteID, job.ID, job.Status, job.LastSequence); err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("update job event state: %w", err)
	}
	event := jobs.Event{JobID: job.ID, OrganizationID: agent.OrganizationID, SiteID: agent.SiteID, Sequence: request.Sequence, Type: request.Type, Message: request.Message, Actor: "agent:" + agent.ID, OccurredAt: occurredAt}
	if err := insertJobEvent(ctx, transaction, event); err != nil {
		return jobs.Job{}, jobs.Event{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return jobs.Job{}, jobs.Event{}, fmt.Errorf("commit job event: %w", err)
	}
	return job, event, nil
}

func (store *Store) ListJobs(ctx context.Context, scope tenancy.Scope, agentID string, limit int) ([]jobs.Job, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `SELECT id, organization_id, site_id, agent_id, action, target, approved_by, reason, requested_at, status, last_sequence, signature, signing_public_key FROM jobs WHERE organization_id=$1 AND site_id=$2 AND agent_id=$3 ORDER BY requested_at DESC, id DESC LIMIT $4`, scope.OrganizationID, scope.SiteID, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("query jobs: %w", err)
	}
	defer rows.Close()
	values := make([]jobs.Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan jobs: %w", err)
		}
		values = append(values, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return values, nil
}

func (store *Store) ListJobEvents(ctx context.Context, scope tenancy.Scope, agentID, jobID string) ([]jobs.Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `SELECT event.job_id, event.organization_id, event.site_id, event.sequence, event.type, event.message, event.actor, event.occurred_at FROM job_events event JOIN jobs job ON job.id=event.job_id WHERE event.job_id=$1 AND event.organization_id=$2 AND event.site_id=$3 AND job.organization_id=$2 AND job.site_id=$3 AND job.agent_id=$4 ORDER BY event.sequence`, jobID, scope.OrganizationID, scope.SiteID, agentID)
	if err != nil {
		return nil, fmt.Errorf("query job events: %w", err)
	}
	defer rows.Close()
	values := make([]jobs.Event, 0)
	for rows.Next() {
		var event jobs.Event
		if err := rows.Scan(&event.JobID, &event.OrganizationID, &event.SiteID, &event.Sequence, &event.Type, &event.Message, &event.Actor, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan job events: %w", err)
		}
		values = append(values, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job events: %w", err)
	}
	if len(values) == 0 {
		return nil, jobs.ErrJobNotFound
	}
	return values, nil
}

type rowScanner interface{ Scan(...any) error }

func scanJob(row rowScanner) (jobs.Job, error) {
	var job jobs.Job
	err := row.Scan(&job.ID, &job.OrganizationID, &job.SiteID, &job.AgentID, &job.Action, &job.Target, &job.ApprovedBy, &job.Reason, &job.RequestedAt, &job.Status, &job.LastSequence, &job.Signature, &job.SigningPublicKey)
	return job, err
}

func insertJobEvent(ctx context.Context, transaction pgx.Tx, event jobs.Event) error {
	if _, err := transaction.Exec(ctx, `INSERT INTO job_events (job_id, organization_id, site_id, sequence, type, message, actor, occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, event.JobID, event.OrganizationID, event.SiteID, event.Sequence, event.Type, event.Message, event.Actor, event.OccurredAt); err != nil {
		return fmt.Errorf("insert job event: %w", err)
	}
	return nil
}

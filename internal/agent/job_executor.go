package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/gokayybaz/bazusop/internal/jobs"
)

var serviceTargetPattern = regexp.MustCompile(`^[A-Za-z0-9_.@:-]+$`)
var jobIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type JobClient interface {
	ClaimNextJob(context.Context, Identity) (*jobs.Job, error)
	ReportJobEvent(context.Context, Identity, string, jobs.EventRequest) error
}

type JobExecutor struct {
	hub     JobClient
	execute func(context.Context, jobs.Job) (string, error)
	mu      sync.Mutex
	pending *pendingJobEvents
}

type pendingJobEvents struct {
	identity Identity
	jobID    string
	events   []jobs.EventRequest
}

func NewJobExecutor(hub JobClient) *JobExecutor {
	return &JobExecutor{hub: hub, execute: executePlatformJob}
}

func (executor *JobExecutor) ProcessNext(ctx context.Context, identity Identity) error {
	if executor == nil || executor.hub == nil || executor.execute == nil {
		return errors.New("job executor is incomplete")
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if executor.pending != nil {
		if err := executor.flushPending(ctx); err != nil {
			return err
		}
	}
	job, err := executor.hub.ClaimNextJob(ctx, identity)
	if err != nil || job == nil {
		return err
	}
	if !jobIDPattern.MatchString(job.ID) {
		return errors.New("claimed job ID is invalid")
	}
	if err := validateClaimedJob(*job, identity); err != nil {
		return executor.reportEvents(ctx, identity, job.ID, []jobs.EventRequest{{Sequence: 2, Type: jobs.EventFailed, Message: truncateUTF8(err.Error(), 32*1024)}})
	}
	output, executeErr := executor.execute(ctx, *job)
	output = strings.TrimSpace(output)
	if output == "" {
		output = fmt.Sprintf("%s produced no output", job.Action)
	}
	if executeErr != nil {
		return executor.reportEvents(ctx, identity, job.ID, []jobs.EventRequest{
			{Sequence: 2, Type: jobs.EventOutput, Message: truncateUTF8(output, 32*1024)},
			{Sequence: 3, Type: jobs.EventFailed, Message: truncateUTF8(executeErr.Error(), 32*1024)},
		})
	}
	return executor.reportEvents(ctx, identity, job.ID, []jobs.EventRequest{
		{Sequence: 2, Type: jobs.EventOutput, Message: truncateUTF8(output, 32*1024)},
		{Sequence: 3, Type: jobs.EventSucceeded, Message: fmt.Sprintf("%s completed", job.Action)},
	})
}

func (executor *JobExecutor) reportEvents(ctx context.Context, identity Identity, jobID string, events []jobs.EventRequest) error {
	executor.pending = &pendingJobEvents{identity: identity, jobID: jobID, events: events}
	return executor.flushPending(ctx)
}

func (executor *JobExecutor) flushPending(ctx context.Context) error {
	for len(executor.pending.events) > 0 {
		event := executor.pending.events[0]
		if err := executor.hub.ReportJobEvent(ctx, executor.pending.identity, executor.pending.jobID, event); err != nil {
			return err
		}
		executor.pending.events = executor.pending.events[1:]
	}
	executor.pending = nil
	return nil
}

func validateClaimedJob(job jobs.Job, identity Identity) error {
	if job.ID == "" || job.AgentID != identity.AgentID || job.Status != jobs.StatusRunning || job.LastSequence != 1 {
		return errors.New("claimed job identity or state is invalid")
	}
	if !jobs.Verify(job) {
		return errors.New("job signature verification failed")
	}
	trustedKey, err := enrollmentCAPublicKey(identity.CACertificatePEM)
	if err != nil {
		return err
	}
	providedKey, err := base64.StdEncoding.DecodeString(job.SigningPublicKey)
	if err != nil || !bytes.Equal(providedKey, trustedKey) {
		return errors.New("job signer does not match pinned enrollment authority")
	}
	switch job.Action {
	case jobs.ActionServiceRestart:
		if !serviceTargetPattern.MatchString(job.Target) || strings.HasPrefix(job.Target, "-") || strings.EqualFold(job.Target, "bazusop-agent") || strings.EqualFold(job.Target, "bazusop-agent.service") {
			return errors.New("service restart target is invalid")
		}
	case jobs.ActionHostReboot:
		if job.Target != "" {
			return errors.New("host reboot target must be empty")
		}
	default:
		return errors.New("job action is not allowed")
	}
	return nil
}

func enrollmentCAPublicKey(certificatePEM []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("pinned enrollment authority certificate is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.New("pinned enrollment authority certificate is invalid")
	}
	publicKey, ok := certificate.PublicKey.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("pinned enrollment authority key is not Ed25519")
	}
	return publicKey, nil
}

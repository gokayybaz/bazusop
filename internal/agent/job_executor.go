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
	"os"
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
	state   JobStateStore
}

func NewJobExecutor(hub JobClient, state JobStateStore) *JobExecutor {
	return &JobExecutor{hub: hub, execute: executePlatformJob, state: state}
}

func (executor *JobExecutor) ProcessNext(ctx context.Context, identity Identity) error {
	if executor == nil || executor.hub == nil || executor.execute == nil || executor.state == nil {
		return errors.New("job executor is incomplete")
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if recovered, err := executor.recoverState(ctx, identity); err != nil || recovered {
		return err
	}
	job, err := executor.hub.ClaimNextJob(ctx, identity)
	if err != nil || job == nil {
		return err
	}
	if !jobIDPattern.MatchString(job.ID) {
		return errors.New("claimed job ID is invalid")
	}
	if err := validateClaimedJob(*job, identity); err != nil {
		return executor.reportEvents(ctx, identity, *job, []jobs.EventRequest{{Sequence: job.LastSequence + 1, Type: jobs.EventFailed, Message: truncateUTF8(err.Error(), 32*1024)}})
	}
	if job.Resumed {
		return executor.reportEvents(ctx, identity, *job, []jobs.EventRequest{{Sequence: job.LastSequence + 1, Type: jobs.EventFailed, Message: "running job has no durable execution state; outcome unknown"}})
	}
	if err := executor.state.Save(jobExecutionState{Job: *job, Phase: jobPhaseExecuting}); err != nil {
		return fmt.Errorf("persist job before execution: %w", err)
	}
	output, executeErr := executor.execute(ctx, *job)
	output = strings.TrimSpace(output)
	if output == "" {
		output = fmt.Sprintf("%s produced no output", job.Action)
	}
	if executeErr != nil {
		return executor.reportEvents(ctx, identity, *job, []jobs.EventRequest{
			{Sequence: 2, Type: jobs.EventOutput, Message: truncateUTF8(output, 32*1024)},
			{Sequence: 3, Type: jobs.EventFailed, Message: truncateUTF8(executeErr.Error(), 32*1024)},
		})
	}
	return executor.reportEvents(ctx, identity, *job, []jobs.EventRequest{
		{Sequence: 2, Type: jobs.EventOutput, Message: truncateUTF8(output, 32*1024)},
		{Sequence: 3, Type: jobs.EventSucceeded, Message: fmt.Sprintf("%s completed", job.Action)},
	})
}

func (executor *JobExecutor) recoverState(ctx context.Context, identity Identity) (bool, error) {
	state, err := executor.state.Load()
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if state.Job.AgentID != identity.AgentID || !jobIDPattern.MatchString(state.Job.ID) {
		return true, errors.New("stored job execution state belongs to another identity")
	}
	if state.Phase == jobPhaseExecuting {
		state.Phase = jobPhaseCompleted
		state.Events = []jobs.EventRequest{{Sequence: state.Job.LastSequence + 1, Type: jobs.EventFailed, Message: "agent restarted during execution; outcome unknown"}}
		if err := executor.state.Save(*state); err != nil {
			return true, err
		}
	}
	return true, executor.flushState(ctx, identity, state)
}

func (executor *JobExecutor) reportEvents(ctx context.Context, identity Identity, job jobs.Job, events []jobs.EventRequest) error {
	state := &jobExecutionState{Job: job, Phase: jobPhaseCompleted, Events: events}
	if err := executor.state.Save(*state); err != nil {
		return fmt.Errorf("persist job result: %w", err)
	}
	return executor.flushState(ctx, identity, state)
}

func (executor *JobExecutor) flushState(ctx context.Context, identity Identity, state *jobExecutionState) error {
	for len(state.Events) > 0 {
		event := state.Events[0]
		if err := executor.hub.ReportJobEvent(ctx, identity, state.Job.ID, event); err != nil {
			return err
		}
		state.Events = state.Events[1:]
		if len(state.Events) > 0 {
			if err := executor.state.Save(*state); err != nil {
				return err
			}
		}
	}
	return executor.state.Delete()
}

func validateClaimedJob(job jobs.Job, identity Identity) error {
	if job.ID == "" || job.AgentID != identity.AgentID || job.Status != jobs.StatusRunning || job.LastSequence < 1 {
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

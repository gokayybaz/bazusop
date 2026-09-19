package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"reflect"
	"testing"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestJobExecutorRunsJobSignedByPinnedEnrollmentAuthority(t *testing.T) {
	identity, signer := jobTestIdentity(t, "agent-01")
	job := claimedJob(t, signer, identity.AgentID, jobs.ActionServiceRestart, "nginx.service")
	client := &fakeJobClient{job: job}
	executed := 0
	executor := &JobExecutor{hub: client, state: NewFileJobStateStore(t.TempDir()), execute: func(_ context.Context, candidate jobs.Job) (string, error) {
		executed++
		return candidate.Target + " restarted", nil
	}}
	if err := executor.ProcessNext(context.Background(), identity); err != nil {
		t.Fatalf("process job: %v", err)
	}
	want := []jobs.EventRequest{{Sequence: 2, Type: jobs.EventOutput, Message: "nginx.service restarted"}, {Sequence: 3, Type: jobs.EventSucceeded, Message: "service.restart completed"}}
	if executed != 1 || !reflect.DeepEqual(client.events, want) {
		t.Fatalf("unexpected execution: count=%d events=%#v", executed, client.events)
	}
}

func TestJobExecutorRejectsUnpinnedSignerWithoutExecution(t *testing.T) {
	identity, _ := jobTestIdentity(t, "agent-01")
	_, otherSigner, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeJobClient{job: claimedJob(t, otherSigner, identity.AgentID, jobs.ActionHostReboot, "")}
	executed := false
	executor := &JobExecutor{hub: client, state: NewFileJobStateStore(t.TempDir()), execute: func(context.Context, jobs.Job) (string, error) { executed = true; return "", nil }}
	if err := executor.ProcessNext(context.Background(), identity); err != nil {
		t.Fatalf("reject job: %v", err)
	}
	if executed || len(client.events) != 1 || client.events[0].Type != jobs.EventFailed || client.events[0].Sequence != 2 {
		t.Fatalf("untrusted job was not safely rejected: executed=%v events=%#v", executed, client.events)
	}
}

func TestJobExecutorReportsPlatformFailure(t *testing.T) {
	identity, signer := jobTestIdentity(t, "agent-01")
	client := &fakeJobClient{job: claimedJob(t, signer, identity.AgentID, jobs.ActionServiceRestart, "nginx.service")}
	executor := &JobExecutor{hub: client, state: NewFileJobStateStore(t.TempDir()), execute: func(context.Context, jobs.Job) (string, error) {
		return "permission denied", errors.New("exit status 1")
	}}
	if err := executor.ProcessNext(context.Background(), identity); err != nil {
		t.Fatalf("process failed job: %v", err)
	}
	if len(client.events) != 2 || client.events[1].Type != jobs.EventFailed || client.events[1].Sequence != 3 {
		t.Fatalf("unexpected failure audit: %#v", client.events)
	}
}

func TestJobExecutorRetriesAuditWithoutExecutingJobTwice(t *testing.T) {
	identity, signer := jobTestIdentity(t, "agent-01")
	client := &fakeJobClient{job: claimedJob(t, signer, identity.AgentID, jobs.ActionServiceRestart, "nginx.service"), failSequenceOnce: 3}
	executed := 0
	executor := &JobExecutor{hub: client, state: NewFileJobStateStore(t.TempDir()), execute: func(context.Context, jobs.Job) (string, error) {
		executed++
		return "restarted", nil
	}}
	if err := executor.ProcessNext(context.Background(), identity); err == nil {
		t.Fatal("expected terminal audit failure")
	}
	if err := executor.ProcessNext(context.Background(), identity); err != nil {
		t.Fatalf("retry terminal audit: %v", err)
	}
	if executed != 1 || len(client.events) != 2 || client.events[1].Type != jobs.EventSucceeded {
		t.Fatalf("job was re-executed or audit was not retried: executed=%d events=%#v", executed, client.events)
	}
}

func TestJobExecutorMarksInterruptedExecutionUnknownWithoutReexecution(t *testing.T) {
	identity, signer := jobTestIdentity(t, "agent-01")
	job := claimedJob(t, signer, identity.AgentID, jobs.ActionServiceRestart, "nginx.service")
	state := NewFileJobStateStore(t.TempDir())
	if err := state.Save(jobExecutionState{Job: *job, Phase: jobPhaseExecuting}); err != nil {
		t.Fatal(err)
	}
	client := &fakeJobClient{}
	executed := false
	executor := &JobExecutor{hub: client, state: state, execute: func(context.Context, jobs.Job) (string, error) {
		executed = true
		return "", nil
	}}
	if err := executor.ProcessNext(context.Background(), identity); err != nil {
		t.Fatalf("recover interrupted job: %v", err)
	}
	if executed || len(client.events) != 1 || client.events[0].Type != jobs.EventFailed || client.events[0].Message != "agent restarted during execution; outcome unknown" {
		t.Fatalf("interrupted job was not safely recovered: executed=%v events=%#v", executed, client.events)
	}
}

func TestJobExecutorDoesNotReexecuteResumedJobWithoutLocalState(t *testing.T) {
	identity, signer := jobTestIdentity(t, "agent-01")
	job := claimedJob(t, signer, identity.AgentID, jobs.ActionServiceRestart, "nginx.service")
	job.Resumed = true
	client := &fakeJobClient{job: job}
	executed := false
	executor := &JobExecutor{hub: client, state: NewFileJobStateStore(t.TempDir()), execute: func(context.Context, jobs.Job) (string, error) {
		executed = true
		return "", nil
	}}
	if err := executor.ProcessNext(context.Background(), identity); err != nil {
		t.Fatalf("recover resumed job: %v", err)
	}
	if executed || len(client.events) != 1 || client.events[0].Type != jobs.EventFailed || client.events[0].Message != "running job has no durable execution state; outcome unknown" {
		t.Fatalf("resumed job was re-executed: executed=%v events=%#v", executed, client.events)
	}
}

type fakeJobClient struct {
	job              *jobs.Job
	events           []jobs.EventRequest
	claimed          bool
	failSequenceOnce int
}

func (client *fakeJobClient) ClaimNextJob(context.Context, Identity) (*jobs.Job, error) {
	if client.claimed {
		return nil, nil
	}
	client.claimed = true
	return client.job, nil
}
func (client *fakeJobClient) ReportJobEvent(_ context.Context, _ Identity, _ string, event jobs.EventRequest) error {
	if client.failSequenceOnce == event.Sequence {
		client.failSequenceOnce = 0
		return errors.New("hub unavailable")
	}
	client.events = append(client.events, event)
	return nil
}

func claimedJob(t *testing.T, signer ed25519.PrivateKey, agentID string, action jobs.Action, target string) *jobs.Job {
	t.Helper()
	registry := inventory.NewService(inventory.NewMemoryStore())
	agent := tenancy.Agent{ID: agentID, OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}
	if err := registry.Report(context.Background(), agent, inventory.Facts{Hostname: "edge-" + agentID, OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}); err != nil {
		t.Fatal(err)
	}
	service, err := jobs.NewService(jobs.NewMemoryStore(), jobs.WithSigningKey(signer), jobs.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(context.Background(), tenancy.DefaultScope(), agentID, jobs.CreateRequest{Action: action, Target: target, ApprovedBy: "ops", Reason: "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.ClaimNext(context.Background(), tenancy.Agent{ID: agentID, OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})
	if err != nil || job == nil {
		t.Fatalf("claim test job: %#v %v", job, err)
	}
	return job
}

func jobTestIdentity(t *testing.T, agentID string) (Identity, ed25519.PrivateKey) {
	t.Helper()
	identity := testIdentity(t, agentID)
	block, _ := pem.Decode(identity.PrivateKeyPEM)
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return identity, key.(ed25519.PrivateKey)
}

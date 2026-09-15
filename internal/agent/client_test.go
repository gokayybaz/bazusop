package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestClientEnrollsAndReportsAgentSnapshotOverMTLS(t *testing.T) {
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatal(err)
	}
	inventoryService := inventory.NewService(inventory.NewMemoryStore())
	telemetryService := telemetry.NewService(telemetry.NewMemoryStore())
	serviceInventory := serviceinventory.NewService(serviceinventory.NewMemoryStore())
	logs := logstream.NewService(logstream.NewMemoryStore())
	jobService, err := jobs.NewService(jobs.NewMemoryStore(), jobs.WithSigningKey(authority.JobSigningKey()))
	if err != nil {
		t.Fatal(err)
	}
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithInventory(inventoryService), server.WithTelemetry(telemetryService), server.WithServiceInventory(serviceInventory), server.WithLogs(logs), server.WithJobs(jobService, "operator-secret"))

	directory := t.TempDir()
	configuration := Config{
		HubURL: "https://hub.example.test", EnrollmentToken: "bootstrap-secret", StateDir: filepath.Join(directory, "state"),
		ReportInterval: time.Minute,
	}
	client, err := NewClient(configuration, NewIdentityStore(configuration.StateDir))
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	client.do = handlerTransport(t, handler)

	identity, err := client.EnsureIdentity(context.Background(), "web-01", "linux")
	if err != nil {
		t.Fatalf("enroll agent: %v", err)
	}
	if identity.AgentID == "" {
		t.Fatal("expected issued agent ID")
	}
	facts := inventory.Facts{Hostname: "web-01", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", KernelVersion: "6.8", CPUCores: 4, MemoryBytes: 8 << 30, IPAddresses: []string{"10.0.0.8"}, AgentVersion: "test"}
	if err := client.ReportInventory(context.Background(), identity, facts); err != nil {
		t.Fatalf("report inventory: %v", err)
	}

	hosts, err := inventoryService.List(context.Background(), tenancy.DefaultScope())
	if err != nil || len(hosts) != 1 {
		t.Fatalf("expected reported host, hosts=%#v err=%v", hosts, err)
	}
	if hosts[0].AgentID != identity.AgentID || hosts[0].Hostname != "web-01" {
		t.Fatalf("unexpected reported host: %#v", hosts[0])
	}
	recordedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := client.ReportTelemetry(context.Background(), identity, telemetry.Sample{RecordedAt: recordedAt, CPUPercent: 12.5, MemoryPercent: 40, DiskPercent: 50}); err != nil {
		t.Fatalf("report telemetry: %v", err)
	}
	samples, err := telemetryService.History(context.Background(), tenancy.DefaultScope(), identity.AgentID, recordedAt.Add(-time.Second), recordedAt.Add(time.Second), 10)
	if err != nil || len(samples) != 1 || samples[0].CPUPercent != 12.5 {
		t.Fatalf("expected reported telemetry, samples=%#v err=%v", samples, err)
	}
	serviceSnapshot := serviceinventory.Snapshot{ObservedAt: recordedAt, Services: []serviceinventory.Fact{{Name: "nginx.service", DisplayName: "NGINX", State: "running", StartupType: "automatic"}}}
	if err := client.ReportServices(context.Background(), identity, serviceSnapshot); err != nil {
		t.Fatalf("report services: %v", err)
	}
	services, err := serviceInventory.List(context.Background(), tenancy.DefaultScope(), identity.AgentID, serviceinventory.Filter{})
	if err != nil || len(services) != 1 || services[0].Name != "nginx.service" {
		t.Fatalf("expected reported services, services=%#v err=%v", services, err)
	}
	logTime := recordedAt.Add(time.Second)
	if err := client.ReportLogs(context.Background(), identity, logstream.Batch{Entries: []logstream.Entry{{OccurredAt: logTime, Collector: "journald", Source: "nginx.service", Severity: "warn", Message: "retrying upstream"}}}); err != nil {
		t.Fatalf("report logs: %v", err)
	}
	entries, err := logs.Search(context.Background(), logstream.Query{Scope: tenancy.DefaultScope(), AgentID: identity.AgentID, From: recordedAt, To: logTime.Add(time.Second), Limit: 10})
	if err != nil || len(entries) != 1 || entries[0].Message != "retrying upstream" {
		t.Fatalf("expected reported logs, entries=%#v err=%v", entries, err)
	}
	created, err := jobService.Create(context.Background(), identity.AgentID, jobs.CreateRequest{Action: jobs.ActionServiceRestart, Target: "nginx.service", ApprovedBy: "ops", Reason: "deploy"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	claimed, err := client.ClaimNextJob(context.Background(), identity)
	if err != nil || claimed == nil || claimed.ID != created.ID {
		t.Fatalf("claim job: %#v %v", claimed, err)
	}
	if err := client.ReportJobEvent(context.Background(), identity, claimed.ID, jobs.EventRequest{Sequence: 2, Type: jobs.EventSucceeded, Message: "nginx restarted"}); err != nil {
		t.Fatalf("report job event: %v", err)
	}
	events, err := jobService.Events(context.Background(), identity.AgentID, claimed.ID)
	if err != nil || len(events) != 3 || events[2].Type != jobs.EventSucceeded {
		t.Fatalf("expected completed job audit, events=%#v err=%v", events, err)
	}
	if next, err := client.ClaimNextJob(context.Background(), identity); err != nil || next != nil {
		t.Fatalf("expected empty job queue, job=%#v err=%v", next, err)
	}
	loaded, err := NewIdentityStore(configuration.StateDir).Load()
	if err != nil || loaded.AgentID != identity.AgentID {
		t.Fatalf("identity was not persisted: %#v %v", loaded, err)
	}
	client.now = func() time.Time { return identity.ExpiresAt.Add(-30 * time.Minute) }
	renewed, err := client.EnsureIdentity(context.Background(), "web-01", "linux")
	if err != nil {
		t.Fatalf("renew identity: %v", err)
	}
	if renewed.AgentID != identity.AgentID || string(renewed.CertificatePEM) == string(identity.CertificatePEM) {
		t.Fatalf("expected a renewed certificate for the same agent: %#v", renewed)
	}
}

func TestClientTLSAlwaysVerifiesHub(t *testing.T) {
	configuration := Config{HubURL: "https://hub.example.test", StateDir: t.TempDir(), ReportInterval: time.Minute}
	client, err := NewClient(configuration, NewIdentityStore(configuration.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	client.configuration.EnrollmentToken = "token"
	client.do = func(_ *http.Request, tlsConfiguration *tls.Config) (*http.Response, error) {
		if tlsConfiguration.InsecureSkipVerify {
			t.Fatal("hub certificate verification must never be disabled")
		}
		return &http.Response{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized", Body: http.NoBody}, nil
	}
	_, _ = client.EnsureIdentity(context.Background(), "web-01", "linux")
}

func TestClientSplitsLargeLogBatchBelowHubRequestLimit(t *testing.T) {
	configuration := Config{HubURL: "https://hub.example.test", StateDir: t.TempDir(), ReportInterval: time.Minute}
	client, err := NewClient(configuration, NewIdentityStore(configuration.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	requests, entries := 0, 0
	client.do = func(request *http.Request, _ *tls.Config) (*http.Response, error) {
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if len(payload) > maxRequestBodyBytes {
			t.Fatalf("request body exceeds hub limit: %d", len(payload))
		}
		var batch logstream.Batch
		if err := json.Unmarshal(payload, &batch); err != nil {
			t.Fatal(err)
		}
		requests++
		entries += len(batch.Entries)
		return &http.Response{StatusCode: http.StatusNoContent, Status: "204 No Content", Body: http.NoBody}, nil
	}
	batch := logstream.Batch{Entries: make([]logstream.Entry, 20)}
	for index := range batch.Entries {
		batch.Entries[index] = logstream.Entry{OccurredAt: time.Now().UTC(), Collector: "journald", Source: "app.service", Severity: "info", Message: strings.Repeat("x", 60*1024)}
	}
	if err := client.ReportLogs(context.Background(), testIdentity(t, "agent-test"), batch); err != nil {
		t.Fatalf("report logs: %v", err)
	}
	if requests < 2 || entries != len(batch.Entries) {
		t.Fatalf("unexpected split: requests=%d entries=%d", requests, entries)
	}
}

func TestDeterministicLogIDIsStableAndAgentScoped(t *testing.T) {
	entry := logstream.Entry{OccurredAt: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC), Collector: "journald", Source: "app.service", Severity: "info", Message: "started", SourceID: "cursor-01"}
	first := deterministicLogID("agent-01", entry)
	otherSource := entry
	otherSource.SourceID = "cursor-02"
	if first != deterministicLogID("agent-01", entry) || first == deterministicLogID("agent-02", entry) || first == deterministicLogID("agent-01", otherSource) || len(first) != 32 {
		t.Fatalf("unexpected deterministic IDs: %q", first)
	}
}

func handlerTransport(t *testing.T, handler http.Handler) func(*http.Request, *tls.Config) (*http.Response, error) {
	t.Helper()
	return func(request *http.Request, tlsConfiguration *tls.Config) (*http.Response, error) {
		request.RemoteAddr = "127.0.0.1:12345"
		if len(tlsConfiguration.Certificates) > 0 {
			peer, err := x509.ParseCertificate(tlsConfiguration.Certificates[0].Certificate[0])
			if err != nil {
				t.Fatalf("parse client certificate: %v", err)
			}
			request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{peer}}
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	}
}

func TestServerCAPoolRejectsInvalidPEM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.crt")
	if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadServerRoots(path); err == nil {
		t.Fatal("expected invalid server CA to be rejected")
	}
}

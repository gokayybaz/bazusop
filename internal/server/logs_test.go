package server_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/server"
)

func TestEnrolledAgentReportsSearchableLogs(t *testing.T) {
	t.Parallel()
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	identity, err := authority.Enroll(enrollment.Request{BootstrapToken: "bootstrap-secret", Name: "edge-01", OperatingSystem: "linux", CSRPEM: serverCSR(t, "edge-01")})
	if err != nil {
		t.Fatalf("enroll identity: %v", err)
	}
	logs := logstream.NewService(logstream.NewMemoryStore())
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithLogs(logs))
	base := time.Date(2026, 9, 11, 5, 0, 0, 0, time.UTC)

	report := httptest.NewRequest(http.MethodPost, "/api/v1/agents/logs", encodeJSON(t, logstream.Batch{Entries: []logstream.Entry{
		{OccurredAt: base, Collector: "journald", Source: "nginx.service", Severity: "error", Message: "upstream timeout"},
	}}))
	report.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, identity.CertificatePEM)}}
	reportResponse := httptest.NewRecorder()
	handler.ServeHTTP(reportResponse, report)
	if reportResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", reportResponse.Code, reportResponse.Body.String())
	}

	path := "/api/v1/instances/" + identity.AgentID + "/logs?from=" + base.Add(-time.Minute).Format(time.RFC3339) + "&to=" + base.Add(time.Minute).Format(time.RFC3339) + "&severity=error&limit=100"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Entries []logstream.Entry `json:"entries"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(payload.Entries) != 1 || payload.Entries[0].Message != "upstream timeout" {
		t.Fatalf("unexpected logs: %#v", payload.Entries)
	}
}

func TestLogStreamPublishesServerSentEvents(t *testing.T) {
	logs := logstream.NewService(logstream.NewMemoryStore())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := newStreamRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/instances/agent-01/logs/stream", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		server.NewHandler(server.WithLogs(logs)).ServeHTTP(recorder, request)
		close(done)
	}()
	recorder.waitFor(t, "event: ready")
	if recorder.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("unexpected content type %q", recorder.Header().Get("Content-Type"))
	}

	if err := logs.Ingest(context.Background(), "agent-01", logstream.Batch{Entries: []logstream.Entry{{OccurredAt: time.Now(), Collector: "file", Source: "app.log", Severity: "info", Message: "live entry"}}}); err != nil {
		t.Fatalf("ingest live log: %v", err)
	}
	recorder.waitFor(t, "live entry")
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not stop after cancellation")
	}
}

func TestLogIngestRequiresAgentIdentity(t *testing.T) {
	t.Parallel()
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithLogs(logstream.NewService(logstream.NewMemoryStore())))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/logs", encodeJSON(t, logstream.Batch{}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestEmptyLogSearchReturnsJSONArray(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(server.WithLogs(logstream.NewService(logstream.NewMemoryStore())))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/instances/agent-01/logs?limit=20", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if strings.TrimSpace(response.Body.String()) != `{"entries":[]}` {
		t.Fatalf("expected empty JSON array, got %s", response.Body.String())
	}
}

type streamRecorder struct {
	mu     sync.Mutex
	header http.Header
	body   bytes.Buffer
	flush  chan struct{}
}

func newStreamRecorder() *streamRecorder {
	return &streamRecorder{header: make(http.Header), flush: make(chan struct{}, 1)}
}

func (recorder *streamRecorder) Header() http.Header { return recorder.header }
func (recorder *streamRecorder) WriteHeader(_ int)   {}
func (recorder *streamRecorder) Write(value []byte) (int, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.body.Write(value)
}
func (recorder *streamRecorder) Flush() {
	select {
	case recorder.flush <- struct{}{}:
	default:
	}
}
func (recorder *streamRecorder) waitFor(t *testing.T, expected string) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		recorder.mu.Lock()
		found := strings.Contains(recorder.body.String(), expected)
		recorder.mu.Unlock()
		if found {
			return
		}
		select {
		case <-recorder.flush:
		case <-timer.C:
			t.Fatalf("stream did not contain %q", expected)
		}
	}
}

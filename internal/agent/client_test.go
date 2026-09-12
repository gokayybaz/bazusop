package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/server"
)

func TestClientEnrollsPersistsIdentityAndReportsInventoryOverMTLS(t *testing.T) {
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatal(err)
	}
	inventoryService := inventory.NewService(inventory.NewMemoryStore())
	handler := server.NewHandler(server.WithEnrollment(authority), server.WithInventory(inventoryService))

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

	hosts, err := inventoryService.List(context.Background())
	if err != nil || len(hosts) != 1 {
		t.Fatalf("expected reported host, hosts=%#v err=%v", hosts, err)
	}
	if hosts[0].AgentID != identity.AgentID || hosts[0].Hostname != "web-01" {
		t.Fatalf("unexpected reported host: %#v", hosts[0])
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

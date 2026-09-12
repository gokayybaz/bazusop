package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	"github.com/gokayybaz/bazusop/internal/telemetry"
)

const renewalWindow = time.Hour

type Client struct {
	configuration Config
	store         *IdentityStore
	serverRoots   *x509.CertPool
	now           func() time.Time
	do            func(*http.Request, *tls.Config) (*http.Response, error)
}

func NewClient(configuration Config, store *IdentityStore) (*Client, error) {
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("identity store is required")
	}
	roots, err := loadServerRoots(configuration.ServerCAFile)
	if err != nil {
		return nil, err
	}
	return &Client{configuration: configuration, store: store, serverRoots: roots, now: time.Now, do: executeRequest}, nil
}

func (client *Client) EnsureIdentity(ctx context.Context, name, operatingSystem string) (Identity, error) {
	identity, err := client.store.Load()
	if err == nil {
		if !client.now().UTC().Before(identity.ExpiresAt) {
			return Identity{}, errors.New("agent certificate expired; a new enrollment token is required")
		}
		if identity.ExpiresAt.Sub(client.now().UTC()) <= renewalWindow {
			return client.renew(ctx, identity)
		}
		return identity, nil
	}
	if !os.IsNotExist(err) {
		return Identity{}, fmt.Errorf("load agent identity: %w", err)
	}
	if strings.TrimSpace(client.configuration.EnrollmentToken) == "" {
		return Identity{}, errors.New("BAZUSOP_AGENT_ENROLLMENT_TOKEN is required for first enrollment")
	}
	return client.enroll(ctx, name, operatingSystem)
}

func (client *Client) enroll(ctx context.Context, name, operatingSystem string) (Identity, error) {
	privateKey, privateKeyPEM, csrPEM, err := generateIdentityMaterial(name)
	if err != nil {
		return Identity{}, err
	}
	request := enrollment.Request{
		BootstrapToken: client.configuration.EnrollmentToken,
		Name:           name, OperatingSystem: operatingSystem, CSRPEM: string(csrPEM),
	}
	var response enrollment.Identity
	if err := client.request(ctx, http.MethodPost, "/api/v1/agents/enroll", request, nil, http.StatusCreated, &response); err != nil {
		return Identity{}, fmt.Errorf("enroll agent: %w", err)
	}
	identity := identityFromEnrollment(response, privateKeyPEM)
	if _, err := tls.X509KeyPair(identity.CertificatePEM, marshalPrivateKey(privateKey)); err != nil {
		return Identity{}, fmt.Errorf("validate issued agent certificate: %w", err)
	}
	if err := client.store.Save(identity); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (client *Client) renew(ctx context.Context, current Identity) (Identity, error) {
	_, privateKeyPEM, csrPEM, err := generateIdentityMaterial(current.AgentID)
	if err != nil {
		return Identity{}, err
	}
	var response enrollment.Identity
	if err := client.request(ctx, http.MethodPost, "/api/v1/agents/renew", map[string]string{"csr": string(csrPEM)}, &current, http.StatusOK, &response); err != nil {
		return Identity{}, fmt.Errorf("renew agent identity: %w", err)
	}
	identity := identityFromEnrollment(response, privateKeyPEM)
	if identity.AgentID != current.AgentID {
		return Identity{}, errors.New("renewed identity changed agent ID")
	}
	if err := client.store.Save(identity); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (client *Client) ReportInventory(ctx context.Context, identity Identity, facts inventory.Facts) error {
	if err := client.request(ctx, http.MethodPut, "/api/v1/agents/inventory", facts, &identity, http.StatusNoContent, nil); err != nil {
		return fmt.Errorf("report inventory: %w", err)
	}
	return nil
}

func (client *Client) ReportTelemetry(ctx context.Context, identity Identity, sample telemetry.Sample) error {
	if err := client.request(ctx, http.MethodPost, "/api/v1/agents/telemetry", sample, &identity, http.StatusNoContent, nil); err != nil {
		return fmt.Errorf("report telemetry: %w", err)
	}
	return nil
}

func (client *Client) ReportServices(ctx context.Context, identity Identity, snapshot serviceinventory.Snapshot) error {
	if err := client.request(ctx, http.MethodPut, "/api/v1/agents/services", snapshot, &identity, http.StatusNoContent, nil); err != nil {
		return fmt.Errorf("report services: %w", err)
	}
	return nil
}

func (client *Client) request(ctx context.Context, method, path string, body any, identity *Identity, expectedStatus int, destination any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	tlsConfiguration := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: client.serverRoots}
	if identity != nil {
		certificate, err := identity.TLSCertificate()
		if err != nil {
			return err
		}
		tlsConfiguration.Certificates = []tls.Certificate{certificate}
	}
	request, err := http.NewRequestWithContext(ctx, method, client.configuration.HubURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.do(request, tlsConfiguration)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("hub returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	if destination != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(destination); err != nil {
			return fmt.Errorf("decode hub response: %w", err)
		}
	}
	return nil
}

func executeRequest(request *http.Request, tlsConfiguration *tls.Config) (*http.Response, error) {
	httpClient := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfiguration, ForceAttemptHTTP2: true},
	}
	return httpClient.Do(request)
}

func generateIdentityMaterial(commonName string) (ed25519.PrivateKey, []byte, []byte, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate agent key: %w", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: commonName}}, privateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create agent CSR: %w", err)
	}
	privateKeyPEM := marshalPrivateKey(privateKey)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	return privateKey, privateKeyPEM, csrPEM, nil
}

func marshalPrivateKey(privateKey ed25519.PrivateKey) []byte {
	der, _ := x509.MarshalPKCS8PrivateKey(privateKey)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func identityFromEnrollment(response enrollment.Identity, privateKeyPEM []byte) Identity {
	return Identity{
		AgentID: response.AgentID, CertificatePEM: []byte(response.CertificatePEM), PrivateKeyPEM: privateKeyPEM,
		CACertificatePEM: []byte(response.CACertificatePEM), ExpiresAt: response.ExpiresAt.UTC(),
	}
}

func loadServerRoots(path string) (*x509.CertPool, error) {
	if path == "" {
		return nil, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read hub server CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(contents) {
		return nil, errors.New("hub server CA file does not contain a certificate")
	}
	return pool, nil
}

package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIdentityStoreRoundTrip(t *testing.T) {
	directory := t.TempDir()
	identity := testIdentity(t, "agent-test")
	store := NewIdentityStore(directory)

	if err := store.Save(identity); err != nil {
		t.Fatalf("save identity: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	if loaded.AgentID != identity.AgentID || !loaded.ExpiresAt.Equal(identity.ExpiresAt) {
		t.Fatalf("unexpected loaded identity: %#v", loaded)
	}
	if _, err := loaded.TLSCertificate(); err != nil {
		t.Fatalf("load TLS certificate: %v", err)
	}
	if mode := fileMode(t, filepath.Join(directory, privateKeyFile)); mode.Perm() != 0o600 {
		t.Fatalf("private key permissions must be 0600, got %o", mode.Perm())
	}
}

func TestIdentityStoreReportsMissingIdentity(t *testing.T) {
	_, err := NewIdentityStore(t.TempDir()).Load()
	if !os.IsNotExist(err) {
		t.Fatalf("expected missing identity, got %v", err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode()
}

func testIdentity(t *testing.T, agentID string) Identity {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: agentID},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return Identity{
		AgentID:          agentID,
		CertificatePEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		PrivateKeyPEM:    pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}),
		CACertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		ExpiresAt:        template.NotAfter.UTC(),
	}
}

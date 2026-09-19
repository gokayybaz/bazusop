package agent

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	identityFile    = "identity.json"
	privateKeyFile  = "agent.key"
	certificateFile = "agent.crt"
	agentCAFile     = "agent-ca.crt"
)

type Identity struct {
	AgentID          string    `json:"agent_id"`
	CertificatePEM   []byte    `json:"-"`
	PrivateKeyPEM    []byte    `json:"-"`
	CACertificatePEM []byte    `json:"-"`
	ExpiresAt        time.Time `json:"expires_at"`
}

func (identity Identity) TLSCertificate() (tls.Certificate, error) {
	certificate, err := tls.X509KeyPair(identity.CertificatePEM, identity.PrivateKeyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load agent certificate: %w", err)
	}
	return certificate, nil
}

type identityMetadata struct {
	AgentID   string    `json:"agent_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type IdentityStore struct{ directory string }

func NewIdentityStore(directory string) *IdentityStore { return &IdentityStore{directory: directory} }

func (store *IdentityStore) Load() (Identity, error) {
	if err := protectStateDirectory(store.directory); err != nil && !os.IsNotExist(err) {
		return Identity{}, fmt.Errorf("protect agent state directory: %w", err)
	}
	metadataBytes, err := os.ReadFile(filepath.Join(store.directory, identityFile))
	if err != nil {
		return Identity{}, err
	}
	var metadata identityMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return Identity{}, fmt.Errorf("decode agent identity: %w", err)
	}
	identity := Identity{AgentID: metadata.AgentID, ExpiresAt: metadata.ExpiresAt}
	for path, target := range map[string]*[]byte{
		certificateFile: &identity.CertificatePEM,
		privateKeyFile:  &identity.PrivateKeyPEM,
		agentCAFile:     &identity.CACertificatePEM,
	} {
		contents, readErr := os.ReadFile(filepath.Join(store.directory, path))
		if readErr != nil {
			return Identity{}, readErr
		}
		*target = contents
	}
	if metadata.AgentID == "" || metadata.ExpiresAt.IsZero() {
		return Identity{}, errors.New("stored agent identity is incomplete")
	}
	if _, err := identity.TLSCertificate(); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (store *IdentityStore) Save(identity Identity) error {
	if identity.AgentID == "" || identity.ExpiresAt.IsZero() {
		return errors.New("agent identity is incomplete")
	}
	if _, err := identity.TLSCertificate(); err != nil {
		return err
	}
	if err := os.MkdirAll(store.directory, 0o700); err != nil {
		return fmt.Errorf("create agent state directory: %w", err)
	}
	if err := protectStateDirectory(store.directory); err != nil {
		return fmt.Errorf("protect agent state directory: %w", err)
	}
	metadata, err := json.MarshalIndent(identityMetadata{AgentID: identity.AgentID, ExpiresAt: identity.ExpiresAt.UTC()}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode agent identity: %w", err)
	}
	files := []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{privateKeyFile, identity.PrivateKeyPEM, 0o600},
		{certificateFile, identity.CertificatePEM, 0o644},
		{agentCAFile, identity.CACertificatePEM, 0o644},
		{identityFile, append(metadata, '\n'), 0o600},
	}
	for _, file := range files {
		if err := atomicWrite(filepath.Join(store.directory, file.name), file.data, file.mode); err != nil {
			return err
		}
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".bazusop-agent-*")
	if err != nil {
		return fmt.Errorf("create temporary identity file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect identity file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write identity file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync identity file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close identity file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace identity file: %w", err)
	}
	return nil
}

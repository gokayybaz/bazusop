package cloudinventory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

var (
	ErrInvalidCloudInventory = errors.New("invalid cloud inventory")
	ErrCloudAccountNotFound  = errors.New("cloud account not found")
)

type Provider string
type AccountStatus string
type MatchStatus string

const (
	ProviderAWS   Provider = "aws"
	ProviderAzure Provider = "azure"
	ProviderGCP   Provider = "gcp"

	AccountPending   AccountStatus = "pending"
	AccountConnected AccountStatus = "connected"

	MatchVerified  MatchStatus = "verified"
	MatchCandidate MatchStatus = "candidate"
	MatchUnmatched MatchStatus = "unmatched"
)

type AccountRequest struct {
	Name       string   `json:"name"`
	Provider   Provider `json:"provider"`
	ExternalID string   `json:"external_id"`
}

type Account struct {
	ID string `json:"id"`
	AccountRequest
	Status     AccountStatus `json:"status"`
	LastSyncAt *time.Time    `json:"last_sync_at,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
}

type DiscoveredInstance struct {
	ProviderInstanceID string            `json:"provider_instance_id"`
	Name               string            `json:"name"`
	Region             string            `json:"region"`
	Zone               string            `json:"zone"`
	State              string            `json:"state"`
	OSFamily           string            `json:"os_family"`
	PrivateIPs         []string          `json:"private_ips"`
	PublicIPs          []string          `json:"public_ips"`
	AgentIDHint        string            `json:"agent_id_hint"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

type Instance struct {
	AccountID   string   `json:"account_id"`
	AccountName string   `json:"account_name"`
	Provider    Provider `json:"provider"`
	DiscoveredInstance
	AgentID          string      `json:"agent_id"`
	CandidateAgentID string      `json:"candidate_agent_id,omitempty"`
	MatchStatus      MatchStatus `json:"match_status"`
	MatchReason      string      `json:"match_reason"`
	DiscoveredAt     time.Time   `json:"discovered_at"`
}

type Store interface {
	CreateCloudAccount(context.Context, Account) error
	GetCloudAccount(context.Context, string) (Account, error)
	ListCloudAccounts(context.Context) ([]Account, error)
	ReplaceCloudInstances(context.Context, Account, []Instance) error
	ListCloudInstances(context.Context) ([]Instance, error)
}

type HostLister interface {
	List(context.Context, tenancy.Scope) ([]inventory.Host, error)
}

type Service struct {
	store Store
	hosts HostLister
	now   func() time.Time
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option {
	return func(service *Service) { service.now = clock }
}

func NewService(store Store, hosts HostLister, options ...Option) *Service {
	service := &Service{store: store, hosts: hosts, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

func (service *Service) CreateAccount(ctx context.Context, request AccountRequest) (Account, error) {
	request.Name = strings.TrimSpace(request.Name)
	request.ExternalID = strings.TrimSpace(request.ExternalID)
	if request.Name == "" || len(request.Name) > 128 || request.ExternalID == "" || len(request.ExternalID) > 256 || !validProvider(request.Provider) {
		return Account{}, ErrInvalidCloudInventory
	}
	id, err := newID()
	if err != nil {
		return Account{}, err
	}
	account := Account{ID: id, AccountRequest: request, Status: AccountPending, CreatedAt: service.now().UTC().Truncate(time.Microsecond)}
	if err := service.store.CreateCloudAccount(ctx, account); err != nil {
		return Account{}, err
	}
	return account, nil
}

func (service *Service) ListAccounts(ctx context.Context) ([]Account, error) {
	return service.store.ListCloudAccounts(ctx)
}

func (service *Service) ListInstances(ctx context.Context) ([]Instance, error) {
	return service.store.ListCloudInstances(ctx)
}

func (service *Service) Reconcile(ctx context.Context, accountID string, discovered []DiscoveredInstance) ([]Instance, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || len(discovered) > 10000 {
		return nil, ErrInvalidCloudInventory
	}
	account, err := service.store.GetCloudAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	hosts, err := service.hosts.List(ctx, tenancy.DefaultScope())
	if err != nil {
		return nil, err
	}
	hostByID := make(map[string]inventory.Host, len(hosts))
	for _, host := range hosts {
		hostByID[host.AgentID] = host
	}

	now := service.now().UTC().Truncate(time.Microsecond)
	instances := make([]Instance, 0, len(discovered))
	seen := make(map[string]struct{}, len(discovered))
	for _, value := range discovered {
		normalized, err := normalizeDiscovery(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[normalized.ProviderInstanceID]; exists {
			return nil, ErrInvalidCloudInventory
		}
		seen[normalized.ProviderInstanceID] = struct{}{}
		instance := Instance{AccountID: account.ID, AccountName: account.Name, Provider: account.Provider, DiscoveredInstance: normalized, MatchStatus: MatchUnmatched, MatchReason: "no_agent_signal", DiscoveredAt: now}
		if host, found := hostByID[normalized.AgentIDHint]; normalized.AgentIDHint != "" && found {
			instance.AgentID = host.AgentID
			instance.MatchStatus = MatchVerified
			instance.MatchReason = "provider_agent_id"
		} else if normalized.AgentIDHint != "" {
			instance.MatchReason = "agent_identity_not_found"
		} else if candidate := uniqueCandidate(normalized, hosts); candidate != "" {
			instance.CandidateAgentID = candidate
			instance.MatchStatus = MatchCandidate
			instance.MatchReason = "hostname_or_private_ip"
		}
		instances = append(instances, instance)
	}
	account.Status = AccountConnected
	account.LastSyncAt = &now
	if err := service.store.ReplaceCloudInstances(ctx, account, instances); err != nil {
		return nil, err
	}
	return instances, nil
}

func validProvider(provider Provider) bool {
	return provider == ProviderAWS || provider == ProviderAzure || provider == ProviderGCP
}

func normalizeDiscovery(value DiscoveredInstance) (DiscoveredInstance, error) {
	value.ProviderInstanceID = strings.TrimSpace(value.ProviderInstanceID)
	value.Name = strings.TrimSpace(value.Name)
	value.Region = strings.TrimSpace(value.Region)
	value.Zone = strings.TrimSpace(value.Zone)
	value.State = strings.ToLower(strings.TrimSpace(value.State))
	value.OSFamily = strings.ToLower(strings.TrimSpace(value.OSFamily))
	value.AgentIDHint = strings.TrimSpace(value.AgentIDHint)
	value.PrivateIPs = normalizeIPs(value.PrivateIPs)
	value.PublicIPs = normalizeIPs(value.PublicIPs)
	if value.ProviderInstanceID == "" || len(value.ProviderInstanceID) > 256 || value.Name == "" || len(value.Name) > 253 || value.Region == "" || len(value.Region) > 128 || value.State == "" || (value.OSFamily != "linux" && value.OSFamily != "windows" && value.OSFamily != "unknown") || len(value.AgentIDHint) > 256 {
		return DiscoveredInstance{}, ErrInvalidCloudInventory
	}
	return value, nil
}

func normalizeIPs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if address := net.ParseIP(strings.TrimSpace(value)); address != nil {
			normalized := address.String()
			if _, exists := seen[normalized]; !exists {
				seen[normalized] = struct{}{}
				result = append(result, normalized)
			}
		}
	}
	return result
}

func uniqueCandidate(discovered DiscoveredInstance, hosts []inventory.Host) string {
	candidates := make(map[string]struct{})
	name := strings.ToLower(strings.TrimSuffix(discovered.Name, "."))
	privateIPs := make(map[string]struct{}, len(discovered.PrivateIPs))
	for _, address := range discovered.PrivateIPs {
		privateIPs[address] = struct{}{}
	}
	for _, host := range hosts {
		matched := strings.ToLower(strings.TrimSuffix(host.Hostname, ".")) == name
		for _, address := range host.IPAddresses {
			if _, exists := privateIPs[address]; exists {
				matched = true
			}
		}
		if matched {
			candidates[host.AgentID] = struct{}{}
		}
	}
	if len(candidates) != 1 {
		return ""
	}
	for agentID := range candidates {
		return agentID
	}
	return ""
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

type MemoryStore struct {
	mu        sync.RWMutex
	accounts  map[string]Account
	instances map[string][]Instance
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{accounts: make(map[string]Account), instances: make(map[string][]Instance)}
}

func (store *MemoryStore) CreateCloudAccount(_ context.Context, account Account) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.accounts[account.ID] = account
	return nil
}

func (store *MemoryStore) GetCloudAccount(_ context.Context, id string) (Account, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	account, exists := store.accounts[id]
	if !exists {
		return Account{}, ErrCloudAccountNotFound
	}
	return account, nil
}

func (store *MemoryStore) ListCloudAccounts(_ context.Context) ([]Account, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	result := make([]Account, 0, len(store.accounts))
	for _, account := range store.accounts {
		result = append(result, account)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (store *MemoryStore) ReplaceCloudInstances(_ context.Context, account Account, instances []Instance) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.accounts[account.ID]; !exists {
		return ErrCloudAccountNotFound
	}
	store.accounts[account.ID] = account
	store.instances[account.ID] = append([]Instance(nil), instances...)
	return nil
}

func (store *MemoryStore) ListCloudInstances(_ context.Context) ([]Instance, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	result := make([]Instance, 0)
	for _, instances := range store.instances {
		result = append(result, instances...)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].AccountName == result[j].AccountName {
			return result[i].Name < result[j].Name
		}
		return result[i].AccountName < result[j].AccountName
	})
	return result, nil
}

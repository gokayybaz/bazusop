package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func (store *Store) CreateCloudAccount(ctx context.Context, scope tenancy.Scope, account cloudinventory.Account) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if account.OrganizationID != scope.OrganizationID || account.SiteID != scope.SiteID {
		return tenancy.ErrInvalidScope
	}
	_, err := store.pool.Exec(ctx, `
		INSERT INTO cloud_accounts (organization_id,site_id,id,name,provider,external_id,status,last_sync_at,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, scope.OrganizationID, scope.SiteID, account.ID, account.Name, account.Provider, account.ExternalID, account.Status, account.LastSyncAt, account.CreatedAt)
	if err != nil {
		return fmt.Errorf("create cloud account: %w", err)
	}
	return nil
}

func (store *Store) GetCloudAccount(ctx context.Context, scope tenancy.Scope, id string) (cloudinventory.Account, error) {
	if err := scope.Validate(); err != nil {
		return cloudinventory.Account{}, err
	}
	var account cloudinventory.Account
	err := store.pool.QueryRow(ctx, `SELECT organization_id,site_id,id,name,provider,external_id,status,last_sync_at,created_at FROM cloud_accounts WHERE organization_id=$1 AND site_id=$2 AND id=$3`, scope.OrganizationID, scope.SiteID, id).Scan(
		&account.OrganizationID, &account.SiteID, &account.ID, &account.Name, &account.Provider, &account.ExternalID, &account.Status, &account.LastSyncAt, &account.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudinventory.Account{}, cloudinventory.ErrCloudAccountNotFound
	}
	if err != nil {
		return cloudinventory.Account{}, fmt.Errorf("get cloud account: %w", err)
	}
	return account, nil
}

func (store *Store) ListCloudAccounts(ctx context.Context, scope tenancy.Scope) ([]cloudinventory.Account, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `SELECT organization_id,site_id,id,name,provider,external_id,status,last_sync_at,created_at FROM cloud_accounts WHERE organization_id=$1 AND site_id=$2 ORDER BY name,id`, scope.OrganizationID, scope.SiteID)
	if err != nil {
		return nil, fmt.Errorf("query cloud accounts: %w", err)
	}
	defer rows.Close()
	accounts := make([]cloudinventory.Account, 0)
	for rows.Next() {
		var account cloudinventory.Account
		if err := rows.Scan(&account.OrganizationID, &account.SiteID, &account.ID, &account.Name, &account.Provider, &account.ExternalID, &account.Status, &account.LastSyncAt, &account.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan cloud account: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud accounts: %w", err)
	}
	return accounts, nil
}

func (store *Store) ReplaceCloudInstances(ctx context.Context, scope tenancy.Scope, account cloudinventory.Account, instances []cloudinventory.Instance) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if account.OrganizationID != scope.OrganizationID || account.SiteID != scope.SiteID {
		return tenancy.ErrInvalidScope
	}
	for _, instance := range instances {
		if instance.OrganizationID != scope.OrganizationID || instance.SiteID != scope.SiteID || instance.AccountID != account.ID {
			return tenancy.ErrInvalidScope
		}
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cloud reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `UPDATE cloud_accounts SET status=$3,last_sync_at=$4 WHERE organization_id=$1 AND site_id=$2 AND id=$5`, scope.OrganizationID, scope.SiteID, account.Status, account.LastSyncAt, account.ID)
	if err != nil {
		return fmt.Errorf("update cloud account sync: %w", err)
	}
	if result.RowsAffected() == 0 {
		return cloudinventory.ErrCloudAccountNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cloud_instances WHERE organization_id=$1 AND site_id=$2 AND account_id=$3`, scope.OrganizationID, scope.SiteID, account.ID); err != nil {
		return fmt.Errorf("replace cloud instances: %w", err)
	}
	for _, instance := range instances {
		metadata, err := json.Marshal(instance.Metadata)
		if err != nil {
			return fmt.Errorf("encode cloud instance metadata: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO cloud_instances (
				organization_id,site_id,account_id,provider_instance_id,name,region,zone,state,os_family,private_ips,public_ips,
				agent_id_hint,metadata,agent_id,candidate_agent_id,match_status,match_reason,discovered_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,''),NULLIF($15,''),$16,$17,$18)`,
			scope.OrganizationID, scope.SiteID, instance.AccountID, instance.ProviderInstanceID, instance.Name, instance.Region, instance.Zone, instance.State,
			instance.OSFamily, instance.PrivateIPs, instance.PublicIPs, instance.AgentIDHint, metadata, instance.AgentID,
			instance.CandidateAgentID, instance.MatchStatus, instance.MatchReason, instance.DiscoveredAt,
		)
		if err != nil {
			return fmt.Errorf("insert cloud instance: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cloud reconciliation: %w", err)
	}
	return nil
}

func (store *Store) ListCloudInstances(ctx context.Context, scope tenancy.Scope) ([]cloudinventory.Instance, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT i.organization_id,i.site_id,i.account_id,a.name,a.provider,i.provider_instance_id,i.name,i.region,i.zone,i.state,i.os_family,
			i.private_ips,i.public_ips,i.agent_id_hint,i.metadata,COALESCE(i.agent_id,''),COALESCE(i.candidate_agent_id,''),
			i.match_status,i.match_reason,i.discovered_at
		FROM cloud_instances i JOIN cloud_accounts a ON a.id=i.account_id AND a.organization_id=i.organization_id AND a.site_id=i.site_id
		WHERE i.organization_id=$1 AND i.site_id=$2
		ORDER BY a.name,i.name,i.provider_instance_id`, scope.OrganizationID, scope.SiteID)
	if err != nil {
		return nil, fmt.Errorf("query cloud instances: %w", err)
	}
	defer rows.Close()
	instances := make([]cloudinventory.Instance, 0)
	for rows.Next() {
		var instance cloudinventory.Instance
		var metadata []byte
		if err := rows.Scan(
			&instance.OrganizationID, &instance.SiteID, &instance.AccountID, &instance.AccountName, &instance.Provider, &instance.ProviderInstanceID, &instance.Name,
			&instance.Region, &instance.Zone, &instance.State, &instance.OSFamily, &instance.PrivateIPs, &instance.PublicIPs,
			&instance.AgentIDHint, &metadata, &instance.AgentID, &instance.CandidateAgentID, &instance.MatchStatus,
			&instance.MatchReason, &instance.DiscoveredAt,
		); err != nil {
			return nil, fmt.Errorf("scan cloud instance: %w", err)
		}
		if err := json.Unmarshal(metadata, &instance.Metadata); err != nil {
			return nil, fmt.Errorf("decode cloud instance metadata: %w", err)
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud instances: %w", err)
	}
	return instances, nil
}

package authorization

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type Permission string

const (
	PermissionManageUsers           Permission = "manage_users"
	PermissionManageOrgSecurity     Permission = "manage_org_security"
	PermissionViewSite              Permission = "view_site"
	PermissionManageAgents          Permission = "manage_agents"
	PermissionManageAlerts          Permission = "manage_alerts"
	PermissionManageCloudAccounts   Permission = "manage_cloud_accounts"
	PermissionCreateJobs            Permission = "create_jobs"
	PermissionAcknowledgeIncidents  Permission = "acknowledge_incidents"
	PermissionViewActivity          Permission = "view_activity"
	PermissionViewAuditEvents       Permission = "view_audit_events"
	PermissionManageServiceAccounts Permission = "manage_service_accounts"
)

type SiteRole string

const (
	SiteRoleAdmin    SiteRole = "site-admin"
	SiteRoleOperator SiteRole = "operator"
	SiteRoleViewer   SiteRole = "viewer"
)

type SiteMembership struct {
	ID             string
	UserID         string
	OrganizationID string
	SiteID         string
	Role           SiteRole
	CreatedAt      time.Time
}

var ErrMembershipNotFound = errors.New("site membership not found")

// PlatformAdminChecker reports whether userID holds the org-scoped
// platform-admin identity role. internal/identity provides the production
// implementation (Service.IsPlatformAdmin); this package stays decoupled
// from identity's concrete types, matching the jobs.HostScopeChecker /
// sessions.UserActiveChecker convention already used in this codebase.
type PlatformAdminChecker func(context.Context, string) (bool, error)

type Store interface {
	AssignRole(ctx context.Context, membership SiteMembership) error
	RevokeRole(ctx context.Context, userID, siteID string) error
	RoleForUserAtSite(ctx context.Context, userID, siteID string) (SiteRole, error)
	MembershipsForUser(ctx context.Context, userID string) ([]SiteMembership, error)
}

// orgScopedPermissions are platform-admin only, regardless of site
// membership — they concern the whole organization, not a single site.
var orgScopedPermissions = map[Permission]bool{
	PermissionManageUsers:       true,
	PermissionManageOrgSecurity: true,
}

// siteRolePermissions maps each site-scoped permission to the site roles
// that satisfy it. This is the "Roller" table from the design spec,
// transcribed exactly. A permission absent here (and not in
// orgScopedPermissions) is deny-by-default for every non-platform-admin
// caller.
var siteRolePermissions = map[Permission]map[SiteRole]bool{
	PermissionViewSite:              {SiteRoleAdmin: true, SiteRoleOperator: true, SiteRoleViewer: true},
	PermissionManageAgents:          {SiteRoleAdmin: true},
	PermissionManageAlerts:          {SiteRoleAdmin: true},
	PermissionManageCloudAccounts:   {SiteRoleAdmin: true},
	PermissionCreateJobs:            {SiteRoleAdmin: true, SiteRoleOperator: true},
	PermissionAcknowledgeIncidents:  {SiteRoleAdmin: true, SiteRoleOperator: true},
	PermissionViewActivity:          {SiteRoleAdmin: true, SiteRoleOperator: true, SiteRoleViewer: true},
	PermissionViewAuditEvents:       {SiteRoleAdmin: true, SiteRoleOperator: true},
	PermissionManageServiceAccounts: {SiteRoleAdmin: true},
}

type Service struct {
	store         Store
	platformAdmin PlatformAdminChecker
}

func NewService(store Store, platformAdmin PlatformAdminChecker) (*Service, error) {
	if platformAdmin == nil {
		return nil, errors.New("authorization platform-admin checker is required")
	}
	return &Service{store: store, platformAdmin: platformAdmin}, nil
}

// PermissionAllowsRole reports whether role alone (without any
// platform-admin bypass) satisfies permission. Org-scoped permissions
// always return false — they require platform-admin, an identity-level
// concept unrelated to SiteRole. internal/serviceaccounts uses this
// directly (a service account has no platform-admin bypass at all, so it
// never needs the full Service.Can/store round trip).
func PermissionAllowsRole(permission Permission, role SiteRole) bool {
	if orgScopedPermissions[permission] {
		return false
	}
	allowedRoles, known := siteRolePermissions[permission]
	if !known {
		return false
	}
	return allowedRoles[role]
}

// Can evaluates the fixed RBAC matrix deny-by-default: platform-admin
// satisfies every permission everywhere; a site-scoped permission requires
// a site membership at siteID whose role is in the permission's
// allowed-role set (org-scoped permissions are platform-admin only, per
// PermissionAllowsRole). Any unmodeled permission, or any lookup error
// treated as "no access", denies.
func (service *Service) Can(ctx context.Context, userID string, permission Permission, siteID string) (bool, error) {
	isPlatformAdmin, err := service.platformAdmin(ctx, userID)
	if err != nil {
		return false, err
	}
	if isPlatformAdmin {
		return true, nil
	}
	role, err := service.store.RoleForUserAtSite(ctx, userID, siteID)
	if errors.Is(err, ErrMembershipNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return PermissionAllowsRole(permission, role), nil
}

func (service *Service) AssignRole(ctx context.Context, userID, organizationID, siteID string, role SiteRole) error {
	id, err := newID()
	if err != nil {
		return err
	}
	return service.store.AssignRole(ctx, SiteMembership{
		ID: id, UserID: userID, OrganizationID: organizationID, SiteID: siteID, Role: role, CreatedAt: time.Now().UTC(),
	})
}

func (service *Service) RevokeRole(ctx context.Context, userID, siteID string) error {
	return service.store.RevokeRole(ctx, userID, siteID)
}

func (service *Service) RoleForUserAtSite(ctx context.Context, userID, siteID string) (SiteRole, error) {
	return service.store.RoleForUserAtSite(ctx, userID, siteID)
}

func (service *Service) MembershipsForUser(ctx context.Context, userID string) ([]SiteMembership, error) {
	return service.store.MembershipsForUser(ctx, userID)
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

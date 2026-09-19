package tenancy

import (
	"errors"
	"strings"
	"time"
)

const (
	DefaultOrganizationID = "org_default"
	DefaultSiteID         = "site_default"
)

var (
	ErrInvalidScope = errors.New("invalid tenancy scope")
	ErrNotFound     = errors.New("tenancy resource not found")
)

type Scope struct {
	OrganizationID string `json:"organization_id"`
	SiteID         string `json:"site_id"`
}

func DefaultScope() Scope {
	return Scope{OrganizationID: DefaultOrganizationID, SiteID: DefaultSiteID}
}

func (scope Scope) Validate() error {
	if strings.TrimSpace(scope.OrganizationID) == "" || strings.TrimSpace(scope.SiteID) == "" ||
		len(scope.OrganizationID) > 128 || len(scope.SiteID) > 128 ||
		strings.ContainsRune(scope.OrganizationID, '\x00') || strings.ContainsRune(scope.SiteID, '\x00') {
		return ErrInvalidScope
	}
	return nil
}

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Site struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	CreatedAt      time.Time `json:"created_at"`
}

type Agent struct {
	ID             string `json:"agent_id"`
	OrganizationID string `json:"organization_id"`
	SiteID         string `json:"site_id"`
}

func (agent Agent) Scope() Scope {
	return Scope{OrganizationID: agent.OrganizationID, SiteID: agent.SiteID}
}

func (agent Agent) Validate() error {
	if strings.TrimSpace(agent.ID) == "" || len(agent.ID) > 128 || strings.ContainsRune(agent.ID, '\x00') {
		return ErrInvalidScope
	}
	return agent.Scope().Validate()
}

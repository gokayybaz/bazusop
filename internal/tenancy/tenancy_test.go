package tenancy

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultScopeIsValid(t *testing.T) {
	scope := DefaultScope()
	if scope.OrganizationID != DefaultOrganizationID || scope.SiteID != DefaultSiteID {
		t.Fatalf("unexpected default scope: %#v", scope)
	}
	if err := scope.Validate(); err != nil {
		t.Fatalf("default scope must be valid: %v", err)
	}
}

func TestScopeRejectsMissingOrOversizedIdentifiers(t *testing.T) {
	for _, scope := range []Scope{
		{},
		{OrganizationID: DefaultOrganizationID},
		{OrganizationID: strings.Repeat("x", 129), SiteID: DefaultSiteID},
		{OrganizationID: DefaultOrganizationID, SiteID: strings.Repeat("x", 129)},
	} {
		if !errors.Is(scope.Validate(), ErrInvalidScope) {
			t.Fatalf("expected invalid scope for %#v", scope)
		}
	}
}

func TestAgentRequiresIDAndValidScope(t *testing.T) {
	agent := Agent{ID: "agent-1", OrganizationID: DefaultOrganizationID, SiteID: DefaultSiteID}
	if err := agent.Validate(); err != nil {
		t.Fatalf("valid agent rejected: %v", err)
	}

	for _, agent := range []Agent{
		{ID: "", OrganizationID: DefaultOrganizationID, SiteID: DefaultSiteID},
		{ID: "   ", OrganizationID: DefaultOrganizationID, SiteID: DefaultSiteID},
		{ID: strings.Repeat("x", 129), OrganizationID: DefaultOrganizationID, SiteID: DefaultSiteID},
		{ID: "agent-1", SiteID: DefaultSiteID},
		{ID: "agent-1", OrganizationID: DefaultOrganizationID},
	} {
		if !errors.Is(agent.Validate(), ErrInvalidScope) {
			t.Fatalf("expected invalid agent for %#v", agent)
		}
	}
}

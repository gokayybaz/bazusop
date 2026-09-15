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

func TestScopeRejectsEmbeddedNUL(t *testing.T) {
	for _, test := range []struct {
		name  string
		scope Scope
	}{
		{name: "organization", scope: Scope{OrganizationID: "org\x00other", SiteID: "site"}},
		{name: "site", scope: Scope{OrganizationID: "org", SiteID: "site\x00other"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.scope.Validate(); !errors.Is(err, ErrInvalidScope) {
				t.Fatalf("scope containing NUL accepted: %#v, err=%v", test.scope, err)
			}
		})
	}
}

func TestAgentRejectsEmbeddedNUL(t *testing.T) {
	for _, test := range []struct {
		name  string
		agent Agent
	}{
		{name: "agent", agent: Agent{ID: "agent\x00other", OrganizationID: "org", SiteID: "site"}},
		{name: "organization", agent: Agent{ID: "agent", OrganizationID: "org\x00other", SiteID: "site"}},
		{name: "site", agent: Agent{ID: "agent", OrganizationID: "org", SiteID: "site\x00other"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.agent.Validate(); !errors.Is(err, ErrInvalidScope) {
				t.Fatalf("agent containing NUL accepted: %#v, err=%v", test.agent, err)
			}
		})
	}
}

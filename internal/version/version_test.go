package version_test

import (
	"testing"

	"github.com/gokayybaz/bazusop/internal/version"
)

func TestCurrentReturnsLinkerBuildIdentity(t *testing.T) {
	originalVersion, originalCommit, originalBuildDate := version.Version, version.Commit, version.BuildDate
	t.Cleanup(func() {
		version.Version, version.Commit, version.BuildDate = originalVersion, originalCommit, originalBuildDate
	})
	version.Version = "0.3.0"
	version.Commit = "abc123def456"
	version.BuildDate = "2026-09-12T09:30:00Z"

	identity := version.Current()
	if identity.Version != "0.3.0" || identity.Commit != "abc123def456" || identity.BuildDate != "2026-09-12T09:30:00Z" {
		t.Fatalf("unexpected build identity: %#v", identity)
	}
	if got := identity.String(); got != "bazUSOP hub 0.3.0 (abc123def456, 2026-09-12T09:30:00Z)" {
		t.Fatalf("unexpected version output: %q", got)
	}
}

func TestDevelopmentIdentityIsExplicit(t *testing.T) {
	identity := version.Info{}
	if got := identity.String(); got != "bazUSOP hub dev (unknown, unknown)" {
		t.Fatalf("unexpected fallback version output: %q", got)
	}
}

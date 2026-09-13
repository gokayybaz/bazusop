package deployment_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerAndComposeBaseline(t *testing.T) {
	t.Parallel()

	dockerfile := readProjectFile(t, "Dockerfile")
	for _, required := range []string{"AS web", "AS backend", "USER 65532:65532", "HEALTHCHECK"} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile must contain %q", required)
		}
	}

	compose := readProjectFile(t, "compose.yaml")
	for _, required := range []string{"postgres:", "healthcheck:", "condition: service_healthy", "DATABASE_URL", "BAZUSOP_ADMIN_TOKEN", "BAZUSOP_TELEMETRY_RETENTION_DAYS", "BAZUSOP_LOG_RETENTION_DAYS"} {
		if !strings.Contains(compose, required) {
			t.Errorf("compose.yaml must contain %q", required)
		}
	}
}

func TestCIExercisesPostgresIntegration(t *testing.T) {
	t.Parallel()
	workflow := readProjectFile(t, ".github/workflows/ci.yml")
	for _, required := range []string{"services:", "postgres:18", "BAZUSOP_TEST_DATABASE_URL", "pg_isready"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("CI PostgreSQL integration must contain %q", required)
		}
	}
}

func TestReleaseSmokeUsesAnIsolatedComposeStack(t *testing.T) {
	t.Parallel()

	makefile := readProjectFile(t, "Makefile")
	if !strings.Contains(makefile, "smoke-compose:") || !strings.Contains(makefile, "scripts/smoke-compose.sh") {
		t.Error("Makefile must expose the isolated Compose smoke test")
	}

	script := readProjectFile(t, "scripts/smoke-compose.sh")
	for _, required := range []string{
		"set -euo pipefail",
		"docker compose",
		"--project-name",
		"up --detach --build --wait",
		"/api/v1/health",
		"/api/v1/system/configuration",
		"BAZUSOP_TEST_DATABASE_URL",
		"go test -count=1 ./internal/storage/postgres -run TestPostgres",
		"down --volumes",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("Compose smoke test must contain %q", required)
		}
	}

	workflow := readProjectFile(t, ".github/workflows/ci.yml")
	if !strings.Contains(workflow, "make smoke-compose") {
		t.Error("CI must execute the isolated Compose smoke test")
	}
}

func TestHelmChartDefinesScalableSafeWorkload(t *testing.T) {
	t.Parallel()

	readProjectFile(t, "deploy/helm/bazusop/Chart.yaml")
	deployment := readProjectFile(t, "deploy/helm/bazusop/templates/deployment.yaml")
	for _, required := range []string{"replicaCount", "readinessProbe", "livenessProbe", "topologySpreadConstraints", "secretKeyRef", "BAZUSOP_ADMIN_TOKEN", "BAZUSOP_TELEMETRY_RETENTION_DAYS", "BAZUSOP_LOG_RETENTION_DAYS"} {
		if !strings.Contains(deployment, required) {
			t.Errorf("deployment template must contain %q", required)
		}
	}
	values := readProjectFile(t, "deploy/helm/bazusop/values.yaml")
	if !strings.Contains(values, "runAsNonRoot: true") {
		t.Error("chart values must require a non-root pod security context")
	}
	if !strings.Contains(values, "adminTokenKey: admin-token") {
		t.Error("chart values must expose the admin token secret key")
	}
	if !strings.Contains(values, "telemetryRetentionDays: 30") || !strings.Contains(values, "logRetentionDays: 14") {
		t.Error("chart values must expose retention defaults")
	}

	hpa := readProjectFile(t, "deploy/helm/bazusop/templates/hpa.yaml")
	if !strings.Contains(hpa, "autoscaling/v2") || !strings.Contains(hpa, "maxReplicas") {
		t.Error("HPA must use autoscaling/v2 and define maxReplicas")
	}
	pdb := readProjectFile(t, "deploy/helm/bazusop/templates/pdb.yaml")
	if !strings.Contains(pdb, "policy/v1") || !strings.Contains(pdb, "maxUnavailable") {
		t.Error("PDB must use policy/v1 and define maxUnavailable")
	}
}

func TestReleaseBuildProducesVersionedCrossPlatformArtifacts(t *testing.T) {
	t.Parallel()

	makefile := readProjectFile(t, "Makefile")
	for _, required := range []string{"LDFLAGS", "release:"} {
		if !strings.Contains(makefile, required) {
			t.Errorf("Makefile release build must contain %q", required)
		}
	}

	releaseScript := readProjectFile(t, "scripts/build-release.sh")
	for _, required := range []string{"linux amd64", "linux arm64", "windows amd64", "bazusop-${component}", "bazusop-agent_*", "checksums.txt", "sha256", "github.com/gokayybaz/bazusop/internal/version", ".Version=$version", ".Commit=$commit", ".BuildDate=$build_date"} {
		if !strings.Contains(releaseScript, required) {
			t.Errorf("release script must contain %q", required)
		}
	}

	workflow := readProjectFile(t, ".github/workflows/release.yml")
	for _, required := range []string{"tags:", "v*", "make test", "build-release.sh", "gh release create"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release workflow must contain %q", required)
		}
	}
}

func TestNativePackagesAndSignedReleaseAreDefined(t *testing.T) {
	t.Parallel()

	goModule := readProjectFile(t, "go.mod")
	if !strings.Contains(goModule, "go 1.26.4") {
		t.Error("release toolchain must satisfy nFPM's minimum Go 1.26.4 requirement")
	}

	nfpm := readProjectFile(t, "packaging/nfpm.yaml")
	for _, required := range []string{"name: bazusop-hub", "${VERSION}", "${ARCH}", "bazusop-hub.service", "upgrade-hub.sh"} {
		if !strings.Contains(nfpm, required) {
			t.Errorf("nFPM configuration must contain %q", required)
		}
	}

	wix := readProjectFile(t, "packaging/windows/Package.wxs")
	for _, required := range []string{"http://wixtoolset.org/schemas/v4/wxs", "MajorUpgrade", "bazusop-hub.exe", "ProgramFiles64Folder"} {
		if !strings.Contains(wix, required) {
			t.Errorf("WiX package must contain %q", required)
		}
	}

	workflow := readProjectFile(t, ".github/workflows/release.yml")
	for _, required := range []string{"nfpm@v2.47.0", "wix --version 4.0.6", "cosign-installer@v4.1.2", "cosign sign-blob", "cosign sign --yes", "docker/build-push-action", "id-token: write", "packages: write"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("signed package workflow must contain %q", required)
		}
	}
}

func TestAgentNativePackagesInstallManagedServices(t *testing.T) {
	t.Parallel()

	nfpm := readProjectFile(t, "packaging/nfpm-agent.yaml")
	for _, required := range []string{"name: bazusop-agent", "bazusop-agent.service", "/var/lib/bazusop-agent", "postinstall-agent.sh", "preremove-agent.sh"} {
		if !strings.Contains(nfpm, required) {
			t.Errorf("agent nFPM configuration must contain %q", required)
		}
	}

	unit := readProjectFile(t, "packaging/linux/bazusop-agent.service")
	for _, required := range []string{"EnvironmentFile=/etc/bazusop/agent.env", "StateDirectory=bazusop-agent", "StateDirectoryMode=0700", "ExecStart=/usr/bin/bazusop-agent", "WantedBy=multi-user.target"} {
		if !strings.Contains(unit, required) {
			t.Errorf("agent systemd unit must contain %q", required)
		}
	}

	wix := readProjectFile(t, "packaging/windows/AgentPackage.wxs")
	for _, required := range []string{"bazusop-agent.exe", "ServiceInstall", "ServiceControl", "CommonAppDataFolder", "BAZUSOP_AGENT_STATE_DIR", "MajorUpgrade"} {
		if !strings.Contains(wix, required) {
			t.Errorf("agent WiX package must contain %q", required)
		}
	}

	windowsRuntime := readProjectFile(t, "cmd/bazusop-agent/run_windows.go")
	for _, required := range []string{"svc.IsWindowsService", "svc.Run", "svc.AcceptStop", "svc.AcceptShutdown", "svc.StopPending"} {
		if !strings.Contains(windowsRuntime, required) {
			t.Errorf("Windows agent runtime must contain %q", required)
		}
	}
	windowsPermissions := readProjectFile(t, "internal/agent/identity_permissions_windows.go")
	for _, required := range []string{"WinBuiltinAdministratorsSid", "WinLocalSystemSid", "PROTECTED_DACL_SECURITY_INFORMATION", "SUB_CONTAINERS_AND_OBJECTS_INHERIT"} {
		if !strings.Contains(windowsPermissions, required) {
			t.Errorf("Windows identity protection must contain %q", required)
		}
	}

	nativeScript := readProjectFile(t, "scripts/build-native-packages.sh")
	for _, required := range []string{"for component in hub agent", "nfpm-agent.yaml", "bazusop-${component}_${version}_linux_${architecture}"} {
		if !strings.Contains(nativeScript, required) {
			t.Errorf("native package build must contain %q", required)
		}
	}

	workflow := readProjectFile(t, ".github/workflows/release.yml")
	for _, required := range []string{"AgentPackage.wxs", "bazusop-agent_${version}_windows_amd64.msi", "bazusop-agent.exe"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release workflow must build agent MSI with %q", required)
		}
	}
	ciWorkflow := readProjectFile(t, ".github/workflows/ci.yml")
	for _, required := range []string{"AgentPackage.wxs", "bazusop-agent_0.0.0_windows_amd64.msi"} {
		if !strings.Contains(ciWorkflow, required) {
			t.Errorf("CI must compile the agent MSI contract with %q", required)
		}
	}
}

func readProjectFile(t *testing.T, name string) string {
	t.Helper()

	contents, err := os.ReadFile(filepath.Join("..", "..", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}

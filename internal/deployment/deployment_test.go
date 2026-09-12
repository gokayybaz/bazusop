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
	for _, required := range []string{"nfpm@v2.47.0", "wix --version 4.0.6", "cosign-installer@v4", "cosign sign-blob", "cosign sign --yes", "docker/build-push-action", "id-token: write", "packages: write"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("signed package workflow must contain %q", required)
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

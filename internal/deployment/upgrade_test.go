package deployment_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeReplacesHealthyCandidateAndKeepsPreviousVersion(t *testing.T) {
	t.Parallel()
	testDirectory := t.TempDir()
	target := writeExecutable(t, testDirectory, "bazusop-hub", fakeHub("0.2.0"))
	candidate := writeExecutable(t, testDirectory, "candidate", fakeHub("0.3.0"))
	health := filepath.Join(testDirectory, "health")
	if err := os.WriteFile(health, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeSystemctl(t, testDirectory)

	runUpgrade(t, testDirectory, candidate, target, "0.3.0", checksum(t, candidate), "file://"+health, false)

	assertContains(t, target, "0.3.0")
	assertContains(t, target+".previous", "0.2.0")
}

func TestUpgradeRollsBackWhenHealthCheckFails(t *testing.T) {
	t.Parallel()
	testDirectory := t.TempDir()
	target := writeExecutable(t, testDirectory, "bazusop-hub", fakeHub("0.2.0"))
	candidate := writeExecutable(t, testDirectory, "candidate", fakeHub("0.3.0"))
	fakeSystemctl(t, testDirectory)

	runUpgrade(t, testDirectory, candidate, target, "0.3.0", checksum(t, candidate), "file://"+filepath.Join(testDirectory, "missing"), true)

	assertContains(t, target, "0.2.0")
}

func TestUpgradeRejectsInvalidChecksumBeforeReplacingTarget(t *testing.T) {
	t.Parallel()
	testDirectory := t.TempDir()
	target := writeExecutable(t, testDirectory, "bazusop-hub", fakeHub("0.2.0"))
	candidate := writeExecutable(t, testDirectory, "candidate", fakeHub("0.3.0"))
	fakeSystemctl(t, testDirectory)

	runUpgrade(t, testDirectory, candidate, target, "0.3.0", strings.Repeat("0", 64), "file:///unused", true)

	assertContains(t, target, "0.2.0")
	if _, err := os.Stat(target + ".previous"); !os.IsNotExist(err) {
		t.Fatalf("preflight failure must not create a backup, got %v", err)
	}
}

func runUpgrade(t *testing.T, testDirectory, candidate, target, version, sum, healthURL string, wantFailure bool) {
	t.Helper()
	command := exec.Command("bash", filepath.Join("..", "..", "scripts", "upgrade-hub.sh"),
		"--candidate", candidate,
		"--target", target,
		"--version", version,
		"--sha256", sum,
		"--health-url", healthURL,
		"--service", "bazusop-hub",
	)
	command.Env = append(os.Environ(),
		"PATH="+testDirectory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"BAZUSOP_UPGRADE_ATTEMPTS=1",
		"BAZUSOP_UPGRADE_DELAY=0",
	)
	output, err := command.CombinedOutput()
	if wantFailure && err == nil {
		t.Fatalf("expected upgrade failure, got success: %s", output)
	}
	if !wantFailure && err != nil {
		t.Fatalf("upgrade failed: %v: %s", err, output)
	}
}

func fakeHub(version string) string {
	return fmt.Sprintf("#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'bazUSOP hub %s (test, test)'; fi\n", version)
}

func fakeSystemctl(t *testing.T, directory string) {
	t.Helper()
	writeExecutable(t, directory, "systemctl", "#!/bin/sh\nexit 0\n")
}

func writeExecutable(t *testing.T, directory, name, contents string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func checksum(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(contents))
}

func assertContains(t *testing.T, path, expected string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), expected) {
		t.Fatalf("%s does not contain %q", path, expected)
	}
}

//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// requireTool skips the test if a required external binary is not on PATH.
func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("e2e requires %q on PATH: %v", name, err)
	}
}

// repoRoot returns the repository root (two levels up from this test file).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// randToken returns a short random hex token for unique sandbox names.
func randToken() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// cleanupStrayMachines removes any leftover ppp-e2e-* Podman Machines so a
// single-active-VM host is not blocked by a previous aborted run (ADR-0007),
// and kills any orphaned ppp-spawned mitmdump proxy holding the WG port pool
// (which would otherwise fail the next daemon start with "address already in
// use"). Both are cleanup of aborted prior runs, not product behavior.
func cleanupStrayMachines(t *testing.T, dataDir string) {
	t.Helper()
	out, err := exec.Command("podman", "machine", "list", "--format", "{{.Name}}").Output()
	if err == nil {
		for _, name := range strings.Fields(string(out)) {
			name = strings.TrimSuffix(name, "*") // strip the default-machine marker
			if strings.HasPrefix(name, "ppp-e2e-") {
				_ = exec.Command("podman", "machine", "stop", name).Run()
				_ = exec.Command("podman", "machine", "rm", "-f", name).Run()
			}
		}
	}
	// Kill only THIS test's orphaned ppp mitmdump (identified by its per-test
	// PPP_DATA in the argv), not a developer's real daemon. dataDir is the
	// test's PPP_DATA.
	killOrphanProxy(dataDir)
}

// killOrphanProxy kills a mitmdump whose argv references the given (per-test)
// data dir — an orphan from an aborted run — without touching any other
// mitmdump on the host.
func killOrphanProxy(dataDir string) {
	if dataDir == "" {
		return
	}
	out, err := exec.Command("pgrep", "-f", "mitmdump --mode wireguard").Output()
	if err != nil {
		return
	}
	for _, pidStr := range strings.Fields(string(out)) {
		argv, aerr := exec.Command("ps", "-o", "command=", "-p", pidStr).Output()
		if aerr != nil {
			continue
		}
		if strings.Contains(string(argv), dataDir) {
			_ = exec.Command("kill", pidStr).Run()
		}
	}
}

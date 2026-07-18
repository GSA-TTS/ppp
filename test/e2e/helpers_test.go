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
func cleanupStrayMachines(t *testing.T) {
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
	// Kill any orphaned ppp mitmdump (a detached proxy from an aborted run).
	_ = exec.Command("pkill", "-f", "mitmdump --mode wireguard").Run()
}

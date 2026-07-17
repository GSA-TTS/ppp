package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GSA-TTS/ppp/internal/sandbox"
)

func TestDaemonStatusFakeSupervisor(t *testing.T) {
	testEnv(t)
	d, h := newHarness()

	// Not running.
	h.supervisor.status = ProxyStatus{Running: false}
	out, err := run(t, d, "", "daemon", "status")
	if err != nil {
		t.Fatalf("daemon status: %v", err)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("expected not-running status, got %q", out)
	}

	// Running with a pid.
	h.supervisor.status = ProxyStatus{Running: true, PID: 4242}
	out, err = run(t, d, "", "daemon", "status")
	if err != nil {
		t.Fatalf("daemon status: %v", err)
	}
	if !strings.Contains(out, "4242") {
		t.Errorf("expected pid in status, got %q", out)
	}
}

func TestDaemonStartStopThroughSupervisor(t *testing.T) {
	testEnv(t)
	d, h := newHarness()
	if _, err := run(t, d, "", "daemon", "start"); err != nil {
		t.Fatalf("daemon start: %v", err)
	}
	if _, err := run(t, d, "", "daemon", "stop"); err != nil {
		t.Fatalf("daemon stop: %v", err)
	}
	if h.supervisor.started != 1 || h.supervisor.stopped != 1 {
		t.Errorf("expected 1 start and 1 stop, got %d/%d", h.supervisor.started, h.supervisor.stopped)
	}
}

// TestHostSupervisorStatusReadsPIDFile exercises the real hostSupervisor.Status
// against a temp $PPP_DATA proxy.pid file (no process spawn).
func TestHostSupervisorStatusReadsPIDFile(t *testing.T) {
	testEnv(t)
	sup := newHostSupervisor()

	// Absent pid file => not running.
	st, err := sup.Status()
	if err != nil {
		t.Fatalf("status (absent): %v", err)
	}
	if st.Running {
		t.Error("expected not-running when proxy.pid is absent")
	}

	// Write a pid file => running.
	dataDir, _ := sandbox.ResolveDataDir()
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "proxy.pid"), []byte("1234\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	st, err = sup.Status()
	if err != nil {
		t.Fatalf("status (present): %v", err)
	}
	if !st.Running || st.PID != 1234 {
		t.Errorf("expected running pid 1234, got %+v", st)
	}
}

func TestPortsLists(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()
	// create allocates a port.
	if _, err := run(t, d, "", "create", "opencode", "/tmp/ws", "--name", "ppp-red-bird"); err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := run(t, d, "", "ports")
	if err != nil {
		t.Fatalf("ports: %v", err)
	}
	if !strings.Contains(out, "ppp-red-bird") || !strings.Contains(out, "51820") {
		t.Errorf("expected port allocation listed, got %q", out)
	}
}

func TestPortsEmpty(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()
	out, err := run(t, d, "", "ports")
	if err != nil {
		t.Fatalf("ports empty: %v", err)
	}
	if !strings.Contains(out, "no port allocations") {
		t.Errorf("expected friendly empty message, got %q", out)
	}
}

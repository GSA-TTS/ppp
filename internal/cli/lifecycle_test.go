package cli

import (
	"strings"
	"testing"

	"github.com/GSA-TTS/ppp/internal/podman"
	"github.com/GSA-TTS/ppp/internal/sandbox"
)

func TestCreateStateOnly(t *testing.T) {
	testEnv(t)
	d, h := newHarness()

	out, err := run(t, d, "", "create", "opencode", "/tmp/ws", "--name", "ppp-red-bird")
	if err != nil {
		t.Fatalf("create: %v (out=%q)", err, out)
	}

	box, err := sandbox.Load("ppp-red-bird")
	if err != nil {
		t.Fatalf("loading persisted sandbox: %v", err)
	}
	if box.Agent != "opencode" || box.Workspace != "/tmp/ws" {
		t.Errorf("unexpected persisted sandbox: %+v", box)
	}
	if box.Status != sandbox.StatusCreated {
		t.Errorf("expected status created, got %q", box.Status)
	}
	if box.Port < 51820 {
		t.Errorf("expected allocated port >= 51820, got %d", box.Port)
	}
	if box.InnerIP == "" {
		t.Errorf("expected an inner IP, got empty")
	}

	// The runner recorded an init call (create does NOT start).
	assertRecorded(t, h.runner, "init")
	if recorded(h.runner, "start") {
		t.Error("create must not start the machine")
	}
}

func TestRunStateOnlyInitAndStart(t *testing.T) {
	testEnv(t)
	d, h := newHarness()

	// With no daemon-captured client configs, run creates+starts the machine
	// and surfaces the not-provisioned hint (see hostprovision_test.go for the
	// provisioned path).
	out, err := run(t, d, "", "run", "opencode", "/tmp/ws", "--name", "ppp-swift-otter")
	if err != nil {
		t.Fatalf("run: %v (out=%q)", err, out)
	}
	box, err := sandbox.Load("ppp-swift-otter")
	if err != nil {
		t.Fatalf("loading persisted sandbox: %v", err)
	}
	if box.Status != sandbox.StatusRunning {
		t.Errorf("expected status running, got %q", box.Status)
	}
	assertRecorded(t, h.runner, "init")
	assertRecorded(t, h.runner, "start")
}

func TestCreateGeneratesName(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()
	out, err := run(t, d, "", "create", "opencode", "/tmp/ws", "-q")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	name := strings.TrimSpace(out)
	if err := validateSandboxName(name); err != nil {
		t.Errorf("generated name %q is invalid: %v", name, err)
	}
}

func TestCreateRejectsUnsupportedAgent(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()
	if _, err := run(t, d, "", "create", "claude", "/tmp/ws"); err == nil {
		t.Error("expected unsupported agent to error")
	}
}

func TestStopTransitionsAndCallsRunner(t *testing.T) {
	testEnv(t)
	d, h := newHarness()
	box := sandbox.Sandbox{
		Name: "ppp-red-bird", Agent: "opencode", Workspace: "/tmp/ws",
		Status: sandbox.StatusRunning, Port: 51820, InnerIP: "10.0.0.1",
	}
	if err := box.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := run(t, d, "", "stop", "ppp-red-bird"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	reloaded, _ := sandbox.Load("ppp-red-bird")
	if reloaded.Status != sandbox.StatusStopped {
		t.Errorf("expected stopped, got %q", reloaded.Status)
	}
	assertRecorded(t, h.runner, "stop")
}

func TestRmFreesPortAndRemovesDir(t *testing.T) {
	testEnv(t)
	d, h := newHarness()
	// create allocates a port and persists state.
	if _, err := run(t, d, "", "create", "opencode", "/tmp/ws", "--name", "ppp-red-bird"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := run(t, d, "", "rm", "ppp-red-bird"); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if _, err := sandbox.Load("ppp-red-bird"); err == nil {
		t.Error("expected sandbox state removed after rm")
	}
	assertRecorded(t, h.runner, "rm")
}

func recorded(f *podman.Fake, op string) bool {
	for _, c := range f.Calls {
		if c.Op == op {
			return true
		}
	}
	return false
}

func assertRecorded(t *testing.T, f *podman.Fake, op string) {
	t.Helper()
	if !recorded(f, op) {
		t.Errorf("expected runner to record a %q call; calls=%v", op, f.Calls)
	}
}

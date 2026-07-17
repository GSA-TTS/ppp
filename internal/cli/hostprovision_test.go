package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/GSA-TTS/ppp/internal/proxy/capture"
	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// writeClientConfs writes a minimal client-confs.json for the given port into
// $PPP_DATA/wg so provisionAndRun can render a wg0.conf.
func writeClientConfs(t *testing.T, port int) {
	t.Helper()
	dataDir, err := sandbox.ResolveDataDir()
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}
	wgDir := filepath.Join(dataDir, "wg")
	if err := os.MkdirAll(wgDir, 0o700); err != nil {
		t.Fatalf("mkdir wg: %v", err)
	}
	byPort := map[string]capture.Config{
		strconv.Itoa(port): {
			ListenPort:   port,
			Address:      "10.0.0.1/32",
			PublicKey:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=",
			EndpointHost: "172.17.0.3",
			AllowedIPs:   "0.0.0.0/0",
		},
	}
	data, _ := json.MarshalIndent(byPort, "", "  ")
	if err := os.WriteFile(filepath.Join(wgDir, "client-confs.json"), data, 0o600); err != nil {
		t.Fatalf("write client-confs: %v", err)
	}
}

// TestRunNoDaemonSurfacesHint verifies that `run` creates+starts the sandbox
// but, with no captured client configs, surfaces the daemon-not-ready hint
// instead of hard-failing.
func TestRunNoDaemonSurfacesHint(t *testing.T) {
	testEnv(t)
	d, h := newHarness()

	out, err := run(t, d, "", "run", "opencode", "/tmp/ws", "--name", "ppp-swift-otter")
	if err != nil {
		t.Fatalf("run: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "not provisioned") {
		t.Errorf("expected daemon-not-ready hint, got %q", out)
	}
	// The machine was still init+started through the runner.
	assertRecorded(t, h.runner, "init")
	assertRecorded(t, h.runner, "start")
	box, err := sandbox.Load("ppp-swift-otter")
	if err != nil || box.Status != sandbox.StatusRunning {
		t.Fatalf("sandbox not persisted running: %+v err=%v", box, err)
	}
}

// TestRunProvisionsWhenDaemonReady verifies that with captured client configs
// present, `run` renders wg0.conf, copies assets, runs provision, and launches
// the agent — all through the fake PodmanRunner (no real VM).
func TestRunProvisionsWhenDaemonReady(t *testing.T) {
	testEnv(t)
	d, h := newHarness()
	writeClientConfs(t, 51820)

	out, err := run(t, d, "", "run", "opencode", "/tmp/ws", "--name", "ppp-red-bird")
	if err != nil {
		t.Fatalf("run: %v (out=%q)", err, out)
	}

	// wg0.conf was rendered into the sandbox dir with the rewrite applied.
	sbDir, _ := sandbox.SandboxDir("ppp-red-bird")
	wg0, err := os.ReadFile(filepath.Join(sbDir, "wg0.conf"))
	if err != nil {
		t.Fatalf("wg0.conf not written: %v", err)
	}
	conf := string(wg0)
	for _, want := range []string{"Table = off", "Endpoint = 192.168.127.254:51820", "Address = 10.0.0.1/32"} {
		if !strings.Contains(conf, want) {
			t.Errorf("wg0.conf missing %q:\n%s", want, conf)
		}
	}
	if strings.Contains(conf, "DNS =") {
		t.Errorf("wg0.conf must omit DNS line:\n%s", conf)
	}

	// Assets were copied and provision + agent were run through the runner.
	assertRecorded(t, h.runner, "cp")
	assertRecorded(t, h.runner, "ssh")
}

func TestProvisionAndRunLaunchFalseSkipsAgent(t *testing.T) {
	testEnv(t)
	_, h := newHarness()
	writeClientConfs(t, 51820)
	box := sandbox.Sandbox{
		Name: "ppp-blue-fox", Agent: "opencode", Workspace: "/tmp/ws",
		Status: sandbox.StatusRunning, Port: 51820, InnerIP: "10.0.0.1",
	}
	if err := box.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := provisionAndRun(context.Background(), h.runner, box, false, nil); err != nil {
		t.Fatalf("provisionAndRun: %v", err)
	}
	// Exactly one ssh (provision), no agent run: count ssh calls.
	sshCount := 0
	for _, c := range h.runner.Calls {
		if c.Op == "ssh" {
			sshCount++
		}
	}
	if sshCount != 1 {
		t.Errorf("expected 1 ssh (provision only), got %d", sshCount)
	}
}

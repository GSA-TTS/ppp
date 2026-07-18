//go:build e2e

// Package e2e is the single no-mocks, host-only end-to-end validation of the
// whole isolation path (ticket T14 / #27, spec seam 8). It is excluded from the
// default `go test` run, CI, and the dev container by the `e2e` build tag: it
// requires a real macOS/libkrun (or Linux/qemu) host with `podman`, `mitmdump`
// (12.2.3), and network access, and it creates/destroys a throwaway Podman
// Machine.
//
// Run it explicitly on a host:
//
//	go test -tags e2e -timeout 20m ./test/e2e/ -run TestE2E -v
//
// It uses a DUMMY secret only (never a real credential) and tears the sandbox
// down (even on failure) via t.Cleanup.
package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// harness wires a built ppp binary to isolated $PPP_DATA/$PPP_CONFIG and a
// uniquely-named throwaway sandbox.
type harness struct {
	t       *testing.T
	bin     string
	env     []string
	dataDir string
	sandbox string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	requireTool(t, "podman")
	requireTool(t, "mitmdump")

	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "ppp")
	build := exec.Command("go", "build", "-o", bin, "./cmd/ppp")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building ppp: %v\n%s", err, out)
	}

	dataDir := t.TempDir()
	cfgDir := t.TempDir()
	env := append(os.Environ(),
		"PPP_DATA="+dataDir,
		"PPP_CONFIG="+cfgDir,
	)
	// Point the agent image at a small, definitely-pullable public image unless
	// the caller already set one: the e2e validates the ISOLATION path (tunnel,
	// interception, identity, policy, egress), not the agent itself, and the
	// real opencode image may not be published/reachable. `alpine` is tiny and
	// on Docker Hub. Override with PPP_OPENCODE_IMAGE to test a real agent image.
	if os.Getenv("PPP_OPENCODE_IMAGE") == "" {
		env = append(env, "PPP_OPENCODE_IMAGE=docker.io/library/alpine:3.20")
	}
	h := &harness{
		t:       t,
		bin:     bin,
		dataDir: dataDir,
		sandbox: "ppp-e2e-" + strings.ToLower(randToken()),
		env:     env,
	}
	return h
}

// ppp runs `ppp <args...>`, returning combined output. stdin is optional.
func (h *harness) ppp(stdin string, args ...string) (string, error) {
	h.t.Helper()
	cmd := exec.Command(h.bin, args...)
	cmd.Env = h.env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	h.t.Logf("$ ppp %s\n%s", strings.Join(args, " "), out.String())
	return out.String(), err
}

// mustPPP runs a command and fails the test on error.
func (h *harness) mustPPP(stdin string, args ...string) string {
	h.t.Helper()
	out, err := h.ppp(stdin, args...)
	if err != nil {
		h.t.Fatalf("ppp %s failed: %v", strings.Join(args, " "), err)
	}
	return out
}

// sshGuest runs a command inside the sandbox VM via `ppp exec`.
func (h *harness) sshGuest(args ...string) (string, error) {
	// `--` stops ppp from parsing the guest command's flags (e.g. curl -s -o).
	return h.ppp("", append([]string{"exec", h.sandbox, "--"}, args...)...)
}

func TestE2E(t *testing.T) {
	h := newHarness(t)

	// Podman Machine allows only one running VM at a time (ADR-0007), so a
	// leftover VM from a previous aborted run would block this one. Remove any
	// stray ppp-e2e-* machines first, and always tear down our own at the end.
	cleanupStrayMachines(t)
	t.Cleanup(func() {
		_, _ = h.ppp("", "rm", "-f", h.sandbox)
		_, _ = h.ppp("", "daemon", "stop")
		cleanupStrayMachines(t)
	})

	// A global allow-all policy so egress isn't blocked by default. We do NOT
	// set a real secret here: writing to the developer's OS keychain is a side
	// effect (and a prompt) the e2e must not impose, and the isolation path
	// (tunnel/interception/identity/policy/egress + secret non-leak) does not
	// require an injected credential. Secret injection itself is covered by the
	// addon unit tests (mocked UDS); here we assert the stronger property that
	// nothing sensitive leaks into the sandbox.
	h.mustPPP("", "policy", "init", "allow-all")

	// `run` creates + starts the VM, lazily starts the proxy daemon, provisions
	// the guest (WireGuard tunnel, CA, IPv6), and launches the agent container.
	// The agent-launch step runs `podman run -i -t <image> opencode`; with the
	// default test image (alpine, no opencode) that command exits non-zero,
	// which is fine — we assert the ISOLATION path via `ppp exec` below, not the
	// agent itself. So `run` is allowed to return an error here; provisioning
	// (which precedes agent launch) is what must have succeeded, verified next.
	out, _ := h.ppp("", "run", "opencode", repoRoot(t), "--name", h.sandbox, "--cpus", "2", "--memory", "2048")
	if strings.Contains(out, "not provisioned") {
		t.Fatalf("provisioning did not run (daemon not ready?):\n%s", out)
	}

	waitForTunnel(t, h)

	t.Run("egress_intercepted_and_allowed", func(t *testing.T) {
		// A flow only reaches flows.jsonl if it traversed the tunnel and was
		// seen by mitmdump — so an allow entry for a freshly-fetched unique host
		// is definitive proof of interception (a bypassed request would never be
		// logged). This is a stronger assertion than scraping the leaf issuer.
		out, err := h.sshGuest("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "20", "https://www.iana.org")
		if err != nil || !strings.Contains(out, "200") {
			t.Fatalf("expected 200 for allowed host, got %q (err=%v)", out, err)
		}
		assertFlow(t, h, "www.iana.org", "allow", 200)
	})

	t.Run("interception_uses_mitmproxy_ca", func(t *testing.T) {
		// The mitmproxy CA must be installed in the guest trust store — the
		// provisioning proof that interception (not passthrough) is in force.
		out, err := h.sshGuest("ls", "/etc/pki/ca-trust/source/anchors/")
		if err != nil || !strings.Contains(out, "mitmproxy.crt") {
			t.Errorf("expected mitmproxy CA anchor in guest trust store, got %q (err=%v)", out, err)
		}
	})

	t.Run("upstream_bad_cert_rejected", func(t *testing.T) {
		// The narrowed tolerated-errno set must still reject genuinely bad certs.
		for _, host := range []string{
			"https://self-signed.badssl.com",
			"https://untrusted-root.badssl.com",
			"https://expired.badssl.com",
			"https://wrong.host.badssl.com",
		} {
			out, _ := h.sshGuest("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "20", host)
			if strings.Contains(out, "200") {
				t.Errorf("%s must NOT verify (got %q)", host, out)
			}
		}
	})

	t.Run("policy_deny_enforced", func(t *testing.T) {
		// The addon loads policy per-request from disk, so a deny takes effect
		// immediately — no daemon reload/SIGHUP needed.
		h.mustPPP("", "policy", "deny", "network", "--sandbox", h.sandbox, "example.com")
		out, _ := h.sshGuest("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "15", "https://example.com")
		if strings.Contains(out, "200") {
			t.Errorf("denied host should be blocked, got %q", out)
		}
		assertFlow(t, h, "example.com", "deny", 403)
	})

	t.Run("no_host_secrets_in_sandbox", func(t *testing.T) {
		// Nothing resembling host credential material should be reachable inside
		// the guest: no ppp secret UDS/age store, no host env creds.
		out, _ := h.sshGuest("sh", "-c",
			"env | grep -iE 'API_KEY|_TOKEN|SECRET|PPP_AGE' || true; "+
				"ls -a /run/ppp /var/lib/ppp 2>/dev/null || true")
		for _, needle := range []string{"secret.sock", "secrets.age", "API_KEY=", "_TOKEN="} {
			if strings.Contains(out, needle) {
				t.Errorf("possible host secret material reachable in sandbox: %q in %q", needle, out)
			}
		}
	})
}

// waitForTunnel polls until the guest reports the WireGuard interface is up.
func waitForTunnel(t *testing.T, h *harness) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		// `wg show wg0` exits 0 iff the interface exists; pass it as distinct
		// argv (no `bash -c` string) so ppp forwards it cleanly to the guest.
		out, err := h.sshGuest("sudo", "wg", "show", "wg0")
		if err == nil && strings.Contains(out, "interface: wg0") {
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatal("tunnel did not come up within 120s")
}

// assertFlow checks the newest flows.jsonl entry for host matches decision/status.
func assertFlow(t *testing.T, h *harness, host, decision string, status int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dataDir, "flows.jsonl"))
	if err != nil {
		t.Fatalf("reading flows.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var f struct {
			Sandbox  string `json:"sandbox"`
			Host     string `json:"host"`
			Decision string `json:"decision"`
			Status   int    `json:"status"`
		}
		if json.Unmarshal([]byte(lines[i]), &f) != nil {
			continue
		}
		if f.Host == host && f.Sandbox == h.sandbox {
			if f.Decision != decision || f.Status != status {
				t.Errorf("flow for %s: got decision=%s status=%d, want %s/%d", host, f.Decision, f.Status, decision, status)
			}
			return
		}
	}
	t.Errorf("no flow log entry found for host=%s sandbox=%s", host, h.sandbox)
}

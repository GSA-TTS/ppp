package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/GSA-TTS/ppp/assets"
	"github.com/GSA-TTS/ppp/internal/agent"
	"github.com/GSA-TTS/ppp/internal/podman"
	"github.com/GSA-TTS/ppp/internal/proxy/capture"
	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// errDaemonNotReady means the proxy daemon has not captured client configs yet,
// so guest provisioning cannot proceed. It is not fatal to sandbox creation
// (the sandbox state is already persisted); callers surface it as a hint.
var errDaemonNotReady = errors.New("proxy daemon has not captured client configs (run `ppp daemon start` first)")

// provisionAndRun performs the host-only bring-up for a started sandbox: render
// the guest wg0.conf from the captured client config for this sandbox's port,
// copy it and the provision script into the VM, run the provision script, and
// (when launch is true) run the agent container. It is invoked only after the
// machine is started.
//
// If the proxy daemon has not captured client configs yet (no
// $PPP_DATA/wg/client-confs.json), provisioning cannot proceed; provisionAndRun
// returns errDaemonNotReady so the caller can surface a clear, non-fatal hint
// (the sandbox state is already persisted) rather than a cryptic file error.
// Every guest command goes through the PodmanRunner argv boundary; no shell
// strings are built.
func provisionAndRun(ctx context.Context, runner podman.PodmanRunner, box sandbox.Sandbox, launch bool, agentArgs []string) error {
	cfg, err := clientConfigForPort(box.Port)
	if err != nil {
		return err
	}
	wg0, err := cfg.Rewrite(innerIPOctet(box))
	if err != nil {
		return fmt.Errorf("rendering wg0.conf: %w", err)
	}

	sbDir, err := sandbox.SandboxDir(box.Name)
	if err != nil {
		return err
	}
	wg0Path := filepath.Join(sbDir, "wg0.conf")
	if err := os.WriteFile(wg0Path, []byte(wg0), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", wg0Path, err)
	}

	if err := copyProvisionAssets(ctx, runner, box.Name, wg0Path); err != nil {
		return err
	}
	if err := runProvision(ctx, runner, box); err != nil {
		return err
	}
	if !launch {
		return nil
	}
	return runAgent(ctx, runner, box, agentArgs)
}

// innerIPOctet is N in 10.0.0.N, derived from the sandbox's port so it always
// matches the allocation (N = port - 51819).
func innerIPOctet(box sandbox.Sandbox) int { return box.Port - 51819 }

// clientConfigForPort loads the captured client config for a port from
// $PPP_DATA/wg/client-confs.json (written by the supervisor at daemon start).
func clientConfigForPort(port int) (capture.Config, error) {
	dataDir, err := sandbox.ResolveDataDir()
	if err != nil {
		return capture.Config{}, err
	}
	path := filepath.Join(dataDir, "wg", "client-confs.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return capture.Config{}, errDaemonNotReady
	}
	if err != nil {
		return capture.Config{}, fmt.Errorf("reading captured client configs %q: %w", path, err)
	}
	var byPort map[string]capture.Config
	if err := json.Unmarshal(raw, &byPort); err != nil {
		return capture.Config{}, fmt.Errorf("decoding %q: %w", path, err)
	}
	cfg, ok := byPort[fmt.Sprintf("%d", port)]
	if !ok {
		return capture.Config{}, fmt.Errorf("no captured client config for port %d", port)
	}
	return cfg, nil
}

// copyProvisionAssets writes the embedded provision script to a temp path and
// copies it plus the rendered wg0.conf into the guest.
func copyProvisionAssets(ctx context.Context, runner podman.PodmanRunner, name, wg0Path string) error {
	tmpDir, err := os.MkdirTemp("", "ppp-prov-")
	if err != nil {
		return fmt.Errorf("temp dir for provision assets: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	scriptPath, err := assets.WriteProvisionScript(tmpDir)
	if err != nil {
		return err
	}
	if err := runner.Cp(ctx, name, scriptPath, "/tmp/provision.sh"); err != nil {
		return fmt.Errorf("copying provision.sh into %s: %w", name, err)
	}
	if err := runner.Cp(ctx, name, wg0Path, "/tmp/ppp-wg0.conf"); err != nil {
		return fmt.Errorf("copying wg0.conf into %s: %w", name, err)
	}
	return nil
}

// runProvision executes the provision script inside the guest as root, passing
// the wg config path and agent image via the environment on the command line.
func runProvision(ctx context.Context, runner podman.PodmanRunner, box sandbox.Sandbox) error {
	ag, err := agent.Lookup(box.Agent)
	if err != nil {
		return err
	}
	// The agent image ref is forwarded to the guest login shell (podman machine
	// ssh re-parses argv), so validate it can't inject even though it is a ppp
	// constant today (defense in depth; BLOCKER-1 hardening).
	if err := guestArg("agent image", ag.DefaultImage); err != nil {
		return err
	}
	// sudo -E bash /tmp/provision.sh, with PPP_* exported. Each token is a
	// separate argv element (no shell string); the guest's sudo runs the script
	// as root and the leading VAR=VALUE assignments set the script's env. The
	// fixed config path and the validated image ref carry no shell metacharacters.
	cmd := []string{
		"sudo",
		"PPP_WG_CONF=/tmp/ppp-wg0.conf",
		"PPP_AGENT_IMAGE=" + ag.DefaultImage,
		"bash", "/tmp/provision.sh",
	}
	out, err := runner.SSH(ctx, box.Name, cmd...)
	if err != nil {
		return fmt.Errorf("provisioning %s failed: %w\n%s", box.Name, err, out)
	}
	return logMachineOutput(box.Name, "provision", out)
}

// runAgent launches the agent container inside the guest with the workspace
// mounted. The container image and headless invocation come from the registry;
// credentials are injected by the proxy, never passed here.
func runAgent(ctx context.Context, runner podman.PodmanRunner, box sandbox.Sandbox, agentArgs []string) error {
	ag, err := agent.Lookup(box.Agent)
	if err != nil {
		return err
	}
	// Both the image ref and the workspace path are forwarded to the guest
	// login shell; validate them (workspace was validated at ingress too, but
	// re-check at the point of use so this function is safe on any caller path).
	if err := guestArg("agent image", ag.DefaultImage); err != nil {
		return err
	}
	if err := validateWorkspacePath(box.Workspace); err != nil {
		return err
	}
	for _, a := range agentArgs {
		if err := guestArg("agent arg", a); err != nil {
			return err
		}
	}
	inner := ag.InteractiveArgs(agentArgs)
	// podman run -i -t -v <ws>:/workspace --workdir /workspace <image> <agent...>
	cmd := []string{
		"podman", "run", "-i", "-t",
		"-v", box.Workspace + ":/workspace",
		"--workdir", "/workspace",
		ag.DefaultImage,
	}
	cmd = append(cmd, inner...)
	out, err := runner.SSH(ctx, box.Name, cmd...)
	if err != nil {
		return fmt.Errorf("running agent in %s: %w\n%s", box.Name, err, out)
	}
	return logMachineOutput(box.Name, "agent", out)
}

// logMachineOutput appends captured guest output to the sandbox's machine.log
// (spec §5.8) for `ppp diagnose`.
func logMachineOutput(name, phase string, out []byte) error {
	sbDir, err := sandbox.SandboxDir(name)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(sbDir, "machine.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening machine.log: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "=== %s ===\n%s\n", phase, out); err != nil {
		return fmt.Errorf("writing machine.log: %w", err)
	}
	return nil
}

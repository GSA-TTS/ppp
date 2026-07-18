package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/GSA-TTS/ppp/internal/podman"
	"github.com/GSA-TTS/ppp/internal/proxy/portpool"
	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// createOptions is the state-only subset of `run`/`create` flags T12 wires:
// name, agent, workspace, and VM sizing. The VM bring-up beyond the
// PodmanRunner calls (provision script, WireGuard config, agent container) is
// host-only and lands in T13.
type createOptions struct {
	name      string // "" => auto-generate ppp-<adjective>-<noun>
	agent     string
	workspace string
	cpus      uint
	memoryMiB uint
}

// portRegistryPath returns $PPP_DATA/port-registry.json (spec §5.8).
func portRegistryPath() (string, error) {
	dataDir, err := sandbox.ResolveDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "port-registry.json"), nil
}

// provisionSandbox performs the state-only creation path shared by `create`
// and `run`: validate inputs, allocate a name and a WireGuard port + inner IP,
// invoke the PodmanRunner to init (and, when start is true, start) the machine,
// and persist sandbox.json. It returns the persisted record.
//
// The whole mutation is serialized under $PPP_DATA/state.lock so two concurrent
// CLI invocations cannot race the port registry (spec §5.8). VM provisioning
// steps beyond init/start (WireGuard config rewrite, provision script, agent
// container) are host-only and deferred to T13.
func provisionSandbox(runner podman.PodmanRunner, opts createOptions, start bool) (sandbox.Sandbox, error) {
	if opts.agent != "opencode" {
		return sandbox.Sandbox{}, fmt.Errorf("agent %q not supported in this version (v1: opencode only)", opts.agent)
	}
	if err := validateWorkspacePath(opts.workspace); err != nil {
		return sandbox.Sandbox{}, err
	}

	name, err := resolveName(opts.name)
	if err != nil {
		return sandbox.Sandbox{}, err
	}

	var result sandbox.Sandbox
	err = sandbox.WithLock(func() error {
		box, lerr := createLocked(runner, name, opts, start)
		if lerr != nil {
			return lerr
		}
		result = box
		return nil
	})
	return result, err
}

// resolveName returns a validated explicit name or a generated one.
func resolveName(explicit string) (string, error) {
	if explicit != "" {
		if err := validateSandboxName(explicit); err != nil {
			return "", err
		}
		if exists, _ := sandboxExists(explicit); exists {
			return "", fmt.Errorf("sandbox %q already exists", explicit)
		}
		return explicit, nil
	}
	return generateName()
}

// sandboxExists reports whether a sandbox.json already exists for name.
func sandboxExists(name string) (bool, error) {
	path, err := sandbox.SandboxJSONPath(name)
	if err != nil {
		return false, err
	}
	return fileExists(path), nil
}

// createLocked does the allocation + runner calls + persistence while the state
// lock is held. On any failure after the port is allocated it frees the port so
// a partial create leaks nothing.
func createLocked(runner podman.PodmanRunner, name string, opts createOptions, start bool) (sandbox.Sandbox, error) {
	regPath, err := portRegistryPath()
	if err != nil {
		return sandbox.Sandbox{}, err
	}
	pool, err := portpool.New(regPath)
	if err != nil {
		return sandbox.Sandbox{}, err
	}
	alloc, err := pool.Allocate(name)
	if err != nil {
		return sandbox.Sandbox{}, err
	}

	box, err := bringUp(runner, name, opts, alloc, start)
	if err != nil {
		_ = pool.Free(name) // best-effort rollback of the port allocation
		return sandbox.Sandbox{}, err
	}
	return box, nil
}

// bringUp invokes the PodmanRunner and persists sandbox.json for a freshly
// allocated port. VM provisioning beyond init/start is deferred to T13.
func bringUp(runner podman.PodmanRunner, name string, opts createOptions, alloc portpool.Allocation, start bool) (sandbox.Sandbox, error) {
	ctx := context.Background()
	// Enforce single-active BEFORE creating the VM (ADR-0007): the check is
	// state-only and cheap, so doing it first avoids creating (and then having to
	// roll back) a Podman Machine that could never start anyway.
	if start {
		if err := ensureNoActiveSandbox(name); err != nil {
			return sandbox.Sandbox{}, err
		}
	}
	if err := runner.Init(ctx, podman.InitOptions{
		Name:           name,
		CPUs:           opts.cpus,
		MemoryMiB:      opts.memoryMiB,
		ImportNativeCA: true,
	}); err != nil {
		return sandbox.Sandbox{}, fmt.Errorf("podman machine init: %w", err)
	}

	status := sandbox.StatusCreated
	if start {
		if err := runner.Start(ctx, name); err != nil {
			return sandbox.Sandbox{}, fmt.Errorf("podman machine start: %w", err)
		}
		status = sandbox.StatusRunning
	}

	box := sandbox.Sandbox{
		Name:      name,
		Agent:     opts.agent,
		Workspace: opts.workspace,
		Status:    status,
		CreatedAt: time.Now().UTC(),
		CPUs:      opts.cpus,
		Memory:    opts.memoryMiB,
		Port:      alloc.Port,
		InnerIP:   alloc.InnerIP,
	}
	if err := box.Save(); err != nil {
		return sandbox.Sandbox{}, err
	}
	return box, nil
}

// ensureNoActiveSandbox enforces the v1 single-active-sandbox constraint on
// Podman-Machine hosts (ADR-0007): Podman Machine allows only one running VM at
// a time, so starting a second sandbox fails with a raw "only one VM can be
// active at a time" error. We pre-check the recorded sandbox state and return a
// clear, actionable message naming the running sandbox instead. `about` is the
// sandbox being started (excluded from the check).
func ensureNoActiveSandbox(about string) error {
	boxes, err := loadAllSandboxes()
	if err != nil {
		return err
	}
	for _, b := range boxes {
		if b.Name == about {
			continue
		}
		if b.Status == sandbox.StatusRunning {
			return fmt.Errorf(
				"sandbox %q is already running; only one sandbox can run at a time on this host "+
					"(stop it first with `ppp stop %s`; see docs/decisions/0007)",
				b.Name, b.Name)
		}
	}
	return nil
}

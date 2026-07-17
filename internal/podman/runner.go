package podman

import (
	"context"
	"fmt"
	"runtime"
)

// Provider is a Podman Machine VM provider (spec §5.1). ppp normally lets
// Podman autodetect the provider; the value is exposed so ppp can override it
// via `--provider` when necessary.
type Provider string

const (
	// ProviderLibkrun is the macOS default (spec §5.1, `vmtype: libkrun`).
	ProviderLibkrun Provider = "libkrun"
	// ProviderWSL is the Windows default.
	ProviderWSL Provider = "wsl"
	// ProviderQEMU is the Linux default.
	ProviderQEMU Provider = "qemu"
)

// knownProviders is the set of provider values ppp will emit into argv.
var knownProviders = map[Provider]struct{}{
	ProviderLibkrun: {},
	ProviderWSL:     {},
	ProviderQEMU:    {},
}

// DetectProvider returns the default Podman Machine provider for the host OS
// (spec §5.1). This mirrors Podman's own autodetection; ppp uses it only when
// it needs to name the provider explicitly. It does not shell out.
func DetectProvider() Provider {
	switch runtime.GOOS {
	case "darwin":
		return ProviderLibkrun
	case "windows":
		return ProviderWSL
	default:
		return ProviderQEMU
	}
}

// validateProvider rejects provider overrides ppp does not recognize, failing
// closed rather than emitting an unknown value into argv.
func validateProvider(p string) error {
	if _, ok := knownProviders[Provider(p)]; !ok {
		return fmt.Errorf("podman: unknown provider %q (want libkrun, wsl, or qemu)", p)
	}
	return nil
}

// Machine is a decoded entry from `podman machine list --format json`.
// Only the fields ppp needs (name, running state, provider VM type) are
// modeled; unknown fields are ignored by encoding/json.
type Machine struct {
	Name    string `json:"Name"`
	Running bool   `json:"Running"`
	VMType  string `json:"VMType,omitempty"`
}

// PodmanRunner is the single boundary for all podman interaction. Every method
// builds an argv slice internally (never a shell string) and — for name-taking
// operations — refuses Podman's implicit default machine and non-ppp-namespaced
// names (ADR-0001). Implementations: the host-only shell-out Runner (real exec
// is T13) and the in-memory Fake for tests.
type PodmanRunner interface {
	// Init runs `podman machine init` with the given options.
	Init(ctx context.Context, opts InitOptions) error
	// Start runs `podman machine start <name>`.
	Start(ctx context.Context, name string) error
	// Stop runs `podman machine stop <name>`.
	Stop(ctx context.Context, name string) error
	// Rm runs `podman machine rm [--force] <name>`.
	Rm(ctx context.Context, name string, force bool) error
	// SSH runs `podman machine ssh <name> -- <command...>` and returns the
	// captured combined output.
	SSH(ctx context.Context, name string, command ...string) ([]byte, error)
	// Cp runs `podman machine cp <localPath> <name>:<remotePath>`.
	Cp(ctx context.Context, name, localPath, remotePath string) error
	// List runs `podman machine list --format json` and decodes the result.
	List(ctx context.Context) ([]Machine, error)
	// Inspect runs `podman machine inspect <name>` and returns raw JSON.
	Inspect(ctx context.Context, name string) ([]byte, error)
	// Provider reports the provider this runner targets (for logging/override).
	Provider() Provider
}

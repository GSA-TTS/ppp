package cli

import (
	"github.com/GSA-TTS/ppp/internal/podman"
	"github.com/GSA-TTS/ppp/internal/secret"
)

// deps bundles the runtime seams a command needs, so tests can inject fakes
// while production uses the real implementations. It is threaded through the
// command constructors (newXCmd(d)) rather than reached for via globals, so a
// test builds its own root with a fully-controlled dependency set and never
// touches the OS keychain, a real Podman Machine, or a live proxy process.
//
// Every field is a factory (a function returning the seam) rather than the
// seam itself. Factories keep construction lazy: resolving the real
// PodmanRunner or opening the keychain must not happen at `NewRootCmd()` time
// (that runs for `--help`, completion, and every unrelated subcommand), only
// when a command actually needs the seam.
type deps struct {
	// newRunner returns the PodmanRunner a VM-touching command should use.
	newRunner func() podman.PodmanRunner
	// newStore returns the secret.Store secret commands read/write through.
	newStore func() secret.Store
	// newSupervisor returns the proxy Supervisor daemon commands drive.
	newSupervisor func() Supervisor
}

// defaultDeps builds the production dependency set. The real PodmanRunner
// (shell-out exec) and the mitmdump supervisor are host-only and land in T13;
// until then those factories return the interface-satisfying placeholders whose
// VM/process operations report "not implemented on this host", while every
// state-only path (name/port/inner-IP allocation, sandbox.json persistence,
// policy and secret storage) is fully wired here in T12.
func defaultDeps() deps {
	return deps{
		newRunner:     func() podman.PodmanRunner { return podman.NewRunner() },
		newStore:      func() secret.Store { return secret.NewKeyringStore() },
		newSupervisor: func() Supervisor { return newHostSupervisor() },
	}
}

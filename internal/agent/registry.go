// Package agent holds the built-in agent registry (spec §5.7).
//
// v1 ships exactly one agent, opencode. The registry maps an agent name to the
// facts ppp needs to run it inside a sandbox: the default container image, the
// headless invocation, and the environment. Credentials are NOT stored here —
// they are injected host-side by the mitmproxy addon on outbound requests
// (wayfinder #11), so the agent inside the sandbox only ever holds placeholders.
package agent

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrUnknownAgent is returned for an agent name not in the v1 registry.
var ErrUnknownAgent = errors.New("agent: not supported in this version")

// Agent describes how to run a coding agent inside a sandbox container.
type Agent struct {
	// Name is the agent identifier used on the CLI (e.g. "opencode").
	Name string
	// DefaultImage is the OCI image ppp pulls and runs in the sandbox VM.
	DefaultImage string
	// Env is the fixed environment set on the agent container. It never
	// contains secrets (those are injected by the proxy).
	Env map[string]string
	// runHeadless renders the argv to run the agent non-interactively for a
	// given prompt (spec §6.1; wayfinder #11: `opencode run "<prompt>"`).
	runHeadless func(prompt string, passthrough []string) []string
	// runInteractive renders the argv to run the agent attached to a TTY.
	runInteractive func(passthrough []string) []string
}

// HeadlessArgs returns the argv to run the agent for a scripted prompt, with
// any user `--` passthrough args appended verbatim (spec §6.1 AGENT_ARGS).
func (a Agent) HeadlessArgs(prompt string, passthrough []string) []string {
	return a.runHeadless(prompt, passthrough)
}

// InteractiveArgs returns the argv to run the agent attached to a TTY, with any
// user `--` passthrough args appended verbatim.
func (a Agent) InteractiveArgs(passthrough []string) []string {
	return a.runInteractive(passthrough)
}

// registry is the v1 built-in agent set.
var registry = map[string]Agent{
	"opencode": {
		Name:         "opencode",
		DefaultImage: "ghcr.io/gsa-tts/ppp-opencode:latest",
		Env: map[string]string{
			"OPENCODE_SANDBOX": "1",
		},
		runHeadless: func(prompt string, passthrough []string) []string {
			argv := []string{"opencode", "run", prompt}
			return append(argv, passthrough...)
		},
		runInteractive: func(passthrough []string) []string {
			return append([]string{"opencode"}, passthrough...)
		},
	},
}

// Lookup returns the registered agent by name, or ErrUnknownAgent.
//
// The default container image may be overridden with PPP_<AGENT>_IMAGE (agent
// name upper-cased, non-alphanumerics -> underscore), e.g. PPP_OPENCODE_IMAGE.
// This is primarily for testing (the e2e points it at a small public image
// before the real opencode image is published) but is a legitimate escape hatch
// for air-gapped/mirror registries too. The override is validated at the point
// of use (guest-arg safety) like any image ref.
func Lookup(name string) (Agent, error) {
	a, ok := registry[name]
	if !ok {
		return Agent{}, fmt.Errorf("%w: %q (v1 supports: opencode)", ErrUnknownAgent, name)
	}
	if img := os.Getenv(imageEnvVar(name)); img != "" {
		a.DefaultImage = img
	}
	return a, nil
}

// imageEnvVar returns the per-agent image-override env var name, e.g.
// "opencode" -> "PPP_OPENCODE_IMAGE".
func imageEnvVar(name string) string {
	var b strings.Builder
	b.WriteString("PPP_")
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	b.WriteString("_IMAGE")
	return b.String()
}

// Names returns the registered agent names (v1: just "opencode").
func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	return names
}

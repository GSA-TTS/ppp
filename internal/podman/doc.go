// Package podman wraps the podman CLI for per-sandbox Podman Machine
// lifecycle management and host-provider detection (spec §5.1).
//
// It will provide init/start/stop/rm/ssh operations against the one dedicated
// Podman Machine that each sandbox owns (the strict 1:1 sandbox↔machine
// isolation invariant), plus provider selection (libkrun on macOS, wsl on
// Windows, qemu on Linux). No logic is implemented yet.
package podman

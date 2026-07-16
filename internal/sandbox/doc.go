// Package sandbox manages sandbox state, lifecycle, and resources (spec §5.8).
//
// It will own the XDG-based state directory layout, per-sandbox sandbox.json
// records, and the lifecycle transitions that tie a sandbox to its dedicated
// Podman Machine and WireGuard port. No logic is implemented yet.
package sandbox

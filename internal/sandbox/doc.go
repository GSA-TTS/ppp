// Package sandbox manages sandbox state, lifecycle, and resources (spec §5.8).
//
// It owns the XDG-based state directory layout, per-sandbox sandbox.json
// records, and the lifecycle transitions that tie a sandbox to its dedicated
// Podman Machine and WireGuard port.
//
// # State layout
//
// Paths are resolved from the environment with ppp-specific overrides taking
// precedence over the XDG base-directory variables, which in turn fall back to
// $HOME (spec §5.8):
//
//	$PPP_DATA   → $XDG_DATA_HOME/ppp   → ~/.local/share/ppp
//	$PPP_CONFIG → $XDG_CONFIG_HOME/ppp → ~/.config/ppp
//	$PPP_CACHE  → $XDG_CACHE_HOME/ppp  → ~/.cache/ppp
//
// Each sandbox stores its record at <PPP_DATA>/sandboxes/<name>/sandbox.json.
// A single flock at <PPP_DATA>/state.lock (see WithLock) serializes concurrent
// CLI operations that mutate state.
//
// # Records
//
// A Sandbox is written atomically (temp file + rename) so a crash mid-write
// never leaves a partial record. MachineName maps 1:1 to Name — a sandbox owns
// exactly one dedicated Podman Machine and machines are never shared
// (ADR-0001). The WireGuard listen Port is the sandbox's identity (ADR-0003).
//
// # Lifecycle
//
// Statuses are created, running, and stopped. The only permitted transitions
// are created→running, running→stopped, and stopped→running (see Transition);
// every other change, including self-loops and any transition back to created,
// is rejected.
package sandbox

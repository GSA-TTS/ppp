// Package portpool allocates and frees the WireGuard port pool and the inner
// tunnel IPs that are derived from it, persisting the port-to-sandbox mapping to
// a port-registry.json file so allocations survive daemon crashes (spec §5.3,
// §5.8).
//
// # Port and inner-IP derivation
//
// Ports start at [BasePort] (51820). Each port maps to an index N via
// N = port - 51819, so 51820 is N=1, 51821 is N=2, and so on. The inner tunnel
// IP is derived directly from N as 10.0.0.N; the inner IP is therefore never
// tracked separately — freeing a port frees its inner IP automatically. The
// inner IP is not an identity (a sudo agent in the guest can rewrite it); a
// sandbox is identified by its WireGuard listen port (ADR-0003).
//
// # Reuse safety
//
// Reallocating a previously used port to a new sandbox is safe because identity
// is bound to the listen port and its per-port keypair (managed elsewhere), not
// to the inner IP (ADR-0003). A "removing" tombstone still holds a port out of
// the free pool while its sandbox is being torn down, so a port is not reused
// until the teardown is confirmed with [Pool.Free].
//
// # Reconciliation
//
// On daemon start, [Pool.Reconcile] compares the persisted registry against a
// live list of sandboxes obtained from an injected [MachineLister] and frees any
// entry whose sandbox no longer exists (a stale entry left by a crash). The
// lister is an interface so callers inject a real podman-backed implementation
// and tests inject a fake; this package never imports the podman package.
package portpool

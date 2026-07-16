---
title: "Identify a sandbox by its WireGuard listen port, not its inner tunnel IP"
status: "accepted"
date: "2026-07-16"
decision_makers: ["agentic-coding-team"]
category: "Authentication and Identity"
nist_controls: ["IA-3", "IA-9", "AC-4", "AC-6", "SC-23"]
impact_level: "low"
ato_relevance: "no"
risk_treatment: "mitigate"
---

# Identify a sandbox by its WireGuard listen port, not its inner tunnel IP

## Context and Problem Statement

The single mitmdump addon (ADR-0002) must map each intercepted flow back to the
sandbox that produced it, in order to apply that sandbox's network policy and
inject the correct (possibly per-sandbox / billing-code-scoped) secrets. If this
attribution can be forged from inside a sandbox, an agent could inherit another
sandbox's policy allowances and credentials — a privilege-escalation and
credential-disclosure risk. We must choose a sandbox identifier that a
`sudo`-capable agent inside the guest cannot spoof.

## Decision Drivers

- The agent inside a sandbox has root/sudo (matching the Docker `sbx` model),
  so it can reconfigure its own network interfaces.
- Attribution drives both policy enforcement (AC-4) and secret scoping (AC-6);
  a wrong mapping leaks credentials across sandbox boundaries.
- Identity should be cryptographically bound where possible (IA-3/IA-9 device/
  service identification, SC-23 session authenticity).
- The mechanism must be verifiable against the actual mitmproxy/mitmproxy_rs
  behavior, not assumed.

## Considered Options

1. **WireGuard listen port** — key attribution on `flow.client.sockname` (the
   server-side UDP port that received the flow). Each sandbox has its own
   `--mode wireguard@<port>` instance with its own keypair.
2. **Inner tunnel IP** — key on `flow.client.address` / `peername` (the
   `10.0.0.N` source address of the decrypted inner packet).
3. **Combination** — require both port and inner IP to match.

## Decision Outcome

Chosen option: **the WireGuard listen port** (`flow.client.sockname`), because it
is cryptographically bound to a per-sandbox keypair and cannot be changed by the
guest, whereas the inner IP is guest-controllable.

This was verified two ways (`docs/explorations/ppp-spec.md` §3.1):

- **Source trace:** the decrypted inner IPv4 source address (`packet.src_addr()`
  in `mitmproxy_rs/.../wireguard.rs`) flows through to the addon as
  `flow.client.peername`/`address`. That value is simply whatever address
  `wg-quick` assigned to the guest's `wg0` — i.e. the `Address =` line — which a
  `sudo` agent can change with `ip addr` to impersonate another sandbox's
  `10.0.0.M`. By contrast, each `--mode wireguard@<port>` binds its own UDP
  socket, so `dst_addr = socket.local_addr()` (surfaced as `sockname`) reflects
  which instance received the packet; the guest cannot move its traffic to a
  different host port without that instance's server private key.
- **Live spike:** running two WireGuard instances (`@51820`, `@51821`) in one
  `mitmdump` process, both generated client configs carried the hardcoded
  `Address = 10.0.0.1/32` and differed only by `Endpoint = <ip>:<port>` —
  confirming the port is the only reliable per-instance discriminator present.

Option 2 is therefore rejected as the trust anchor. Option 3 adds no security
over Option 1 (the port already binds identity) and would falsely reject valid
flows if the `Address` rewrite ever drifts, so it is rejected for simplicity.
The inner IP is still rewritten per sandbox and logged for readability, but is
never used for authorization.

### Positive Consequences

- Sandbox identity is unspoofable by a root agent inside the guest — closes the
  cross-sandbox policy/secret-inheritance hole.
- Attribution is a simple, deterministic `port → sandbox_name` lookup in the
  addon, refreshed from `port-registry.json` on SIGHUP.
- Consistent with the per-sandbox keypair/port established in ADR-0002.

### Negative Consequences

- The `Address` rewrite is still performed (for log/routing clarity), so the
  parser/rewriter for mitmproxy's client-config text remains a maintenance point
  (`docs/explorations/ppp-spec.md` Risk #8) — but a wrong rewrite now degrades readability only,
  not the security boundary.
- Residual: mitmproxy_rs's `process_outgoing_packet` "fall back to first peer"
  path should be exercised under many-sandbox load to confirm no return-path
  cross-talk (`docs/explorations/ppp-spec.md` Risk #10).

### Compliance Consequences

- Establishes a spoof-resistant identifier for each sandbox "endpoint" (IA-3,
  IA-9) bound to session/transport authenticity (SC-23).
- Preserves information-flow enforcement (AC-4) and least-privilege secret
  scoping (AC-6) by preventing identity forgery.
- Not a FISMA system; controls cited for rigor (`ato_relevance: no`).

## Links

- `docs/explorations/ppp-spec.md` §3.1 ("Sandbox identification — use the listen port"), §4.2,
  §5.4 (addon), §8.4 (multi-sandbox example), Risk #10
- ADR-0002 (single mitmdump + multi-WireGuard) — provides the per-sandbox port/keypair
- `AGENTS.md` → Prohibited Actions (never identify a sandbox by its inner IP)

---
title: "Use a single mitmdump process hosting one WireGuard instance per sandbox"
status: "accepted"
date: "2026-07-16"
decision_makers: ["agentic-coding-team"]
category: "Deployment and Infrastructure"
nist_controls: ["AC-4", "AU-2", "SC-7", "SI-4"]
impact_level: "low"
ato_relevance: "no"
risk_treatment: "mitigate"
---

# Use a single mitmdump process hosting one WireGuard instance per sandbox

## Context and Problem Statement

Every sandbox VM's egress must be transparently intercepted so `ppp` can enforce
network policy, inject host-held credentials, and audit traffic — without any
in-guest proxy configuration the agent could disable. We need a transport that is
truly transparent (not an app-level HTTP proxy), needs no host root, works on
macOS and Windows, and lets a single control point audit **all** sandboxes at
once. We also need each sandbox to be individually attributable within that
control point.

## Decision Drivers

- Transparent interception of all TCP/UDP egress, not just apps that honor
  `HTTP_PROXY` (the agent must not be able to opt out).
- One place to audit and enforce policy across every sandbox simultaneously
  (AU-2, SI-4) rather than N independent proxies to supervise.
- No host root/admin (userspace only).
- macOS + Windows primary.
- Must compose with the "one dedicated VM per sandbox" invariant (ADR-0001) and
  give each sandbox a distinct, attributable channel (AC-4).
- Prefer mature OSS with prior art for the Podman + mitmproxy combination.

## Considered Options

1. **Single `mitmdump` process, one WireGuard server instance per sandbox**
   (repeatable `--mode wireguard:<keys>@<port>`), all sharing one addon and one
   flow log. A pre-allocated pool of ports (51820–51899) is started up front.
2. **One `mitmdump` process per sandbox** — full process isolation per sandbox,
   N proxies to supervise.
3. **Explicit HTTP/HTTPS proxy** (the Docker `sbx` model) — configure the agent
   with proxy env vars instead of a transparent tunnel.
4. **Single WireGuard instance with multiple peers** — one server, many client
   keypairs on one port.

## Decision Outcome

Chosen option: **a single `mitmdump` process hosting one WireGuard instance per
sandbox**, because it gives transparent, userspace, cross-platform interception
with a single audit/enforcement point, while still giving each sandbox its own
keypair and UDP port for attribution and crypto separation (`docs/explorations/ppp-spec.md` §3.1,
§4.2).

Option 4 was rejected after reading the mitmproxy source: a WireGuard instance is
started with a single peer public key (`start_wireguard_server(..., [pubkey], ...)`),
and there is no supported way to add peers to one instance without forking —
the documented approach is repeatable `--mode wireguard` flags, which is exactly
Option 1. Option 2 (process-per-sandbox) multiplies supervision and log
fan-out for no isolation gain, since each WireGuard instance in Option 1 already
has its own keypair/port. Option 3 (explicit proxy) is weaker: a `sudo` agent can
unset the proxy env and non-HTTP traffic escapes; the WireGuard tunnel with
`AllowedIPs = 0.0.0.0/0` cannot be opted out of from inside the guest.

### Positive Consequences

- Truly transparent interception (TCP + UDP); the agent cannot route around it
  by editing proxy env.
- One addon, one `flows.jsonl`, one policy/secret cache → simple, unified audit
  and enforcement across all sandboxes (AU-2, SI-4).
- Per-sandbox keypair + UDP port gives crypto separation and a clean attribution
  handle (see ADR-0003) consistent with the 1-VM-per-sandbox invariant.
- Userspace WireGuard server → no host root.

### Negative Consequences

- **Single blast radius:** if the one `mitmdump` process dies, all sandboxes lose
  their proxy. Mitigated by the Go binary supervising/restarting it; WireGuard's
  roaming re-handshake reconnects tunnels on restart (`docs/explorations/ppp-spec.md` §10.3, Risk #7).
- Pre-starting 80 WireGuard instances has some startup/memory cost; expected
  negligible when idle, but flagged for benchmarking (`docs/explorations/ppp-spec.md` Risk #3).
- `ppp` depends on the exact text format of mitmproxy's generated client config
  (which it must parse and rewrite); pinned mitmproxy version mitigates
  (`docs/explorations/ppp-spec.md` §3.1, Risk #8).
- The single WireGuard endpoint reachable at `AllowedIPs = 0.0.0.0/0` requires an
  in-guest off-tunnel route to avoid a routing loop (`docs/explorations/ppp-spec.md` §5.2, Risk #9).

### Compliance Consequences

- Centralizes information-flow enforcement between each sandbox and the network
  (AC-4) and boundary mediation (SC-7) at one auditable choke point.
- Produces a unified, per-sandbox-attributed flow log supporting audit and
  continuous monitoring (AU-2, SI-4) — with secrets redacted (`docs/explorations/ppp-spec.md` §5.4).
- Not a FISMA system; controls cited for rigor (`ato_relevance: no`).

## Links

- `docs/explorations/ppp-spec.md` §3.1 (mitmproxy WireGuard verification), §4.1–§4.2 (topology,
  single mitmproxy / multi-WG), §5.3 (ProxySup), §9 (daemon lifecycle)
- ADR-0001 (one Podman Machine per sandbox) — this proxy model assumes that 1:1
  mapping
- ADR-0003 (sandbox identification by WireGuard listen port)

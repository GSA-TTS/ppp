---
title: "Route guest DNS off-tunnel via gvproxy instead of mitmproxy's in-tunnel resolver"
status: "accepted"
date: "2026-07-16"
decision_makers: ["agentic-coding-team"]
category: "Deployment and Infrastructure"
nist_controls: ["AC-4", "SC-7", "SI-4"]
impact_level: "low"
ato_relevance: "no"
risk_treatment: "accept"
---

# Route guest DNS off-tunnel via gvproxy instead of mitmproxy's in-tunnel resolver

## Context and Problem Statement

mitmproxy's generated WireGuard client config hardcodes `DNS = 10.0.0.53`, its
own in-tunnel resolver. On the Podman Machine libkrun provider the guest already
has a working resolver at the gvproxy gateway (`192.168.127.1`). We must decide
which resolver the FCOS guest uses when `ppp` writes `wg0.conf`, weighing policy
visibility against reliability of tunnel bring-up. (Wayfinder ticket #4.)

## Decision Drivers

- `wg-quick up` must not fail during provisioning. The `DNS =` line makes
  `wg-quick` invoke `resolvconf`/`resolvectl`, a path that is fragile on FCOS and
  whose failure aborts the entire `up`, taking down the tunnel.
- Network egress policy is enforced by the mitmproxy addon on the intercepted
  TCP/UDP flows regardless of which resolver returns the address (AC-4, SC-7).
- DNS *visibility* in the flow log has audit value (SI-4) but is secondary for a
  local dev tool.
- macOS/libkrun is the only v1 target (Windows/WSL out of scope).

## Considered Options

1. **Keep `DNS = 10.0.0.53`** — DNS resolves through the tunnel to mitmproxy;
   queries are visible/attributable in the proxy.
2. **Drop the `DNS =` line; use gvproxy's resolver (`192.168.127.1`)** — DNS
   resolves off-tunnel via gvproxy; `wg-quick up` avoids the resolvconf path.
3. **Set `Table = off` and manage DNS manually** — full control, more moving parts.

## Decision Outcome

Chosen option: **drop the `DNS =` line and use gvproxy's resolver** (Option 2).
When `ppp` rewrites the captured client config into `wg0.conf`, it removes the
`DNS = 10.0.0.53` line entirely, so the guest keeps its gvproxy-provided resolver.

Rationale: reliable tunnel bring-up on FCOS outweighs in-proxy DNS visibility for
a local dev tool. Crucially, **this does not weaken egress enforcement** — the
addon still intercepts and policies every connection to whatever address DNS
returned; only the DNS *lookup* itself is not seen by the proxy. Option 1 risks
provisioning failures; Option 3 is heavier than needed given `Table = off` is
already adopted for routing (ticket #4) and manual DNS adds little.

### Positive Consequences

- `wg-quick up` no longer depends on the fragile resolvconf path — more robust
  provisioning on FCOS.
- Egress policy enforcement is unchanged (addon operates on connections, not
  lookups).

### Negative Consequences

- **DNS queries are not visible in the mitmproxy flow log** — a minor reduction in
  audit fidelity (SI-4). A determined agent could use DNS as a low-bandwidth
  side channel; acceptable for a local dev tool, and connection-level policy
  still blocks disallowed destinations.
- Slight divergence from mitmproxy's intended default config, documented here and
  as a spec correction.

### Compliance Consequences

- Egress information-flow enforcement (AC-4) and boundary mediation (SC-7) are
  preserved at the connection layer.
- Accepts reduced DNS-lookup visibility (SI-4) as a conscious tradeoff for a
  non-FISMA local tool (`ato_relevance: no`, `risk_treatment: accept`). Revisit if
  DNS-level audit ever becomes a requirement.

## Links

- Wayfinder ticket [#4 — In-guest WireGuard routing on macOS/libkrun](https://github.com/GSA-TTS/ppp/issues/4)
- `docs/explorations/ppp-spec.md` §5.2 (provision DNS note), §3.1 (hardcoded DNS)
- ADR-0002 (single mitmdump + multi-WireGuard) — provides the intercept point

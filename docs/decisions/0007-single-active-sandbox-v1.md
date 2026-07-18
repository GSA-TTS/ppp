---
title: "Accept single-active-sandbox on macOS for v1; plan concurrency below podman machine for v2"
status: "accepted"
date: "2026-07-18"
decision_makers: ["agentic-coding-team"]
category: "Deployment and Infrastructure"
nist_controls: ["CM-7", "SC-39"]
impact_level: "low"
ato_relevance: "no"
risk_treatment: "accept"
supersedes: []
---

# Accept single-active-sandbox on macOS for v1; plan concurrency below podman machine for v2

## Context and Problem Statement

ADR-0001 established one dedicated Podman Machine microVM per sandbox, and
ADR-0002's single-mitmdump / WireGuard-port-pool design assumes **multiple
sandboxes running concurrently** (distinct inner IPs `10.0.0.1`–`10.0.0.N`, a
pool of up to 80 WG instances). During the T14 end-to-end validation on real
macOS/libkrun, starting a second sandbox VM while one was running failed:

```
Error: unable to start "ppp-...": ppp-... already starting or running:
only one VM can be active at a time
```

We must decide v1's stance on concurrent sandboxes and whether the constraint is
fundamental.

## Where the limit comes from (investigated)

It is a **Podman Machine policy, not a hypervisor limit** (see
`/tmp/opencode/research-concurrent-vms.md`):

- The error is `ErrMultipleActiveVM` (`pkg/machine/define/errors.go`), raised by
  `checkExclusiveActiveVM()` (`pkg/machine/shim/host.go`), gated by a per-provider
  `RequireExclusiveActive()` boolean and a global start-lock.
- On macOS every provider opts in: **libkrun → true, applehv → true** (qemu,
  hyperv → true; only **wsl → false**, which is why WSL runs many). Switching to
  `applehv` does not help.
- The underlying VMMs do **not** impose this: `krunkit` (libkrun/HVF) and
  `vfkit` (Virtualization.framework) are one-VM-per-process with no cross-process
  lock. **Lima runs N concurrent VMs on the same `vz`/`krunkit` substrate**,
  proving the limit is podman policy.
- Not configurable: no flag/env; upstream FR (podman #26281) is stale and
  maintainers consider `podman machine` "designed for one".

## Decision Drivers

- v1 should ship on the validated macOS/libkrun path without re-opening the
  isolation model (ADR-0001 one-VM-per-sandbox, never shared, is unchanged).
- A single developer typically runs one agent session at a time, so
  single-active is usable; multi-sandbox is a friction reducer, not a blocker.
- The concurrency ceiling is external (podman), not intrinsic, so it can be
  lifted later without changing the security model.

## Decision Outcome

**v1: accept single-active-sandbox on Podman-Machine hosts (macOS).** Multiple
sandboxes may be *created* and coexist in a *stopped* state, but only one is
*running* at a time. `ppp run`/`ppp create --start` for a second sandbox while
one is running must fail with a clear, actionable message (name the running
sandbox and tell the user to `ppp stop` it first), rather than surfacing
podman's raw error. `ppp ls` continues to show all sandboxes and their state.

The ADR-0002 machinery (single mitmdump, WG port pool, per-listen-port identity)
is **retained unchanged**: it is already correct for one active client and
remains correct — with no rework — if/when concurrency is enabled. The port pool
simply rarely has more than one live client on v1.

**v2 direction (not built now): enable true concurrent per-sandbox microVMs by
dropping below `podman machine`.** Two candidate paths, to be evaluated with an
ADR when v2 starts:

1. **Lima per sandbox** (`limactl`, `vz`/`krunkit` backend) — Lima allows many
   concurrent instances and retains lifecycle + networking glue; closest drop-in.
2. **Direct `krunkit`/`vfkit` per sandbox** — ppp owns VM lifecycle, networking,
   and image management; most control, best fit for the WireGuard→mitmproxy
   egress design, highest implementation cost.

Both preserve ADR-0001's one-kernel-per-sandbox guarantee; they only change the
VM manager. Note this reopens the ADR-0001 "Podman Machine over Lima" choice for
v2, on new evidence (the concurrency ceiling) that was not known at v1.

### Positive Consequences

- v1 ships on the fully-validated path; no isolation-model rework.
- The security-critical proxy/identity design is untouched and forward-compatible
  with concurrency.
- The concurrency ceiling is documented as external and removable, with a
  concrete v2 plan.

### Negative Consequences

- Users cannot run two sandboxes at once on macOS in v1 (must stop one first) —
  friction for parallel-agent workflows.
- v2 concurrency likely means leaving `podman machine`, a non-trivial change
  (re-home lifecycle/networking/image glue) — flagged now so it is a conscious
  future cost.

### Compliance Consequences

- Least-functionality (CM-7) and isolation (SC-39) are unchanged: still exactly
  one dedicated kernel per sandbox; v1 merely limits how many run at once.
- Not a FISMA system (`ato_relevance: no`).

## Links

- ADR-0001 (one Podman Machine per sandbox) — unchanged; its "Podman over Lima"
  rationale is revisited for v2 concurrency.
- ADR-0002 (single mitmdump + WG port pool) — retained; correct for one active
  client and forward-compatible with concurrency.
- Research: `/tmp/opencode/research-concurrent-vms.md`; podman FR #26281.
- Surfaced by T14 (#27) live validation.

---
title: "Isolate each sandbox in its own dedicated Podman Machine microVM"
status: "accepted"
date: "2026-07-16"
decision_makers: ["agentic-coding-team"]
category: "Deployment and Infrastructure"
nist_controls: ["AC-4", "AC-6", "CM-7", "SC-7", "SC-39"]
impact_level: "low"
ato_relevance: "no"
risk_treatment: "mitigate"
---

# Isolate each sandbox in its own dedicated Podman Machine microVM

## Context and Problem Statement

`ppp` runs AI coding agents in isolated sandboxes so an agent (which executes
untrusted, model-generated commands) cannot reach the developer's host, other
sandboxes, or credentials it should not see. We must choose the isolation
primitive and the mapping between a "sandbox" and that primitive. Docker's `sbx`
(the tool `ppp` clones in spirit) gives each sandbox its own microVM with a
separate Linux kernel; we need an equivalent that has first-class macOS **and**
Windows support without a Docker dependency.

## Decision Drivers

- Strong per-sandbox isolation: a compromised or misbehaving agent must not
  cross into the host or a sibling sandbox (least privilege / boundary
  protection — AC-6, SC-7).
- macOS and Windows are the primary host platforms (see `PROJECT_PLAN.md`).
- "Composition over invention" — delegate isolation to a mature, well-supported
  OSS tool rather than building a VM lifecycle manager.
- Per-sandbox network policy and secret scoping only make sense if sandboxes are
  actually separated at the network/kernel boundary (AC-4).
- Operationally simple lifecycle (`create`/`start`/`stop`/`rm`) that maps cleanly
  onto the `sbx`-style CLI surface.

## Considered Options

1. **Podman Machine, one dedicated VM per sandbox** — each sandbox gets its own
   named Podman Machine microVM (libkrun on macOS, WSL2 on Windows, qemu on
   Linux); the agent runs as a container *inside* that VM.
2. **Lima, one instance per sandbox** — similar per-VM model, more customizable
   guest OS and declarative provisioning.
3. **Shared VM, one container per sandbox** — a single long-lived VM (or the
   host container runtime) with each sandbox as just a container.
4. **Host containers only (no VM)** — sandboxes are plain containers sharing the
   host kernel.

## Decision Outcome

Chosen option: **Podman Machine, one dedicated VM per sandbox**, because it
delivers a separate-kernel isolation boundary with production-grade support on
both macOS and Windows, while letting `ppp` remain a thin orchestrator over a
mature tool.

**This decision establishes a load-bearing invariant: exactly one dedicated
Podman Machine per sandbox; VMs are NEVER shared between sandboxes**, and `ppp`
never reuses Podman's implicit `podman-machine-default`. Every machine `ppp`
manages is `ppp`-named and `ppp`-owned, and `machine_name` maps 1:1 to the
sandbox name (`docs/explorations/ppp-spec.md` §5.1).

Lima (Option 2) was rejected primarily because its Windows/WSL2 support is
documented as experimental/untested, whereas Podman Machine's `wsl` provider is
first-class; Podman also has abundant prior art for the mitmproxy integration
(`docs/explorations/ppp-spec.md` §3.4). Options 3 and 4 were rejected because a shared VM or shared
host kernel collapses the isolation boundary and makes per-sandbox network policy
and secret scoping unenforceable.

### Positive Consequences

- Separate Linux kernel per sandbox (via HVF on macOS, KVM on Linux) — the
  strongest practical desktop isolation boundary.
- Clean 1:1 lifecycle mapping: `ppp run/create` → `podman machine init`;
  `ppp rm` → `podman machine rm`; destroying one sandbox never affects another.
- Per-sandbox WireGuard interface, network policy, and injected secrets are
  coherent because the network stack is not shared.
- No Docker dependency; no host root/admin required.

### Negative Consequences

- Higher per-sandbox resource cost (a full microVM) than a container-per-sandbox
  model — acceptable for a developer-workstation tool.
- **WSL2 caveat:** on Windows the WSL2 kernel is shared across all WSL distros,
  so per-sandbox kernel isolation is weaker than on macOS. Documented limitation
  (`docs/explorations/ppp-spec.md` §3.2); still one VM per sandbox, just a weaker boundary.
- Fedora CoreOS guest is not easily customizable and provisions post-boot via
  SSH rather than declaratively (mitigated in `docs/explorations/ppp-spec.md` §5.2).

### Compliance Consequences

- Supports boundary protection and least privilege (SC-7, AC-6) and information-
  flow enforcement between sandboxes (AC-4) by never sharing a VM.
- Least-functionality posture (CM-7): the isolation model must not be weakened
  for convenience — reflected as a MUST-NEVER in this repo's `AGENTS.md`
  (Prohibited Actions).
- `ppp` is not a FISMA system, so this does not touch an ATO boundary
  (`ato_relevance: no`); the controls are cited for design rigor.

## Links

- `docs/explorations/ppp-spec.md` §5.1 (SandboxVM invariant), §3.2 (Podman Machine), §3.4 (Lima vs
  Podman comparison), §4.1 (topology)
- `AGENTS.md` → Prohibited Actions (never share a Podman Machine VM)
- ADR-0002 (single mitmdump + multi-WireGuard proxy model) — depends on this 1:1
  mapping

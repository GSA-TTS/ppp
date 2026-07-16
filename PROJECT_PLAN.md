---
title: "Project Plan"
description: "Starting point for a new federal coding project — fill this out and let the AI agent set up everything else"
status: canonical
tier: 3
load_priority: reference-only
audience: ["developers", "tech-leads", "managers"]
---

# Project Plan

> **Instructions:** Fill out each section below. Your AI coding agent will use this to automatically set up the repository, generate compliance documentation, and create the initial project structure. Be specific — the more detail you provide, the better the agent can help.

## Project Identity

| Field | Value |
|---|---|
| **Project Name** | ppp — Podman Plus Proxy |
| **Repository Name** | ppp |
| **Organization/Agency** | GSA / GSA-TTS |
| **Project Owner** | agentic-coding-team |
| **Start Date** | 2026-07-16 |
| **Target Completion** | Ongoing |

## Business Objective

`ppp` is an isolated coding-agent sandbox runtime that runs AI coding agents (e.g. `opencode`) inside per-sandbox Linux microVMs with all network egress transparently intercepted and policy-controlled. It composes mature OSS — Podman Machine (microVM per sandbox), mitmproxy in WireGuard mode (transparent TLS interception + policy + credential injection), and the OS keychain (secret storage) — into a thin Go CLI that mirrors Docker `sbx`'s operational surface. The value is that developers can let agents work autonomously while API keys never enter the sandbox, network access is deny-by-default and auditable, and per-sandbox billing codes (USAi charge-back) are supported — all without trusting the agent with the host.

## Tech Stack

| Component | Choice | Rationale |
|---|---|---|
| **Language** | Go 1.22+ (CLI/orchestrator) + embedded Python 3.9+ (mitmproxy addon) | Go for a single static cross-platform binary with a Cobra command tree; Python only where mitmproxy's addon API requires it. |
| **Framework** | Cobra (CLI) + Bubbletea/charmbracelet (TUI for `setup`/`tui`) | Standard Go CLI + TUI stack; matches the `sbx`-style command surface and interactive dashboards. |
| **Database** | None | State is on-disk under XDG dirs (`sandbox.json`, `port-registry.json`, `flows.jsonl`, YAML policies); secrets in OS keychain / `age` fallback. |
| **Cloud/Hosting** | None (local developer-workstation tool) | `ppp` runs entirely on the developer's macOS/Windows/Linux host; there is no deployed service. |
| **CI/CD** | GitHub Actions + GoReleaser | Multi-platform binary builds and GHCR image push for the `opencode` agent container (`.goreleaser.yml`, spec §7). |
| **Container Runtime** | Podman (Podman Machine per sandbox) + mitmproxy/mitmdump | Podman Machine provides the microVM (libkrun/wsl/qemu); mitmdump runs the WireGuard proxy layer. Both are shelled out to, not vendored. |

## Compliance Level

<!-- Check ONE: -->

- [x] **FIPS Low** — Public-facing informational content, no PII, no CUI
- [ ] **FIPS Moderate** — Most federal systems: PII, financial data, internal tools
- [ ] **FIPS High** — National security systems, critical infrastructure

> **Note:** `ppp` is **not a FISMA system** — it is a local developer utility, not a deployed federal information system, so no ATO applies. The FIPS Low box reflects the lowest-touch posture rather than a formal categorization. It nonetheless handles developer credentials (see Data Classification) and follows the universal Federal AI Agent behavioral contract. **Crypto caveat to document:** WireGuard (Curve25519/ChaCha20-Poly1305) and the `age` fallback store are **not FIPS 140-validated**. This is acceptable for a local dev tool but must be surfaced in an ADR so any future use in a controlled environment is a conscious decision.

## Data Classification

<!-- Check all that apply: -->

- [ ] Public data only
- [ ] PII (Personally Identifiable Information)
- [x] CUI (Controlled Unclassified Information)
- [ ] PHI (Protected Health Information)
- [ ] Financial data (FTI, payment info)
- [x] Authentication credentials/secrets

> `ppp`'s core job is handling **authentication credentials/secrets** (API keys for anthropic/openai/github/usai/etc., custom tokens, registry credentials) — stored in the OS keychain or an `age`-encrypted fallback and injected by the proxy so they never enter the sandbox. Developers may also mount workspaces that contain **CUI** into a sandbox; `ppp` must never log, persist, or exfiltrate workspace contents or secret values (secrets are redacted from flow logs per spec §5.4).

## Key Requirements

<!-- List the 3-5 most important functional requirements. These help the agent understand what to build. -->

1. **Isolated sandbox lifecycle** — one Podman Machine microVM per sandbox (separate kernel), driven by an `sbx`-faithful CLI surface (`run`/`create`/`ls`/`stop`/`rm`/`exec`/`cp`/`ports`/`daemon`/`policy`/`secret`/`kit`/`template`/`setup`/`diagnose`/`tui`/`version`/`completion`).
2. **Single-process transparent proxy** — one `mitmdump` process running up to 80 WireGuard server instances (ports 51820–51899); all VM egress tunneled through it with no in-guest proxy config. Sandboxes are identified by the **receiving WG listen port** (cryptographically bound, unspoofable), not the inner tunnel IP.
3. **Host-side network policy** — deny-wins allow/deny rules (glob/regex hosts, CIDR IPs, `**` block-all), global or per-sandbox, enforced in the addon and manageable via `ppp policy`. Presets: allow-all / balanced / deny-all.
4. **Host-side secret injection** — keychain-backed secrets injected on outbound requests; per-sandbox scoping takes precedence over global (USAi charge-back billing-code model); secrets never touch the sandbox filesystem.
5. **Cross-platform** — macOS and Windows primary (Linux best-effort); `opencode` is the only agent at v1 behind an extensible agent registry.

## Constraints

<!-- List any hard constraints the project must work within. -->

- [ ] Must use FedRAMP-authorized services only
- [ ] Must support Section 508 accessibility
- [ ] Must integrate with existing system: <!-- name -->
- [ ] Must support offline/air-gapped operation
- [x] Other:
  - **Composition over invention** — delegate every isolation primitive to a mature OSS tool (Podman Machine, mitmproxy, WireGuard, OS keychain); `ppp` owns only lifecycle glue, policy engine, credential-rewrite layer, and UX.
  - **No host root/admin required** — mitmproxy's WireGuard server is userspace; Podman Machine needs no elevated privileges.
  - **Secrets never enter the sandbox** — enforced by design; the agent sees only sentinel/placeholder values.
  - **macOS + Windows are primary targets** — Linux is best-effort; drove the choice of Podman Machine over Lima.
  - **CLI faithful to Docker `sbx` in spirit** (not binary-compatible), minus `login`/`logout`.
  - **Universal Federal AI Agent behavioral contract applies** — the project layers on `~/.agentic-coding-playbook/AGENTS.md`; the contract probe must pass before work proceeds.

## Team

| Role | Person | Access Level |
|---|---|---|
| Project Owner | agentic-coding-team | Admin |
| Lead Developer | agentic-coding-team | Write |
| Security/ISSO | agentic-coding-team | Read + Review |
| Approving Official | agentic-coding-team | Read |

## Agent Environment

<!-- Where will the AI coding agent run? Check all that apply: -->

- [x] **Local machine** — developer's workstation with CLI access
- [ ] **GitHub Codespace** — cloud-hosted dev environment
- [x] **Sandboxed container** — isolated Docker/Podman environment
- [ ] **CI/CD only** — agent runs in GitHub Actions, no local access

<!-- What services does the agent need access to? Check all that apply: -->

- [x] **GitHub** — push code, create PRs, manage issues
- [ ] **cloud.gov** — deploy applications
- [ ] **workshop.cloud.gov (GitLab)** — alternative code hosting
- [ ] **npm/PyPI** — publish packages
- [x] **Container registry** — push images (GHCR: the `opencode` agent image + release artifacts)

<!-- The `agent-permissions` skill will configure minimal-scope credentials for each checked service. -->

## Implementation Approach

Build `ppp` as a thin Go orchestrator (Cobra command tree under `cmd/ppp` + `internal/` packages: `cli`, `podman`, `proxy`, `policy`, `secret`, `agent`, `sandbox`, `tui`) that shells out to `podman` and `mitmdump` and embeds a Python mitmproxy addon plus a provisioning script. The proxy layer runs a **single** `mitmdump` process hosting a pre-allocated pool of WireGuard server instances; the Go supervisor captures each instance's client config from the mitmdump log (blocks fenced by 60 hyphens), rewrites the hardcoded `Address = 10.0.0.1/32` and `Endpoint` per sandbox, and writes `wg0.conf`. Each sandbox is a Podman Machine VM provisioned over SSH to bring up WireGuard (with an off-tunnel `/32` route to the endpoint to avoid a routing loop), trust the mitmproxy CA, disable IPv6, and run the agent container. The addon identifies sandboxes by the receiving WG **listen port** (not the spoofable inner IP), enforces deny-wins network policy, and injects keychain-stored secrets over a Unix-domain-socket RPC to the Go parent. Development is de-risked by first validating the two load-bearing assumptions (multi-WG port-based identification — already spiked; and in-guest tunnel routing on each provider) before building out the full CLI surface.

## What Happens Next

After you fill out this template and place it in your repository:

1. **The AI agent reads this file** and understands your project
2. **It runs the project-bootstrap skill** which:
   - Creates the directory structure appropriate for your stack
   - Generates AGENTS.md (behavioral contract for AI agents)
   - Copies CODING_PRACTICES.md (secure coding standards)
   - Creates ADR-001 from your implementation approach
   - Generates a risk assessment from your compliance level + data classification
   - Sets up CI/CD workflows for your stack
   - Creates SECURITY.md, CONTRIBUTING.md, LICENSE
3. **You review the generated files** and adjust as needed
4. **Start building** — the agent follows the standards automatically

The entire setup takes about 5 minutes of human input and 2 minutes of agent work.

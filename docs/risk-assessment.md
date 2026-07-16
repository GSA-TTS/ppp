---
title: "AI Agent Risk Assessment Worksheet"
description: "Structured risk assessment template aligned with NIST AI RMF — threat analysis, control assessment, and sign-off"
status: canonical
tier: 3
last_updated: "2026-02-25"
nist_controls: ["RA-3", "RA-5"]
frameworks: ["NIST AI RMF 1.0", "OWASP Top 10 LLM 2025", "OWASP Top 10 Agentic 2026"]
audience: "isso"
keywords: ["risk-assessment", "AI-RMF", "threat-analysis", "OWASP", "ATO"]
related_files: ["docs/SECURITY-CONTROLS.md", "docs/AGENT-IDENTITY.md"]
load_priority: "reference-only"
review_cycle: "semi-annually"
---

<!-- LOAD: reference-only — Load only when performing a risk assessment or preparing ATO documentation. -->

# AI Agent Risk Assessment Worksheet

<!--
  INSTRUCTIONS:
  1. Complete this worksheet before deploying an AI coding agent in your project
  2. Review with your ISSO (Information System Security Officer) or equivalent
  3. Update when the agent, system, or environment changes materially
  4. Retain completed assessments as part of your ATO documentation

  Based on: Agentic Coding Playbook v0.4.0
  Aligned with: NIST AI RMF 1.0 (GOVERN, MAP, MEASURE, MANAGE)
  NIST SP 800-53: RA-3 (Risk Assessment)
-->

> **Scope note:** `ppp` is a **local developer utility, not a FISMA system** —
> no deployed service, no ATO boundary. This worksheet is completed for design
> rigor and to document risk treatment, **not** as ATO documentation. It assesses
> the AI coding agent used to *build* `ppp` (OpenCode on a developer workstation),
> and notes where the risk profile is shaped by what `ppp` *itself* does at
> runtime (handle credentials, mount workspaces). ADRs referenced below live in
> `docs/decisions/`.

---

## Section 1: System Identification

| Field | Value |
|-------|-------|
| **System Name** | ppp (Podman Plus Proxy) |
| **System Owner** | agentic-coding-team (GSA / GSA-TTS) |
| **ISSO** | N/A — not a FISMA system (no ATO boundary, no ISSO of record) |
| **FIPS Impact Level** | [x] Low [ ] Moderate [ ] High |
| **ATO Status** | [ ] Active [ ] In process [ ] Pre-ATO — **N/A (not a FISMA system)** |
| **Assessment Date** | 2026-07-16 |
| **Assessor Name/Title** | agentic-coding-team |
| **Next Review Date** | 2027-01-16 (semi-annual) |

---

## Section 2: AI Agent Identification

<!-- AI RMF: MAP 1 (Context is Established) -->

| Field | Value |
|-------|-------|
| **Agent Name/Product** | OpenCode |
| **Agent Version** | (pinned per developer environment) |
| **Agent Vendor** | OpenCode / opencode.ai |
| **Deployment Model** | [x] Local (runs on developer machine) [ ] Cloud SaaS [ ] Self-hosted |
| **FedRAMP Status** | [ ] Authorized [ ] In process [x] Not applicable [ ] Unknown |
| **Data Residency** | [x] US only [ ] International [ ] Unknown |
| **Training Data Opt-Out** | [ ] Confirmed opt-out [ ] Opt-out not available [x] Unknown |

### Agent Capabilities

Check all capabilities the agent will use in this project:

- [x] Code generation and modification
- [x] File system read access
- [x] File system write access
- [x] Command/shell execution
- [x] Network access (external) — reading public docs/registries; installs & pushes gated on approval
- [ ] Network access (internal)
- [ ] Database access
- [x] Git operations (commit, push) — **push gated on human approval**
- [x] CI/CD pipeline interaction — modifications gated on approval
- [x] Package/dependency installation — **gated on human approval**
- [ ] Infrastructure management — real Podman Machine VM ops gated on approval
- [x] Other: run read-only local diagnostics and throwaway spikes in a pre-approved temp dir

---

## Section 3: Data Classification

<!-- AI RMF: MAP 5 (Impacts to Individuals, Groups, Communities) -->
<!-- NIST SP 800-53: RA-2 (Security Categorization) -->

### 3.1 Data Types Accessible to the Agent

| Data Type | Present? | Classification | Agent Needs Access? |
|-----------|----------|---------------|-------------------|
| Source code | [x] Yes [ ] No | Public (open-source dev tool) | [x] Yes [ ] No |
| Configuration files | [x] Yes [ ] No | Public | [x] Yes [ ] No |
| Environment variables | [x] Yes [ ] No | Sensitive (may carry tokens) | [ ] Yes [x] No |
| API keys/tokens/secrets | [x] Yes [ ] No | Secret | [ ] Yes [x] No (dev agent must not read real keys w/o approval) |
| PII (names, SSN, etc.) | [ ] Yes [x] No | — | [ ] Yes [x] No |
| PHI (health records) | [ ] Yes [x] No | — | [ ] Yes [x] No |
| Financial data | [ ] Yes [x] No | — | [ ] Yes [x] No |
| CUI (Controlled Unclassified) | [x] Yes [ ] No | CUI (possible in runtime workspace mounts) | [ ] Yes [x] No (dev agent does not process runtime workspaces) |
| Classified data | [ ] Yes [x] No | — | [ ] Yes [x] No |
| Internal network info | [ ] Yes [x] No | — | [ ] Yes [x] No |
| User credentials | [x] Yes [ ] No | Secret (keychain / age fallback) | [ ] Yes [x] No |
| Test/sample data | [x] Yes [ ] No | Public (no real secrets in fixtures) | [x] Yes [ ] No |

### 3.2 Data Flow

Where does data go when the agent processes it?

| Destination | Authorized? | Encrypted? |
|------------|-------------|------------|
| Agent vendor cloud (prompts/code) | [ ] Yes [ ] No | [ ] Yes [ ] No |
| Agent vendor training pipeline | [ ] Yes [ ] No [ ] Opted out | N/A |
| Local file system | [ ] Yes [ ] No | [ ] Yes [ ] No |
| Version control (remote) | [ ] Yes [ ] No | [ ] Yes [ ] No |
| CI/CD system | [ ] Yes [ ] No | [ ] Yes [ ] No |
| External APIs | [ ] Yes [ ] No | [ ] Yes [ ] No |
| Log aggregation system | [ ] Yes [ ] No | [ ] Yes [ ] No |

---

## Section 4: Threat Analysis

<!-- AI RMF: MAP 2 (Categorization of AI System), MEASURE 1 (Metrics) -->
<!-- Aligned with: OWASP Top 10 for LLM Applications 2025, OWASP Top 10 for Agentic Applications 2026 -->

Rate each threat for your specific deployment. **Likelihood**: 1 (Rare) to 5 (Almost Certain). **Impact**: 1 (Negligible) to 5 (Severe). **Risk = Likelihood x Impact**.

| # | Threat | OWASP Ref | Likelihood (1-5) | Impact (1-5) | Risk Score | Existing Mitigations | Residual Risk |
|---|--------|-----------|-------------------|--------------|------------|---------------------|---------------|
| T1 | **Prompt injection** — Malicious input causes agent to take unauthorized actions | LLM01, Agentic-01 | 2 | 3 | 6 (Med) | Universal contract §11 (treat repo/issue/workspace/traffic as untrusted data); approval gates on push/deps/CI; AGENTS.md prohibited actions | 3 (Low) |
| T2 | **Sensitive data disclosure** — Agent exposes secrets, PII, or CUI in output | LLM02 | 2 | 4 | 8 (Med) | Dev agent must not read real keys w/o approval; secrets in keychain/age not code; gitleaks pre-commit; runtime redacts secrets from flows.jsonl (spec §5.4) | 4 (Low) |
| T3 | **Supply chain compromise** — Agent installs malicious or vulnerable dependency | LLM03, Agentic-07 | 2 | 4 | 8 (Med) | Dependency installs gated on approval; pinned versions + checksums; SCA in CI; approved-registry policy (AGENTS.md) | 4 (Low) |
| T4 | **Insecure code generation** — Agent produces code with vulnerabilities | LLM05 | 2 | 3 | 6 (Med) | Coding standards; input-validation & argv-not-shell rules (AGENTS.md); code review; lint/vet in CI | 3 (Low) |
| T5 | **Excessive agency** — Agent performs actions beyond intended scope | LLM06, Agentic-06 | 2 | 4 | 8 (Med) | Explicit approval gates (VM ops, keychain, push, deps, CI); MUST-NEVER list incl. weakening sandbox isolation | 4 (Low) |
| T6 | **Credential compromise** — Agent token or credentials are exposed or stolen | Agentic-02 | 2 | 4 | 8 (Med) | No real keys in repo/fixtures; keychain-first storage; least-scope service creds; push/publish gated | 4 (Low) |
| T7 | **Unauthorized code execution** — Agent executes untrusted code from external source | Agentic-03 | 2 | 3 | 6 (Med) | Spikes confined to pre-approved temp dir creating no sandboxes/secrets; approval for real VM ops | 3 (Low) |
| T8 | **Context/memory poisoning** — Agent's context is manipulated to influence behavior | Agentic-08 | 2 | 3 | 6 (Med) | Untrusted-input rule (§11); design decisions captured in ADRs, not ephemeral context | 3 (Low) |
| T9 | **Audit trail gaps** — Agent actions cannot be reconstructed from logs | Agentic-02 | 2 | 2 | 4 (Low) | Git history + PR record as audit trail; AI attribution trailer; contract probe in pre-commit + CI | 2 (Low) |
| T10 | **Human trust exploitation** — User over-trusts agent output without review | Agentic-05 | 3 | 3 | 9 (Med) | Human review required for commits/push; approval gates; verification-loop expectation (contract §14) | 4 (Low) |

### Risk Tolerance

| Risk Level | Score Range | Action Required |
|-----------|-------------|-----------------|
| **Critical** | 20-25 | MUST mitigate before agent deployment |
| **High** | 12-19 | MUST mitigate within 30 days of deployment |
| **Medium** | 6-11 | SHOULD mitigate; document accepted risk if deferred |
| **Low** | 1-5 | MAY accept with documentation |

---

## Section 5: Control Assessment

<!-- AI RMF: MANAGE 1 (Risk Treatments), MANAGE 2 (Risk Treatments Managed) -->

For each control area, assess your current implementation status.

| Control Area | Status | Notes |
|-------------|--------|-------|
| **Agent Identity** — Agent has distinct identity, separate from user | [x] Implemented [ ] Partial [ ] Not implemented | `Co-authored-by:` trailer for AI commits; PR AI disclosure (AGENTS.md §Identity) |
| **Least Privilege** — Agent permissions scoped to minimum required | [x] Implemented [ ] Partial [ ] Not implemented | Permitted/Approval/Prohibited action lists in AGENTS.md |
| **Human-in-the-Loop** — Destructive/sensitive actions require approval | [x] Implemented [ ] Partial [ ] Not implemented | Approval gates: push, deps, CI, VM ops, keychain, publish |
| **Audit Logging** — All agent actions logged with attribution | [ ] Implemented [x] Partial [ ] Not implemented | Git history + PR record; no dedicated SIEM (local dev tool) |
| **Secrets Scanning** — Pre-commit hooks prevent credential leaks | [x] Implemented [ ] Partial [ ] Not implemented | gitleaks in `.pre-commit-config.yaml` |
| **SAST/SCA** — Agent code scanned for vulnerabilities in CI | [ ] Implemented [x] Partial [ ] Not implemented | SCA + lint planned in CI; SAST TBD (see checklists/pre-deployment.md) |
| **Branch Protection** — Agent cannot push directly to protected branches | [ ] Implemented [x] Partial [ ] Not implemented | Policy stated (main requires review, no force-push); enable once GitHub remote exists |
| **Dependency Scanning** — Agent-installed packages scanned for CVEs | [ ] Implemented [x] Partial [ ] Not implemented | Pinned versions + SCA planned; installs gated on approval |
| **Session Management** — Agent sessions timeout, no credential persistence | [x] Implemented [ ] Partial [ ] Not implemented | Local tool; no persistent agent service or long-lived tokens |
| **Incident Response** — IR plan covers agent-specific scenarios | [ ] Implemented [ ] Partial [x] Not implemented | N/A for a local non-FISMA dev tool; rely on git revert/branch protection |
| **Data Handling** — Agent cannot access/transmit unauthorized data | [x] Implemented [ ] Partial [ ] Not implemented | Keychain/age storage; no real keys in repo; untrusted-input rule |
| **Configuration Management** — Agent config version-controlled, reviewed | [x] Implemented [ ] Partial [ ] Not implemented | `AGENTS.md`, `opencode.jsonc`, ADRs in `docs/decisions/` all in VCS |

---

## Section 6: Risk Treatment Plan

For each risk rated Medium or above, document the treatment plan.

### Risk: T10 — Human trust exploitation (highest residual driver)

| Field | Value |
|-------|-------|
| **Risk Score** | 9 (Medium) |
| **Treatment** | [x] Mitigate [ ] Transfer [ ] Accept [ ] Avoid |
| **Planned Controls** | Human review required before commit/push; approval gates on all state-changing actions; verification-loop expectation (contract §14); no auto-merge |
| **Responsible Party** | agentic-coding-team |
| **Target Completion** | Ongoing (enforced by branch protection once GitHub remote exists) |
| **Verification Method** | PR review record; contract probe in pre-commit + CI |

### Risk: Non-FIPS-validated cryptography (WireGuard tunnel + `age` fallback store)

| Field | Value |
|-------|-------|
| **Risk Score** | Low (local, non-FISMA tool; host-local tunnel; keychain-first) |
| **Treatment** | [ ] Mitigate [ ] Transfer [x] Accept [ ] Avoid |
| **Planned Controls** | Documented in **ADR-0004**; OS keychain is primary store; `age` only where no keychain; strong modern primitives (Curve25519/ChaCha20-Poly1305) |
| **Responsible Party** | agentic-coding-team |
| **Target Completion** | N/A (accepted) |
| **Verification Method** | Scope-guard revisit trigger in ADR-0004 — MUST re-open if `ppp` ever becomes a deployed/FISMA/ATO-bound system |

*(Design-level residual risks — WG endpoint routing loop, single-mitmdump blast
radius, first-peer fallback under load — are tracked in `docs/explorations/ppp-spec.md` §13 Open
Risks and the relevant ADRs, not duplicated here.)*

---

## Section 7: Acceptance and Sign-Off

### Risk Acceptance Statement

Based on this assessment, the residual risk of using **OpenCode** to develop
**ppp (Podman Plus Proxy)** is:

[x] **Acceptable** — Proceed under the documented controls (all threats reduced to Low residual; non-FIPS crypto formally accepted per ADR-0004). `ppp` is not a FISMA system; no ATO sign-off applies.
[ ] **Conditionally Acceptable** — Proceed after completing the risk treatment plan items marked as pre-deployment requirements
[ ] **Not Acceptable** — Do not deploy until identified risks are mitigated

### Signatures

| Role | Name | Signature | Date |
|------|------|-----------|------|
| **System Owner** | agentic-coding-team | (not a FISMA system — informal) | 2026-07-16 |
| **ISSO** | N/A | N/A | — |
| **Authorizing Official** (if required) | N/A (no ATO) | N/A | — |

---

## Appendix: Revision History

| Date | Version | Assessor | Changes |
|------|---------|----------|---------|
| 2026-07-16 | 1.0 | agentic-coding-team | Initial assessment (local dev tool; FIPS Low / not-a-FISMA-system; non-FIPS crypto accepted per ADR-0004) |

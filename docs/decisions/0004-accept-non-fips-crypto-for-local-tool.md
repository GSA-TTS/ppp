---
title: "Accept non-FIPS-validated crypto (WireGuard, age) for the local dev tool"
status: "accepted"
date: "2026-07-16"
decision_makers: ["agentic-coding-team"]
category: "Cryptography"
nist_controls: ["SC-8", "SC-12", "SC-13", "SC-28", "IA-5"]
impact_level: "low"
ato_relevance: "no"
risk_treatment: "accept"
---

# Accept non-FIPS-validated crypto (WireGuard, age) for the local dev tool

## Context and Problem Statement

`ppp` relies on cryptography in two places: (1) the transparent tunnel between
each sandbox VM and the host proxy uses **WireGuard**, as implemented by
mitmproxy's userspace server; and (2) the fallback secret store, used only when
no OS keychain backend is available, is an **`age`-encrypted** file. Both use
modern elliptic-curve key exchange with an authenticated stream cipher, but
neither WireGuard's nor `age`'s primitives are provided through a FIPS 140-2/3
validated cryptographic module. The universal behavioral contract defaults to FIPS
Moderate and prefers FIPS-validated crypto, so we must consciously record the
posture for this project.

## Decision Drivers

- `ppp` is a **local developer utility, not a FISMA system** — no deployed
  service, no ATO boundary, no production federal data (`PROJECT_PLAN.md`).
- The crypto choices are dictated by the OSS tools `ppp` composes: WireGuard is
  intrinsic to mitmproxy's transparent WireGuard mode; `age` is a widely-used,
  audited file-encryption tool.
- The **primary** secret store is the OS keychain (macOS Keychain / Windows
  Credential Manager / Linux Secret Service); `age` is only a fallback.
- The tunnel is host-local (guest VM ↔ host proxy on the same machine), not a
  network-exposed channel.
- Preference for FIPS-validated modules where crypto protects federal data at
  rest/in transit (SC-13) — must be weighed against the above.

## Considered Options

1. **Accept WireGuard + `age` as-is**, documenting the non-FIPS posture and the
   local-tool scope; rely on the OS keychain as the primary secret store.
2. **Require FIPS-validated crypto everywhere** — replace/​wrap the tunnel and
   fallback store with FIPS 140-validated modules.
3. **Drop the `age` fallback** — require an OS keychain; fail closed where none
   exists.

## Decision Outcome

Chosen option: **accept WireGuard + `age` as-is** for this local developer tool,
with the non-FIPS posture explicitly documented and the OS keychain as the
primary secret store.

Option 2 is rejected as disproportionate: the tunnel crypto is inseparable from
mitmproxy's WireGuard mode (replacing it means abandoning the core architecture
in ADR-0002), and there is no production federal data or ATO boundary that would
require FIPS validation here. Option 3 is rejected because a keychain is not
always available (headless/CI/Linux-without-Secret-Service), and failing closed
would break legitimate local use; the `age` fallback is a reasonable,
encrypted-at-rest compromise (SC-28).

**Scope guard:** this acceptance is valid **only** while `ppp` remains a local,
non-FISMA developer tool. If `ppp` were ever deployed as a service, used to
protect federal data in a controlled environment, or brought under an ATO
boundary, this decision MUST be revisited (superseded by a new ADR) and the FIPS
requirement re-evaluated.

### Positive Consequences

- Keeps `ppp` a thin composition of mature OSS (WireGuard via mitmproxy, `age`)
  without inventing or wrapping crypto.
- Strong, modern primitives (Curve25519 / ChaCha20-Poly1305) protect the tunnel
  and the fallback store even though the modules aren't FIPS-validated.
- Secrets at rest primarily live in the OS-native keychain (SC-28, IA-5).

### Negative Consequences

- Crypto is **not** FIPS 140 validated (SC-13 not satisfied in the validated
  sense) — unacceptable if the scope guard above is ever violated.
- Introduces a documented deviation from the universal contract's FIPS-Moderate
  default that reviewers must be aware of.

### Compliance Consequences

- Records a conscious **risk acceptance** (`risk_treatment: accept`) for SC-13
  (non-validated module) given the local-tool, non-FISMA scope
  (`ato_relevance: no`).
- In-transit (SC-8) and at-rest (SC-28) confidentiality are still provided by
  strong primitives; key management (SC-12) for the tunnel is per-sandbox
  keypairs (ADR-0002/0003), and secret storage (IA-5) prefers the OS keychain.
- Establishes a hard revisit trigger if scope changes (see Scope guard).

## Links

- `docs/explorations/ppp-spec.md` §5.6 (Secret storage: keychain + `age` fallback), §3.1
  (WireGuard), `PROJECT_PLAN.md` (Compliance Level note on non-FIPS crypto)
- ADR-0002 (single mitmdump + multi-WireGuard) — source of the WireGuard dependency
- `AGENTS.md` → Data Handling (approved storage: keychain, `age` fallback)

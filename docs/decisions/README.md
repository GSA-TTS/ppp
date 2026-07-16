# Architecture Decision Records

Decision records for `ppp` (Podman Plus Proxy), in [MADR](https://adr.github.io/madr/)
format with federal compliance frontmatter extensions. Architecture-affecting
changes to the isolation model, proxy topology, sandbox identity, or crypto
posture REQUIRE an ADR here (see `AGENTS.md` → Engineering Discipline).

The authoritative design source is `docs/explorations/ppp-spec.md`; these ADRs record *why* the
load-bearing choices were made.

| # | Title | Status | Category | Risk Treatment |
|---|-------|--------|----------|----------------|
| [0001](0001-one-podman-machine-per-sandbox.md) | Isolate each sandbox in its own dedicated Podman Machine microVM | accepted | Deployment and Infrastructure | mitigate |
| [0002](0002-single-mitmdump-multi-wireguard.md) | Use a single mitmdump process hosting one WireGuard instance per sandbox | accepted | Deployment and Infrastructure | mitigate |
| [0003](0003-sandbox-identity-by-wireguard-port.md) | Identify a sandbox by its WireGuard listen port, not its inner tunnel IP | accepted | Authentication and Identity | mitigate |
| [0004](0004-accept-non-fips-crypto-for-local-tool.md) | Accept non-FIPS-validated crypto (WireGuard, age) for the local dev tool | accepted | Cryptography | accept |

## Relationships

- **0001** establishes the strict 1:1 sandbox ↔ Podman Machine invariant (VMs
  never shared).
- **0002** builds the proxy layer on top of that 1:1 mapping.
- **0003** picks the sandbox identifier used by 0002's addon.
- **0004** records the crypto posture for the tunnel (from 0002) and the secret
  fallback store, with a scope-guard revisit trigger.

## Conventions

- Filename: `NNNN-kebab-case-title.md` (zero-padded sequential).
- Status lifecycle: `proposed → accepted → deprecated | superseded`.
- Superseding an ADR sets `superseded_by:` on the old one and `supersedes:` on
  the new one; never edit an accepted decision's outcome in place.

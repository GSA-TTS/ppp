---
title: "Compose the proxy's upstream TLS trust from the OS trust store plus vendored interception roots"
status: "accepted"
date: "2026-07-17"
decision_makers: ["agentic-coding-team"]
category: "Cryptography"
nist_controls: ["SC-8", "SC-13", "SC-23", "SI-4"]
impact_level: "low"
ato_relevance: "no"
risk_treatment: "accept"
---

# Compose the proxy's upstream TLS trust from the OS trust store plus vendored interception roots

## Context and Problem Statement

`ppp`'s single `mitmdump` process makes the **upstream** TLS connection to real
servers on the sandbox's behalf (the sandbox→proxy leg is the mitmproxy CA; the
proxy→server leg is normal TLS). mitmproxy is Python/OpenSSL, so it verifies
upstream certificates against an OpenSSL PEM bundle — **not** the OS system trust
store. Two problems follow on real hosts:

1. **macOS**: the system roots live in the Keychain, which OpenSSL does not read.
   mitmproxy's default bundle can miss roots that `curl` (system trust) has, so
   upstream verification fails with `unable to get local issuer certificate`
   (observed: HTTPS through the tunnel returned 502 while host `curl` returned
   200).
2. **TLS-inspecting networks (e.g. GSA's Zscaler)**: the real server cert is
   replaced by one issued by a corporate interception root (observed issuer:
   "Zscaler Intermediate Root CA"). Unless mitmproxy trusts that root, every
   upstream handshake fails — even though the host and the dev container already
   trust it.

We must decide what CA material mitmproxy uses to verify upstream TLS, without
silently disabling verification.

## Decision Drivers

- Upstream verification must actually work on the two environments ppp targets:
  a plain macOS host and a Zscaler-inspected GSA host.
- We must NOT weaken verification (never `ssl_insecure`); a failure to find a
  root should be fixed by *adding the right root*, not by turning verification
  off (SC-8, SC-13, SC-23).
- The dev container already vendors the public Zscaler Root CA
  (`.devcontainer/certs/ZscalerRootCA.crt`, ADR pending) and trusts it with zero
  user action; the runtime should be symmetric so GSA users need no config.
- Reproducible/pinned material where possible (AGENTS.md).
- External contributors on a normal network must not be forced to trust a
  corporate interception root they are not behind.

## Considered Options

1. **Compose a bundle at `daemon start`**: concatenate the host OS trust store
   (exported to PEM) + any vendored/again-configured interception roots +
   certifi, write it to `$PPP_DATA`, and pass it via
   `--set ssl_verify_upstream_trusted_ca=<bundle>`. Allow a `PPP_UPSTREAM_CA`
   override.
2. **Point at a single well-known bundle path** (e.g. `/etc/ssl/cert.pem`). Fails
   on macOS (Keychain, not PEM) and on Zscaler (root not in that bundle).
3. **Set `ssl_insecure`** (disable upstream verification). Rejected outright:
   makes the proxy blind to upstream MITM, defeating the point of the audited
   egress path.
4. **Require the user to set `PPP_UPSTREAM_CA`** every time. Works but is not
   zero-config for the primary (GSA/macOS) users.

## Decision Outcome

Chosen option: **compose the upstream trust bundle at `daemon start`** (Option
1). `ppp` builds a PEM bundle from, in order:

1. an explicit `PPP_UPSTREAM_CA` file if set (full override); otherwise
2. the **host OS trust store** exported to PEM (macOS: `security
   find-certificate`/`export`; Linux: the system `ca-certificates` bundle), plus
3. any **vendored interception roots** ppp ships (the public Zscaler Root CA,
   matching the dev container), plus
4. **certifi**/a baseline public-root bundle as a floor.

The composed bundle is written under `$PPP_DATA` and passed to mitmproxy via
`ssl_verify_upstream_trusted_ca`. Verification stays **on**. On a normal network
the interception root is simply never encountered (inert); on Zscaler it makes
upstream verification succeed with no user action.

Option 2 is insufficient (fails on both target environments); Option 3 is
unacceptable (disables verification); Option 4 is a worse UX than Option 1 while
Option 1 still supports the same override for edge cases.

### Positive Consequences

- Upstream TLS works out of the box on plain macOS and on Zscaler'd GSA hosts.
- Verification is never disabled; a missing root is fixed by adding a root.
- Symmetric with the dev container's CA handling; zero-config for GSA users.
- `PPP_UPSTREAM_CA` remains as an explicit escape hatch.

### Negative Consequences

- On an inspected network, mitmproxy (like every other client there) cannot
  distinguish the real server from the interceptor — an inherent property of
  TLS inspection, not something ppp introduces (SI-4 visibility caveat).
- Trusting a vendored interception root by default is a conscious posture; it is
  the *public* corporate root (not a private key), inert off-network, and
  documented here.
- Exporting the OS trust store to PEM is platform-specific glue to maintain.

### Compliance Consequences

- Preserves upstream transport authenticity/verification (SC-8, SC-23, SC-13):
  ppp never sets `ssl_insecure`.
- Records a conscious acceptance (`risk_treatment: accept`) that on a
  TLS-inspecting network the proxy trusts the interception root, consistent with
  the host's own posture. Not a FISMA system (`ato_relevance: no`).

## Links

- Live smoke test (issue #26): HTTPS-through-tunnel 502 traced to mitmproxy
  upstream trust missing the Zscaler interception root.
- ADR-0004 (non-FIPS crypto acceptance) and the dev-container Zscaler CA vendoring
  (`.devcontainer/certs/ZscalerRootCA.crt`).
- `docs/explorations/ppp-spec.md` §5.3 (ProxySup).

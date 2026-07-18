---
title: "Verify upstream TLS against the host OS trust store"
status: "accepted"
date: "2026-07-17"
decision_makers: ["agentic-coding-team"]
category: "Cryptography"
nist_controls: ["SC-8", "SC-13", "SC-23", "SI-4"]
impact_level: "low"
ato_relevance: "no"
risk_treatment: "accept"
supersedes: []
---

# Verify upstream TLS against the host OS trust store

## Context and Problem Statement

`ppp`'s single `mitmdump` process makes the **upstream** TLS connection to real
servers on the sandbox's behalf (the sandbox→proxy leg uses the mitmproxy CA;
the proxy→server leg is normal TLS that must be verified). mitmproxy verifies the
upstream leg against a **PEM bundle only** — `ssl_verify_upstream_trusted_ca` /
`ssl_verify_upstream_trusted_confdir`, defaulting to `certifi` — and **never
consults the OS trust store** (not the macOS Keychain, not the Windows store; it
does not even call OpenSSL's default-verify-paths).

On a TLS-inspecting network (e.g. GSA's Zscaler) the real server certificate is
replaced by a chain that terminates at a corporate interception root. Because
that root is in the host OS trust store but **not** in certifi, mitmproxy has no
anchor for the intercepted chain and every upstream handshake fails with
`X509_V_ERR_UNABLE_TO_GET_ISSUER_CERT_LOCALLY` (errno 20) → the sandbox gets a
502 for all outbound HTTPS. The host's own clients (browser, `curl`) succeed on
the same network because they validate against the OS trust store, which trusts
the interception CA.

We must make upstream verification work on such networks **without ever
disabling verification** and without trusting anything the host does not.

## Decision Drivers

- Upstream verification must work on a plain host and on a TLS-inspecting host,
  with no per-user configuration.
- **Never weaken verification** (never `ssl_insecure`): expiry, hostname
  mismatch, self-signed, untrusted-root, and signature failures must still be
  rejected (SC-8, SC-13, SC-23).
- Trust exactly what the **host OS trust store** already vouches for — do not
  invent, vendor, or blanket-trust interception certs.
- Prefer the simplest mechanism with no coupling to mitmproxy internals.

## Considered Options

1. **Hand mitmproxy the host OS trust store as its upstream bundle** (chosen):
   export the OS store to PEM and pass it via `ssl_verify_upstream_trusted_ca`.
2. **`ssl_insecure`** (disable upstream verification) — rejected outright.
3. **Filter the OS store + a `tls_start_server` verify callback with
   `X509_V_FLAG_PARTIAL_CHAIN`** — the original implementation; later removed as
   unnecessary (see "Correction" below).

## Decision Outcome

Chosen option: **compose the upstream CA bundle from the host OS trust store,
verbatim, and let mitmproxy's default verification do the rest.**

- `internal/catrust` exports the OS trust store to PEM (macOS: `security
  find-certificate` over the System + SystemRoot keychains; Linux: the system
  `ca-certificates` bundle). `PPP_UPSTREAM_CA` overrides it.
- The supervisor writes it to `$PPP_DATA/wg/upstream-ca.pem` and passes it to
  mitmdump via `--set ssl_verify_upstream_trusted_ca=…`.
- No cert filtering, no `X509_V_FLAG_PARTIAL_CHAIN`, and **no custom verify
  callback**. mitmproxy's stock (non-strict) OpenSSL verification anchors the
  intercepted chain at the interception root the host already trusts.

Verified on a real macOS/libkrun host behind Zscaler: legitimate hosts
(example.com, google, github) succeed and are intercepted; `self-signed`,
`untrusted-root`, `expired`, and `wrong.host` badssl endpoints are still
rejected. The security floor is intact because the bundle is exactly the host's
own trust set — nothing more.

Option 2 disables verification. Option 3 was implemented first and then removed
(below).

### Correction — why the partial-chain callback was removed (post-rc1)

The first implementation (a) **dropped** CA certs with a non-critical
`BasicConstraints` from the exported bundle and (b) re-admitted the interception
chain via an addon `tls_start_server` verify callback that set
`X509_V_FLAG_PARTIAL_CHAIN` and authorized the presented chain against the OS
store. This rested on the belief that *OpenSSL 3 rejects a non-critical-
BasicConstraints CA by default*.

A fact-check and a live spike disproved that premise:

- OpenSSL's **default** (non-strict) verification **accepts** a non-critical-
  BasicConstraints CA as a trust anchor. The criticality rejection
  (`X509_V_ERR_CA_BCONS_NOT_CRITICAL`, errno 89) fires **only** under
  `X509_V_FLAG_X509_STRICT`, which mitmproxy never sets.
- The observed failure (errno 20) was therefore **self-inflicted**: dropping the
  root removed the only usable anchor, and the callback existed solely to
  compensate for that drop.

So both the filter and the callback were removed. The current design keeps the
OS store as-is and relies on mitmproxy's default verification — simpler, and with
no coupling to mitmproxy TLS internals.

### Positive Consequences

- Upstream TLS works out of the box on plain and TLS-inspecting hosts, zero
  config, self-healing across interception-cert rotation (the bundle is whatever
  the host currently trusts).
- Verification is never disabled; bad certs are still rejected.
- No vendored/probed interception cert; no `podman`/mitmproxy-internals coupling;
  no golden-hash drift test to maintain.

### Negative Consequences

- On an inspected network the proxy, like every other client there, cannot
  distinguish the real server from the interceptor — inherent to TLS inspection,
  not introduced by ppp (SI-4 caveat).
- The bundle trusts every root the host trusts (including legacy roots) for the
  upstream leg — the same posture as the host's own clients; acceptable.
- Exporting the OS store (macOS `security`, Linux bundle) is platform-specific
  glue; failure to read it makes `daemon start` fail loudly (safe direction).

### Compliance Consequences

- Preserves upstream transport authenticity/verification (SC-8, SC-23, SC-13):
  ppp never sets `ssl_insecure`; it trusts exactly the host's own trust set.
- Records a conscious acceptance (`risk_treatment: accept`) that on a
  TLS-inspecting network the proxy trusts the same interception the host already
  trusts (SI-4). Not a FISMA system (`ato_relevance: no`).

## Links

- Live validation (issue #26/#27): Zscaler network — legit hosts 200 intercepted,
  all bad-cert `badssl.com` cases rejected.
- Spikes/fact-check: `docs/explorations/research/spike-root-in-bundle.md` (root
  kept → default verify succeeds, no callback), plus the mitmproxy behavior
  fact-check.
- `internal/catrust` (OS-store export); `internal/cli/supervisor.go`
  (`writeUpstreamCABundle`); `docs/notes/mitmproxy-partial-chain-request.md`
  (upstream FR — now a convenience "use the OS trust store" ask, not a
  correctness workaround).
- ADR-0004 (non-FIPS crypto acceptance).

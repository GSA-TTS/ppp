---
title: "Verify upstream TLS via the OS trust store, authorizing interception chains at handshake time"
status: "accepted"
date: "2026-07-17"
decision_makers: ["agentic-coding-team"]
category: "Cryptography"
nist_controls: ["SC-8", "SC-13", "SC-23", "SI-4"]
impact_level: "low"
ato_relevance: "no"
risk_treatment: "accept"
---

# Verify upstream TLS via the OS trust store, authorizing interception chains at handshake time

## Context and Problem Statement

`ppp`'s single `mitmdump` process makes the **upstream** TLS connection to real
servers on the sandbox's behalf (the sandbox→proxy leg uses the mitmproxy CA;
the proxy→server leg is normal TLS that must be verified). mitmproxy is
Python/OpenSSL 3, which verifies against an OpenSSL PEM bundle — **not** the OS
system trust store. Two problems follow on real hosts:

1. **macOS**: system roots live in the Keychain, which OpenSSL does not read, so
   mitmproxy's default bundle can miss roots the host itself trusts.
2. **TLS-inspecting networks (e.g. GSA's Zscaler)**: the real server cert is
   replaced by a chain that terminates at a corporate interception **root**. On
   the observed GSA network that root (`Zscaler Root CA`, 2014) is
   **non-conformant** — its BasicConstraints extension is not marked *critical* —
   and OpenSSL 3 strict-rejects it as a trust anchor. The host's own verifier
   (macOS Security) tolerates it, which is why host `curl` works while mitmproxy
   fails every upstream handshake with `certificate verify failed` → the sandbox
   gets a 502 for all outbound HTTPS.

We must make upstream verification work on both environments **without ever
disabling verification** and without trusting anything the host does not.

## Decision Drivers

- Upstream verification must work on a plain macOS host and a Zscaler-inspected
  GSA host, with no per-user configuration.
- **Never weaken verification** (never `ssl_insecure`): expiry, hostname
  mismatch, revocation, self-signed, and untrusted-root failures must still be
  rejected (SC-8, SC-13, SC-23).
- Trust only what the **host OS trust store** already vouches for — do not
  invent, vendor, or blanket-trust interception certs.
- Robust to interception-cert **rotation** (the observed GSA intermediate rotates
  on a ~2-week cycle) with no scheduled maintenance.
- Avoid brittle mechanisms: no stale vendored cert, no per-start network probe,
  no `ppp setup` step, minimal coupling to mitmproxy internals.

## Considered Options

1. **Vendored interception root/intermediate** in the bundle — rejected: the
   intermediate rotates (~2 weeks), so a vendored copy goes stale silently.
2. **Per-`daemon-start` network probe** to harvest the interception intermediate
   + `X509_V_FLAG_PARTIAL_CHAIN` — rejected: depends on connectivity at start,
   hardcodes a probe host, and blanket-trusting a probed/presented CA was shown
   (spike) to wrongly accept `untrusted-root.badssl.com`.
3. **`ssl_insecure`** (disable upstream verification) — rejected outright.
4. **Handshake-time verify callback authorized by the OS trust store** (chosen).

## Decision Outcome

Chosen option: **a scoped OpenSSL verify callback, installed on the upstream
connection, that authorizes an interception chain against the host OS trust
store at handshake time.**

Implementation:

- The upstream CA bundle passed to mitmproxy (`ssl_verify_upstream_trusted_ca`)
  is simply the **host OS trust store, minus CA certs OpenSSL 3 rejects**
  (non-critical BasicConstraints). Normal public chains verify against it
  unchanged (`internal/catrust`).
- The addon's `tls_start_server` hook builds the upstream connection using
  mitmproxy's own `create_proxy_server_context` (identical cipher/version/verify
  policy) and installs a **verify callback**. The callback defers entirely to
  OpenSSL's verdict, EXCEPT it tolerates a narrow allowlist of "cannot reach a
  usable trust anchor" error codes
  (`UNABLE_TO_GET_ISSUER_CERT{,_LOCALLY}`, `NO_ISSUER_PUBLIC_KEY`) **and only when the
  presented chain is cryptographically AUTHORIZED by the host OS trust store** —
  i.e. some presented CA was actually *signature-verified* as issued by a cert
  the host already trusts. SNI + hostname verification mirror mitmproxy's
  `TlsConfig.tls_start_server` exactly (`DEFAULT_HOSTFLAGS`).

Consequences of this design:

- A **normal public chain** verifies through OpenSSL's standard path; the
  callback never fires (no tolerated error occurs).
- An **interception chain the host trusts** is accepted: the non-conformant root
  causes a tolerated error, and the chain is authorized because the presented
  interception intermediate is signature-verified as issued by a root already in
  the OS store.
- **Genuinely bad certs are still rejected** — verified live on a Zscaler
  network: `self-signed`, `untrusted-root`, `expired`, and `wrong.host`
  `badssl.com` endpoints all return 502, while `example.com`, `www.google.com`,
  and `api.github.com` return 200.
- **Rotation-proof and probe-free**: authorization is computed per connection
  from the chain in hand plus the host trust store; nothing is cached, vendored,
  or fetched from a side channel.

Option 1 goes stale; Option 2 was shown to over-trust and depends on start-time
connectivity; Option 3 disables verification. Only the callback both works on
the inspected network and preserves full rejection of illegitimate certs.

### Positive Consequences

- Upstream TLS works out of the box on plain macOS and Zscaler'd GSA hosts, zero
  config, and self-heals across interception-cert rotation.
- Verification is never disabled; the callback can only *tolerate* a specific
  "no usable anchor" failure for a chain the host trust store cryptographically
  authorizes — it cannot accept expiry/hostname/self-signed/untrusted failures.
- No vendored cert, no network probe, no `ppp setup`.

### Negative Consequences

- On an inspected network the proxy, like every other client there, cannot
  distinguish the real server from the interceptor — inherent to TLS inspection,
  not introduced by ppp (SI-4 caveat).
- The hook reconstructs mitmproxy's upstream-TLS setup using internal APIs
  (`net.tls.create_proxy_server_context`, `TlsConfig` SNI/hostname logic), which
  are version-coupled. Mitigated by pinning mitmproxy (12.2.3) and a **golden
  source-hash drift test** (`tests/addon/test_mitmproxy_internals_drift.py`) that
  fails loudly if that upstream code changes.
- Exporting the OS trust store (macOS `security`, Linux bundle) is
  platform-specific glue; failure to read it makes authorization fail closed
  (interception chains rejected), which is the safe direction.

### Compliance Consequences

- Preserves upstream transport authenticity/verification (SC-8, SC-23, SC-13):
  ppp never sets `ssl_insecure`; only the OS-store-authorized interception case
  is tolerated.
- Records a conscious acceptance (`risk_treatment: accept`) that on a
  TLS-inspecting network the proxy trusts the same interception the host already
  trusts (SI-4). Not a FISMA system (`ato_relevance: no`).

## Links

- Live validation (issue #26): Zscaler network — legit hosts 200, all bad-cert
  `badssl.com` cases 502.
- ADR-0004 (non-FIPS crypto acceptance).
- `internal/catrust` (OS-store bundle, drop non-critical-BC CAs);
  `assets/addon.py` `tls_start_server` (verify callback + authorization);
  `tests/addon/test_upstream_authz.py` (authorization logic);
  `tests/addon/test_mitmproxy_internals_drift.py` (B5 drift guard).
- `docs/explorations/ppp-spec.md` §5.3 (ProxySup).

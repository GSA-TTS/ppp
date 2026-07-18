# Feature request (draft): first-class upstream partial-chain / interception-trust option

> **Status:** draft for human review before filing upstream at
> <https://github.com/mitmproxy/mitmproxy/issues>. Edit freely; nothing here is
> submitted yet. Tone target: a concise, concrete FR that shows the workaround
> and asks for a small, well-scoped option.

---

## Title

Option to verify upstream TLS against the OS trust store / allow a trusted
intermediate as an anchor (partial-chain) for TLS-inspecting networks

## Summary

On a corporate TLS-inspecting network (e.g. Zscaler), mitmproxy's **upstream**
verification (`ssl_verify_upstream_trusted_ca` / default) fails even though the
host OS and every normal client on that network succeed. Two related causes:

1. mitmproxy (Python/OpenSSL 3) verifies against a PEM bundle and does **not**
   consult the macOS Keychain (or the Windows cert store), so roots the host
   trusts are invisible to it.
2. Some interception **roots are non-conformant** — their `BasicConstraints`
   extension is not marked *critical* — and OpenSSL 3 strict-rejects them as
   trust anchors (`X509_V_ERR_...`), so the chain cannot be built even if the
   root is added to the bundle. The conformant interception **intermediate**
   could anchor the chain, but only with `X509_V_FLAG_PARTIAL_CHAIN`, which
   mitmproxy does not set and exposes no option for.

The result: with `ssl_insecure=false` (the correct, secure setting), all
upstream HTTPS through mitmproxy fails on these networks; the only escape today
is `ssl_insecure=true`, which disables verification entirely — a large,
undesirable hammer.

## What I'd like

A **small, opt-in** mechanism to make upstream verification succeed on an
inspected network **without disabling it**. Any one of these would suffice
(roughly in order of preference):

1. **`ssl_verify_upstream_use_os_trust_store` (bool)** — verify the upstream
   leg against the OS trust store (macOS Keychain / Windows store / Linux
   bundle) in addition to / instead of the PEM bundle. This is what `curl` and
   browsers do on the same host, so "works in the browser, fails in mitmproxy"
   goes away.
2. **`ssl_verify_upstream_partial_chain` (bool)** — set
   `X509_V_FLAG_PARTIAL_CHAIN` on the upstream verify store, so a trusted
   non-self-signed cert (a conformant interception intermediate present in the
   trust store) may serve as the anchor.
3. **A documented `tls_start_server` recipe** — if a built-in option is out of
   scope, a supported way to customize the upstream `SSL.Context`/verify params
   from an addon without reimplementing `create_proxy_server_context`.

## Why not `ssl_insecure`

`ssl_insecure` disables verification for the whole upstream leg — it would let a
genuinely-bad server (expired, wrong host, self-signed, MITM beyond the
corporate box) through. On an inspected network the goal is the opposite: keep
verification on, and trust exactly what the host already trusts (the corporate
interception CA), nothing more.

## Current workaround (and why a first-class option would be better)

We ship a downstream tool (`ppp`) that runs an addon implementing a
`tls_start_server` hook. It rebuilds the upstream connection with
`create_proxy_server_context(...)` and installs an OpenSSL **verify callback**
that:

- defers entirely to OpenSSL's verdict, **except**
- it tolerates only the narrow "cannot find a usable issuer" errnos
  (`UNABLE_TO_GET_ISSUER_CERT`, `UNABLE_TO_GET_ISSUER_CERT_LOCALLY`), and
- only when the presented chain is **cryptographically authorized** by the host
  OS trust store (some presented CA is signature-verified as issued by a cert
  the host already trusts).

Everything else (expiry, hostname mismatch, self-signed, untrusted root,
signature failure) stays fatal. This works and is safe, but:

- It **reaches into mitmproxy internals** (`mitmproxy.net.tls.
  create_proxy_server_context`, `TlsConfig` SNI/hostname setup,
  `OpenSSL.SSL._lib.X509_VERIFY_PARAM_*`) that carry no stability guarantee, so
  we pin mitmproxy exactly and guard the replicated code with a golden
  source-hash test that breaks on every upstream change.
- Each downstream tool that hits this has to re-derive the same fragile dance
  (there is at least one other in the wild — see the discussion on the stale
  concurrency FR).

A supported option would let us delete the internals coupling.

## Environment / reproduction

- mitmproxy **12.2.3**, OpenSSL 3.x, macOS (Apple Silicon), host behind Zscaler.
- Presented chain: `leaf` ← `Zscaler Intermediate Root CA (zscalergov.net)`
  (BasicConstraints: **critical**, CA:TRUE) ← `Zscaler Root CA` (2014;
  BasicConstraints **not** critical).
- `ssl_verify_upstream_trusted_ca=<OS store as PEM>` → still fails
  (`certificate verify failed: unable to get local issuer certificate`), because
  the only anchor is the non-conformant root.
- `curl https://example.com` on the same host → succeeds (macOS Security
  tolerates the non-critical-BC root).
- Adding `X509_V_FLAG_PARTIAL_CHAIN` + trusting the conformant intermediate →
  succeeds, and correctly still rejects self-signed / untrusted-root / expired /
  wrong-host (verified against badssl.com through the interceptor).

## Scope / notes

- Purely **upstream** (proxy→server) verification; the client-facing CA is
  unaffected.
- Should be **opt-in** and clearly documented as "trust what the host trusts,"
  not a verification bypass.
- Related: the non-critical-BasicConstraints rejection is an OpenSSL 3
  strictness change many tools have hit; an OS-trust-store option sidesteps it
  entirely for hosts whose OS already trusts the CA.

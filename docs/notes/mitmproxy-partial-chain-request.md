# first-class upstream OS-trust-store / partial-chain verification option

> **Status:** draft for human review before filing upstream at <https://github.com/mitmproxy/mitmproxy/issues>. Edit freely; nothing here is submitted yet.
>
> **Filing notes:** mitmproxy disables blank issues and requires the **"Proposal"** feature-request template (label `kind/feature`). The sections below (`Problem Description` / `Proposal` / `Alternatives` / `Additional context`) match that template verbatim — paste the body into it and use the H1 above as the issue title.

---

#### Problem Description

On a corporate TLS-inspecting network (e.g. Zscaler), mitmproxy's **upstream** (proxy→server) verification fails even though the host OS and every normal client on that network succeed — and there is no safe opt-in short of `ssl_insecure`.

The root cause is that mitmproxy verifies the upstream leg against a **PEM bundle only** — `ssl_verify_upstream_trusted_ca` / `ssl_verify_upstream_trusted_confdir`, defaulting to `certifi` (`mitmproxy/net/tls.py`, `create_proxy_server_context` → `load_verify_locations`). It never consults the **OS trust store** (the macOS Keychain or the Windows certificate store), so the corporate interception CA the host already trusts is invisible to it. `curl` and browsers on the same host succeed because they validate against that OS store (macOS system `curl` uses Secure Transport + the Keychain); mitmproxy, verifying against certifi, has no anchor for the intercepted chain and fails with `X509_V_ERR_UNABLE_TO_GET_ISSUER_CERT_LOCALLY` (errno 20).

Pointing `ssl_verify_upstream_trusted_ca` at an export of the OS store is a workable but awkward manual step, and it has a sharp edge: some interception roots are **non-conformant** (e.g. a `BasicConstraints` extension not marked *critical*, contrary to RFC 5280 §4.2.1.9's "MUST"). Default OpenSSL verification accepts such a root as an anchor, but if a tool sanitizes the exported bundle (e.g. to avoid `X509_V_FLAG_X509_STRICT` rejections elsewhere) and drops that root, the only remaining candidate anchor is the interception **intermediate** — which OpenSSL will only treat as an anchor with `X509_V_FLAG_PARTIAL_CHAIN`, a flag mitmproxy neither sets nor exposes.

Net: with `ssl_insecure=false` (the correct, secure setting), upstream HTTPS through mitmproxy fails on these networks unless the operator manually exports and points at the OS store, and even then can hit the partial-chain wall. The only turnkey escape today is `ssl_insecure=true`, which disables verification entirely — a large, undesirable hammer.

#### Proposal

A **small, opt-in** mechanism to make upstream verification succeed on an inspected network **without disabling it**. Any one of these would suffice (roughly in order of preference):

1. **`ssl_verify_upstream_use_os_trust_store` (bool)** — verify the upstream leg against the OS trust store (macOS Keychain / Windows store / Linux bundle) in addition to / instead of the PEM bundle. This is what `curl` and browsers do on the same host, so "works in the browser, fails in mitmproxy" goes away with no manual bundle export. (OpenSSL ≥3.2 offers a `winstore` store provider; macOS/Linux would need the platform equivalents.)
2. **`ssl_verify_upstream_partial_chain` (bool)** — set `X509_V_FLAG_PARTIAL_CHAIN` (`0x80000`) on the upstream verify store, so a trusted non-self-signed cert (a conformant interception intermediate present in the trust bundle) may serve as the anchor. Useful when the self-signed root is unavailable or is dropped for being non-conformant.
3. **A documented `tls_start_server` recipe** — if a built-in option is out of scope, a supported way to customize the upstream `SSL.Context` / verify params from an addon without reimplementing `create_proxy_server_context`.

Whichever form: it should be **opt-in** and clearly documented as "trust what the host already trusts," not a verification bypass.

#### Alternatives

**`ssl_insecure` (rejected).** It disables verification for the whole upstream leg — it would let a genuinely-bad server (expired, wrong host, self-signed, MITM beyond the corporate box) through. On an inspected network the goal is the opposite: keep verification on, and trust exactly what the host already trusts (the corporate interception CA), nothing more.

**Export the OS store to a PEM and point `ssl_verify_upstream_trusted_ca` at it (what we do today).** This is the closest thing available and it works: a downstream tool (`ppp`) exports the host OS trust store to PEM at startup and passes it as `ssl_verify_upstream_trusted_ca`, so mitmproxy's default verification anchors the intercepted chain at the interception root the host already trusts (and still rejects expired / self-signed / untrusted-root / wrong-host). The gaps a first-class option would close are ergonomic, not correctness:

- Every such tool must re-implement OS-store export per platform (macOS `security`, Windows cert store, Linux bundle) and re-export to catch interception-cert rotation, rather than flipping one option.
- If a tool sanitizes the exported bundle (e.g. drops a non-conformant root to avoid `X509_V_FLAG_X509_STRICT` rejections elsewhere), it then needs `X509_V_FLAG_PARTIAL_CHAIN` to anchor at the conformant intermediate — which no option exposes, forcing a `tls_start_server` recipe that reaches into mitmproxy internals (`create_proxy_server_context`, `OpenSSL.SSL._lib.X509_VERIFY_PARAM_*`). (We hit this, then removed it once we realized exporting the store *unsanitized* is sufficient — but option 2 would make the sanitized case supportable too.)

#### Additional context

Environment / reproduction:

- mitmproxy **12.2.3**, OpenSSL 3.x (pyOpenSSL / `cryptography`), macOS (Apple Silicon), host behind Zscaler.
- Presented chain: `leaf` ← `Zscaler Intermediate Root CA (zscalergov.net)` (BasicConstraints: **critical**, CA:TRUE) ← `Zscaler Root CA` (2014; BasicConstraints **not** critical).
- Default `ssl_verify_upstream_trusted_ca` (certifi) → fails `X509_V_ERR_UNABLE_TO_GET_ISSUER_CERT_LOCALLY` (errno 20): certifi has no anchor for the intercepted chain.
- `curl https://example.com` on the same host → succeeds, because it validates against the macOS Keychain (which trusts the corporate root), not certifi.
- Exporting the OS store to a PEM and using it as `ssl_verify_upstream_trusted_ca` → succeeds (the non-critical-BC root is accepted as an anchor under OpenSSL's default, non-strict verification). If that root is dropped from the bundle, verification then needs `X509_V_FLAG_PARTIAL_CHAIN` to anchor at the conformant intermediate; with the flag set it succeeds and still correctly rejects self-signed / untrusted-root / expired / wrong-host (verified against badssl.com through the interceptor).

Clarifications to avoid confusion:

- This is **not** about OpenSSL 3 rejecting a non-critical-`BasicConstraints` CA by default. Default verification accepts it; only `X509_V_FLAG_X509_STRICT` yields `X509_V_ERR_CA_BCONS_NOT_CRITICAL` (errno 89), and mitmproxy does not enable strict mode. The failure here is simply "no usable anchor in the configured bundle."
- mitmproxy loads only the PEM/dir bundle; it does not call OpenSSL's default-verify-paths or the OpenSSL `winstore` provider, so OS-store roots are invisible on both macOS and Windows regardless of what OpenSSL *could* do.

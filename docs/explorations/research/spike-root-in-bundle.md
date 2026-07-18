# Spike: is the catrust root-drop + `tls_start_server` verify callback necessary?

**Date:** 2026-07-18  **Host:** macOS behind Zscaler  **mitmproxy:** 12.2.3
(bundled OpenSSL 4.0.1, pyOpenSSL 26.2.0, Python 3.14) at
`/var/folders/.../opencode/smoke-venv`. Read-only; no repo changes; no ppp
daemon touched; no podman machines created. Any mitmdump started was killed.

## What was tested

Question: if we KEEP the non-critical-BasicConstraints "Zscaler Root CA" in the
upstream CA bundle and let mitmproxy verify with its DEFAULTS
(`load_verify_locations` on a PEM bundle, `VERIFY_PEER`, **no**
`X509_V_FLAG_PARTIAL_CHAIN`, **no** strict mode, **no** custom callback), does
upstream TLS verification just succeed?

- `/tmp/osfull.pem` — macOS System + SystemRoot keychains, 177 certs, **root KEPT**.
- Confirmed target root: `CN=Zscaler Root CA` — `BasicConstraints CA:TRUE,
  critical:FALSE`, self-signed. This is exactly the cert catrust drops.
- `/tmp/osfiltered.pem` — same bundle with the catrust rule applied (drop CA
  certs whose BasicConstraints is non-critical). Dropped 4 roots incl. Zscaler
  Root CA (also Go Daddy / Starfield / ePKI class-2 legacy roots).
- Verified two ways: (a) real `mitmdump` reverse-proxy, no addon, with
  `ssl_verify_upstream_trusted_ca=<bundle>`; (b) pyOpenSSL calling
  mitmproxy's own `net_tls.create_proxy_server_context(... verify=VERIFY_PEER,
  ca_pemfile=<bundle> ...)` against live hosts. All real chains observed are
  Zscaler-re-signed (leaf → Zscaler Intermediate → Zscaler Intermediate Root →
  **Zscaler Root CA**).

## Results

- **mitmdump, full bundle, no addon:** example.com → HTTP 200; www.iana.org →
  HTTP 200; logs show `server connect ... 200 OK`, **no** "certificate verify
  failed" / "Cannot establish TLS".
- **pyOpenSSL, full bundle (root KEPT), mitmproxy default context:**
  example.com & www.iana.org → **HANDSHAKE OK**, verified chain len 4, zero
  verify errors. (verdict = trusted purely on the plain bundle.)
- **Negative control, filtered bundle (root DROPPED):** both hosts →
  **HANDSHAKE FAILED**, `certificate verify failed`, captured verify error
  **errno 20 at depth 2 (UNABLE_TO_GET_ISSUER_CERT_LOCALLY)**. This is exactly
  the failure the addon callback compensates for.
- **Security floor, full bundle (root KEPT), no override:** self-signed.badssl →
  FAIL errno 18; expired.badssl → FAIL errno 10; untrusted-root.badssl → FAIL
  errno 19. All correctly rejected. (`wrong.host.badssl` handshake "OK" only
  because chain trust is genuinely valid — hostname matching is a *separate*
  mitmproxy layer, not part of this chain-trust context; not a regression.)

## Verdict

- **(A) Root KEPT + mitmproxy DEFAULT verify (no callback, no partial-chain):
  upstream verify SUCCEEDS.** OpenSSL's default (non-strict) path accepts the
  non-critical-BC Zscaler root as a trust anchor. Confirmed both via real
  mitmdump and via mitmproxy's own context builder.
- **(B) YES — dropping the root is what breaks it.** The filtered bundle fails
  with **errno 20 (unable to get local issuer)** at the root's depth; that is
  precisely the gap the `tls_start_server` partial-chain + OS-store
  re-authorization callback exists to paper over. The failure is self-inflicted
  by catrust's own filter.
- **(C) YES — bad certs still rejected with root kept + no callback:**
  self-signed (18), expired (10), and untrusted-root (19) all fail closed. The
  security floor holds without any custom verdict logic.

## Recommendation

**ppp can delete the catrust root-drop AND the entire `tls_start_server`
verify callback / partial-chain / OS-store re-authorization machinery, and just
compose the OS trust store as-is.** The drop of non-critical-BC CA certs is the
sole cause of the anchor-missing (errno 20) failure that the callback was
written to re-admit; remove the drop and mitmproxy's stock verification both
(A) trusts the Zscaler MITM chain and (C) still rejects genuinely bad certs. The
callback adds no security value it doesn't already get from default verify — it
only restores what catrust removed.

Caveats: (1) tested against OpenSSL 4.0.1 as shipped in this mitmproxy build;
re-confirm against the exact OpenSSL that ships with the pinned production
mitmproxy if different, though default (non-strict) anchor acceptance of
non-critical BC is long-standing OpenSSL behavior. (2) Dropping the filter also
re-admits Go Daddy / Starfield / ePKI legacy roots — that is correct (they are
OS-trusted) but note the behavior change. (3) Hostname verification is a
distinct mitmproxy layer and is unaffected by this change.

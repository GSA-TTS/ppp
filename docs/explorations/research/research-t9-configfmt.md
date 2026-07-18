# Research T9 — Config-format coupling: pin mitmproxy + robust client-config parser

**Ticket:** GSA-TTS/ppp #12 · **Type:** RESEARCH (decision, not build) · **Date:** 2026-07-16
**Scope:** How does `ppp` stay robust to mitmproxy's generated WireGuard client-config format?
**Spec refs:** `docs/explorations/ppp-spec.md` §3.1, §5.3, Risk #8, Risk #10; ADR-0003.

---

## TL;DR decision

1. **Pin `mitmproxy == 12.2.3` exactly** for v1 (the verified version), with a **manual-bump support policy** (single supported version, upgrade only via a deliberate PR that re-captures the golden fixture). Do **not** float a range.
2. **Parse the block with a purpose-built line scanner + strict key/value validation, then rewrite only the `Address` and `Endpoint` values** — not a generic INI round-trip, and not blind string replacement. Fail **closed and loud** on any deviation.
3. **Guard drift three ways:** a committed **golden fixture** captured from the pinned version, a **startup format assertion** on the live block, and a **version assertion** (`mitmdump --version` must equal the pin).

Because ADR-0003 moved sandbox *identity* onto the WireGuard listen **port** (unspoofable, crypto-bound), a parser failure is now a **routing/clarity** failure, not a security-boundary failure. That lowers the blast radius but does **not** license silent best-effort parsing — a mis-rewritten `Address`/`Endpoint` produces a tunnel that won't come up, so we still fail closed.

---

## 1. Version pin + support policy

### Format-stability evidence (primary source: mitmproxy git history)

I diffed `WireGuardServerInstance.client_conf()` across tagged releases via
`raw.githubusercontent.com/mitmproxy/mitmproxy/<tag>/mitmproxy/proxy/mode_servers.py`:

| Version | `client_conf()` body | Fence emission |
|---|---|---|
| v9.0.0 | `[Interface]/PrivateKey/Address = 10.0.0.1/32/DNS = 10.0.0.53` + `[Peer]/PublicKey/AllowedIPs = 0.0.0.0/0/Endpoint` (`mode_servers.py:352-366`) | fence was a literal **60-char `-` string** embedded in the same `logger.info(...)`, alongside the "listening at" text (`mode_servers.py:343-349`) |
| v10.0.0 | identical body (`mode_servers.py:414-429`) | switched to `logger.info("-" * 60 + "\n" + conf + "\n" + "-" * 60)` (`mode_servers.py:412`) — fence now a **separate** log record |
| v11.0.0 | identical body; `host` gained `listen_host()` fallback (`mode_servers.py:403-415`) | same `"-"*60` fence |
| v12.0.0 | identical body; `_server`→`_servers`, `wg.pubkey`→`mitmproxy_rs.wireguard.pubkey` (`mode_servers.py:393-412`) | same |
| v12.2.0 | byte-identical to 12.0.0 | same |
| **v12.2.3 (pinned)** | byte-identical (`mode_servers.py:404-412`) | `logger.info("-" * 60 + "\n" + conf + "\n" + "-" * 60)` (`mode_servers.py:379`) |

**The five config lines we care about (`Address = 10.0.0.1/32`, `DNS = 10.0.0.53`,
`AllowedIPs = 0.0.0.0/0`, `Endpoint = {host}:{port}`, `PrivateKey`/`PublicKey`) have
been byte-stable across every release since WireGuard mode shipped (v9.0.0, Oct 2022).**
The only observed changes are (a) the fence went from inline to a standalone 60-hyphen
record between v9 and v10, and (b) internal symbol renames that don't affect output.

`mode_specs.py` `WireGuardMode.default_port = 51820` is likewise stable v9→v12.2.3
(v9: `mode_specs.py:253`; v12.2.3: `mode_specs.py:288`). No CHANGELOG entry between v9.0.0
and the current `Unreleased` section touches the client-config **text**; WireGuard-tagged
entries are all bind-order / IPv4-v6 / endpoint-NIC / allow_hosts fixes
(`CHANGELOG.md`: 10.2.0, 10.2.1, 12.0.0 #7589, 10.2.3 #6659) — none change the emitted keys.

### Recommendation

- **Pin `mitmproxy==12.2.3`** (exact `==`, with a hash/lock where the install path allows).
  This is the version the whole spec was verified against and the one I re-verified live below.
- **Support policy: single supported version.** `ppp setup` installs exactly the pinned
  version; `ppp` refuses to run against any other (see §3 version assertion). Upgrades are a
  deliberate maintenance action: bump the pin → re-run the capture script → regenerate the
  golden fixture → run the parser conformance test → land as one PR. This matches AGENTS.md
  ("Pin exact versions … the WireGuard client-config parser depends on its format").
- **Minimum-compatible floor is v10.0.0** (first version with the standalone `"-"*60` fence
  the capture parser expects). Do not silently accept < v10; the v9 inline-fence + combined
  log line would defeat the `^-{60}$` block matcher. But v1 should still hard-pin to 12.2.3
  rather than accept a `>=10` range, because a range re-introduces exactly the drift risk this
  ticket exists to close.

---

## 2. Parser design

### Ground truth (verified live, this session)

I ran a throwaway `mitmdump 12.2.3` (via `uvx --from 'mitmproxy==12.2.3'`) in the
pre-approved temp dir, single- and multi-instance, and captured a real block:

```
[04:59:23.133] ------------------------------------------------------------
[Interface]
PrivateKey = <REDACTED-ephemeral-spike-key>
Address = 10.0.0.1/32
DNS = 10.0.0.53

[Peer]
PublicKey = 1B+kI9c8wogO1mmurP6HoXuKhdNtS+5nxMyDg97HVWs=
AllowedIPs = 0.0.0.0/0
Endpoint = 100.64.0.1:56020
------------------------------------------------------------
[04:59:23.133] WireGuard server listening at *:56020.
```

Verified by `od -c`:
- The **opening fence** and the **`WireGuard server listening at …`** line each carry the
  mitmdump log-formatter timestamp prefix `[HH:MM:SS.mmm] `. The **inner config lines and the
  closing fence do NOT** — the formatter only prefixes the first physical line of a multi-line
  log record, so the closing fence is a **bare** line of exactly 60 hyphens (`od -c` showed
  60 `-` then `\n`, no prefix). The opening fence is `[ts] ` + 60 hyphens.
- **Stream:** empirically the whole thing (block + "listening") went to **stdout**; stderr was
  0 bytes. This contradicts the spec's "stderr/INFO" note (§3.1, §5.3). **Actionable:** the
  supervisor must capture **both** streams merged (`2>&1`) into `proxy.log` rather than assume
  stderr. (Python's default `logging` StreamHandler is stderr, but `mitmdump` installs its own
  `TermLog`/console addon that writes to stdout; do not hard-depend on either — merge them.)
- **Multi-instance ordering is NOT deterministic.** Across repeated 2-instance runs
  (`@53020` + `@53021`) the blocks were emitted **`53021` then `53020`** twice and **`53020`
  then `53021`** once. **So we must NOT correlate a block to a port by flag order.** The
  `Endpoint = host:<port>` line is the only reliable in-band port tag — key each parsed block
  by the port in its own `Endpoint` line and cross-check against the set of ports we launched.
  (Spec §3.1/§5.3 mention flag-order correlation "and can cross-check Endpoint" — invert that:
  **Endpoint is authoritative, flag order is not.**)

### Recommended approach: **targeted rewrite over a validated model — reject generic INI round-trip AND blind string replace**

**Rejected — generic INI parse-and-serialize:** Go's common INI libs will happily normalise
whitespace, reorder/emit keys, drop blank lines, or lowercase section names on write. The
generated block is not really INI for our purposes (it's an opaque WG config we hand to
`wg-quick`), and a round-trip risks reordering `[Interface]`/`[Peer]` or dropping the
`AllowedIPs`/`DNS` lines the guest needs. We must preserve the block **verbatim except two
values**.

**Rejected — blind `strings.Replace("10.0.0.1/32", …)`:** brittle to the theoretical case
where `10.0.0.1` appears elsewhere, and it silently succeeds even if the surrounding format
drifted (defeats fail-closed).

**Recommended — parse into a strict model, validate, rewrite by field, re-emit line-preserving:**

1. **Block extraction (capture layer, `internal/proxy/capture.go`):**
   - Read the merged log stream. Strip any leading `[HH:MM:SS.mmm] ` log prefix per line
     before matching (regex `^\[\d\d:\d\d:\d\d\.\d+\]\s`), because the opening fence is
     prefixed but the closing one is not.
   - A block is the content between two lines that, **after prefix-stripping**, match
     **exactly** `^-{60}$` (60 hyphens, not `---`, not 59/61). Use anchored `regexp.MustCompile(\`^-{60}$\`)`.
   - Ignore any interleaved non-block log lines (e.g. the "listening at" line, later request
     logs). Only text strictly between a matched open/close fence pair is a candidate block.

2. **Structural validation (fail closed):** a valid block MUST contain, in order,
   `[Interface]` then `[Peer]`, and MUST have exactly these keys with values matching:
   - `PrivateKey = <base64/44-char WG key>`
   - `Address = 10.0.0.1/32`  ← assert the literal we expect (drift canary; see §3)
   - `DNS = 10.0.0.53`         ← assert literal (drift canary)
   - `PublicKey = <WG key>`
   - `AllowedIPs = 0.0.0.0/0`  ← assert literal
   - `Endpoint = <host>:<port>` where `port` ∈ the set we launched.
   Any missing/extra/unmatched key, or `Address`/`DNS`/`AllowedIPs` not matching the pinned
   literals, ⇒ **hard error, refuse to start the sandbox** (do not guess).

3. **Rewrite exactly two fields, preserving everything else:** operate on the captured lines,
   replacing only the value on the `Address` line
   (`10.0.0.1/32` → `10.0.0.<octet>/32`, spec §5.3) and the `Endpoint` line
   (`<autodetected>:<port>` → `<host-reachable-ip>:<port>`, spec §5.3), leaving byte-for-byte
   everything else (section headers, blank line, key order, `AllowedIPs`, `DNS`). Emit as
   `wg0.conf`.

4. **Index by port, not order:** store `{port → parsed+rewritten conf}` in
   `$PPP_DATA/wg/client-confs.json` (spec §5.3), keyed off the `Endpoint` port.

### Failure-mode table

| Failure mode | Detection | Behaviour |
|---|---|---|
| **Format drift** (mitmproxy upgrade changes a key/label/order) | structural validation + literal assertions on `Address`/`DNS`/`AllowedIPs` + version assertion (§3) | **Fail closed, loud.** Refuse to provision; error: "unrecognised mitmproxy WG config format; expected pinned 12.2.3 — re-run capture + regenerate golden fixture." |
| **Multiple blocks** (N instances → N blocks, order non-deterministic — *confirmed live*) | each block keyed by its own `Endpoint` port; require the parsed port-set == launched port-set | if a launched port has no matching block, or a block has an unexpected port ⇒ hard error. Never rely on emission order. |
| **Interleaved / partial log lines** (request logs, "listening at", or a block split by a crash mid-write) | only text strictly between a matched `^-{60}$` open/close pair counts; an unterminated open fence (EOF/next open before a close) ⇒ incomplete | discard incomplete blocks; if an expected port never yields a complete block within the startup wait window ⇒ hard error (don't provision a half-parsed conf). |
| **Log prefix on fence** (opening fence prefixed, closing bare — *confirmed live*) | prefix-strip regex before fence match | handled; documented so a future refactor doesn't "simplify" it away. |
| **Stream ambiguity** (stdout vs stderr — spec said stderr, live showed stdout) | capture merged `2>&1` | robust to either; don't hard-code one stream. |
| **Duplicate `Address`/`Endpoint` tokens elsewhere** | field-scoped line match (`^Address = `, `^Endpoint = `), not substring replace | only the real config lines are touched. |
| **Empty/None conf** (`client_conf()` returns None if `_servers` empty) | block simply absent | treated as "port not ready yet" → same incomplete-block path. |

**Fail mode = closed + loud, always.** Even though ADR-0003 makes identity port-based (so a bad
`Address` no longer leaks a secret), a wrong rewrite yields a tunnel that won't establish, and a
silent "best effort" would surface as an opaque connectivity failure later. Erroring at parse
time with a precise message is strictly better.

---

## 3. Drift-guard approach

Three complementary, cheap guards:

1. **Golden fixture (committed):** capture one real block from the pinned 12.2.3 and commit it
   (e.g. `internal/proxy/testdata/wg-client-conf-12.2.3.golden`), sanitised to placeholder keys
   but preserving structure/whitespace/labels. A unit test runs the real parser over the golden
   fixture and asserts the extracted `Address`/`DNS`/`AllowedIPs` literals and the field-scoped
   rewrite output. When someone bumps the pin, this test forces a deliberate re-capture. A
   captured real example is in `/var/folders/35/t5_qtyd91jl34_99mkmhbxqh0000gn/T/opencode/t9-spike/`
   (this session) — use it as the seed.
2. **Startup format assertion (runtime):** at daemon start, after capturing each live block,
   assert the three invariant literals (`Address = 10.0.0.1/32`, `DNS = 10.0.0.53`,
   `AllowedIPs = 0.0.0.0/0`) and the `[Interface]`/`[Peer]` structure. Mismatch ⇒ refuse to
   start with the drift error above. This catches drift even if someone bypassed `ppp setup`.
3. **Version assertion (runtime + setup):** `ppp setup`/`ppp daemon start` runs
   `mitmdump --version`, parses the `Mitmproxy: 12.2.3` line (confirmed format this session),
   and requires an **exact** match to the pin. A non-matching version is itself the loudest,
   earliest drift signal — fail before ever launching WG instances.

Together: version assertion is the first gate, format assertion is defence-in-depth for the
running block, and the golden test is the CI gate that makes an intentional upgrade impossible
to land without re-verifying the format.

---

## Cited sources

- `mitmproxy/proxy/mode_servers.py` `WireGuardServerInstance.client_conf()`:
  v12.2.3 `mode_servers.py:393-414` (body at `:404-412`); fence log at `:377-379`
  (`logger.info("-" * 60 + "\n" + conf + "\n" + "-" * 60)`).
  History: v9.0.0 `:352-366` (inline fence `:343-349`); v10.0.0 `:414-429` (fence moved to `:412`);
  v11.0.0 `:403-415`; v12.0.0 `:393-412`.
  URL: `https://raw.githubusercontent.com/mitmproxy/mitmproxy/v12.2.3/mitmproxy/proxy/mode_servers.py`
- `mitmproxy/proxy/mode_specs.py` `WireGuardMode.default_port = 51820`:
  v12.2.3 `:288` (v9.0.0 `:253`). Stable across versions.
- `CHANGELOG.md` (mitmproxy repo): WireGuard entries 9.0.0 (mode intro), 9.0.1 (perf/stability),
  10.2.0/10.2.1 (bind behaviour), 12.0.0 #7589 (IPv4/IPv6 default bind), 10.2.3 #6659
  (endpoint w/ multiple NICs) — none alter the emitted config **text**.
  URL: `https://raw.githubusercontent.com/mitmproxy/mitmproxy/main/CHANGELOG.md`
- **Live spike (this session), `mitmdump 12.2.3` via `uvx`,**
  `/var/folders/35/t5_qtyd91jl34_99mkmhbxqh0000gn/T/opencode/t9-spike/`:
  captured single- and two-instance blocks; `od -c` confirmed opening fence carries `[ts]`
  prefix + 60 hyphens, closing fence is a bare 60-hyphen line; block emitted on **stdout**
  (stderr empty); **two-instance emission order non-deterministic** across 3 runs;
  `Endpoint = host:<port>` is the only in-band port discriminator.
- `docs/explorations/ppp-spec.md` §3.1 (client-config, `mode_servers.py:406`, `:377-379`,
  fence "exactly 60 hyphens"), §5.3 (capture/rewrite), Risk #8 (this ticket), Risk #10
  (return-path fallback). `docs/decisions/0003-sandbox-identity-by-wireguard-port.md`
  (identity = port, so parser failure ≠ security failure).

---

## Residual uncertainty

- **stdout vs stderr:** spec asserts stderr/INFO (`mode_servers.py:377-379`); my live run put the
  block on **stdout**. Root cause is mitmdump's console log addon, which may differ by
  TTY/`--set` flags/OS. **Mitigation already recommended: capture merged `2>&1`.** Worth a
  30-second confirm on Linux/Windows Podman-Machine hosts before v1 ship.
- **`Endpoint` host value:** locally it auto-detected `100.64.0.1` (a CGNAT/utun address). On a
  real host it depends on `local_ip.get_local_ip()` and active NICs (CHANGELOG 10.2.3 #6659 was
  a bug here). We rewrite `Endpoint` anyway (§5.3), so this only matters if the *format* of the
  line changes — it hasn't — but confirm the rewrite runs before `wg0.conf` is handed to the
  guest.
- **Multi-instance under load:** only tested 1–2 instances. Order non-determinism is already
  handled by keying on `Endpoint` port; the spec's 80-instance target (Risk #10) should be
  exercised for the return-path fallback separately (out of scope for the *format* question).
- **Future mitmproxy majors:** v13+ could theoretically make `Address`/`DNS` configurable
  (long-requested). The version assertion + golden test will catch it; treat as a deliberate
  upgrade, not auto-adopt.

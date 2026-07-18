# ppp LIVE E2E validation spike — results

- **Date:** 2026-07-16 (macOS 26.5.2, arm64)
- **Stack:** podman 6.0.1 / machine-os 6.0 (FCOS 44) / libkrun (krunkit 1.3.2) / mitmproxy 12.2.3
- **Scope:** wayfinder #4 (routing), #5 (guest WG install), #7 (CA bootstrap), ADR-0005 (guest DNS off-tunnel)
- **Machine:** throwaway `ppp-spike-e2e` — created, used, and **removed** (confirmed gone).

---

## Verdict per check

| # | Check | Result |
|---|---|---|
| 4a | Tunnel handshake succeeds (`wg show` recent handshake + transfer) | **PASS** |
| 4b | `curl https://<host>` traverses tunnel, is intercepted, correct per-instance identity | **PASS** (with one important correction — see below) |
| 4c | `curl http://mitm.it/cert/pem` returns a PEM once tunnel is up (#7) | **PASS** |
| 4d | DNS resolves with the `DNS=` line removed (ADR-0005) | **PASS** |
| 4e | Second instance on `:51821` from the guest, distinct identity | **PASS** (as a second WG iface `wg1`; one guest CAN hold multiple) |

Overall: **the T1/T5/T7/ADR-0005 decisions hold up on real hardware** — but with **two material corrections** (Endpoint address, and the identity attribute name) that MUST be folded into the spec.

---

## CORRECTION 1 (blocking) — Endpoint must be the gvproxy **host alias `192.168.127.254`**, NOT the host LAN IP

The spec/ticket assumed the guest reaches the host at the host LAN IP (`192.168.5.212`) via the gvproxy gateway `192.168.127.1`. **This does not work.**

- With `Endpoint = 192.168.5.212:51820`, the guest sent handshake initiations (`296–592 B sent, 0 B received`) but mitmdump **never received them**. gvproxy on macOS NATs the guest packet to a host-side socket whose source==dest==`192.168.5.212` (the host's own LAN IP); those packets never reached mitmdump's `*:51820` socket. tcpdump on `lo0`/`en0`/`any` for `udp port 51820` captured **nothing**. Result: connection timeouts, no handshake.
- The gvproxy stack exposes the **host** to the guest at `192.168.127.254` (`host.containers.internal` / `host.docker.internal` in `/etc/hosts`), while `.1` is the gateway. Switching to `Endpoint = 192.168.127.254:51820` gave an **immediate handshake** (`latest handshake: Now`, `3.49 KiB received, 2.62 KiB sent`) and full traffic flow.

**Spec fix:** the per-sandbox `Endpoint` rewrite target on macOS/libkrun is **`192.168.127.254`** (gvproxy host alias), not a detected host LAN IP. Recommend deriving it from the guest's `host.containers.internal` entry (fall back to the `192.168.127.254` literal). This also means the `ip route replace <endpoint-ip>/32 via <gw>` off-tunnel exception targets `192.168.127.254/32`.

## CORRECTION 2 (blocking) — sandbox identity is **`flow.client_conn.proxy_mode`**, NOT `flow.client.sockname`

Spec §3.1/§5.4 say to identify the sandbox by `flow.client.sockname[1]` = the WG listen port. **On mitmproxy 12.2.3 that is wrong.** Verified by dumping the connection object on a real tunnel:

- `flow.client_conn.sockname` = **the inner TCP destination** (e.g. `142.251.157.119:443`), i.e. the *server the guest is talking to* — useless for sandbox identity.
- `flow.client_conn.peername` / `.address` = the inner tunnel source (`10.0.0.1:...`) — the spoofable one (as spec already notes).
- The listen port **is** available, but on `flow.client_conn.proxy_mode`: a `ProxyMode` whose `.full_spec` is e.g. `'wireguard:wg-keys-1.conf@51820'` and `.listen_port()` returns the port. This is per-instance and set by mitmproxy, not by the guest.

**Spike attribution proof (two live instances in one process):**
```
host=142.251.157.119  full_spec='wireguard:wg-keys-1.conf@51820'  peername=('10.0.0.1', ...)
host=8.8.4.4          full_spec='wireguard:wg-keys-2.conf@51821'  peername=('10.0.0.2', ...)
host=172.66.147.243   full_spec='wireguard:wg-keys-2.conf@51821'  peername=('10.0.0.2', ...)
```
Distinct instances are cleanly separated by `proxy_mode.full_spec` / `listen_port()`. **Port-based, cryptographically-bound identity is confirmed on a real tunnel — but the addon must read `client_conn.proxy_mode.listen_port()` (or parse `full_spec`), not `client_conn.sockname`.** The security argument in §3.1 is unchanged; only the attribute name is wrong. NOTE: verify the exact `listen_port` accessor (it is a bound method — call it, or parse the `@<port>` from `full_spec`).

---

## Confirmations of existing decisions (held up)

- **#5 (guest WG install):** `wireguard-tools-1.0.20260223-1.fc44` is **already present** in machine-os 6.0; `sudo modprobe wireguard` loads the module (`wireguard 102400 0` in `lsmod`). No `rpm-ostree install`, no reboot needed. **Confirmed.**
- **#4 (routing recipe):** the `Table = off` + manual-route recipe works verbatim (with the Endpoint correction):
  1. `sudo install -m600 wg0.conf /etc/wireguard/wg0.conf`
  2. `GW=$(ip route show default | awk '/default/{print $3}')`  → **`192.168.127.1` confirmed** (matches spec).
  3. `sudo ip route replace 192.168.127.254/32 via 192.168.127.1`  (off-tunnel exception to the Endpoint)
  4. `sudo wg-quick up wg0`
  5. `sudo ip route replace default dev wg0`

  No routing loop; handshake + full egress worked. **Caveat:** `wg-quick down/up` recreates the interface and **drops the `default dev wg0` route**, so step 5 must be re-run after any bounce (provision script already re-runs every boot, so fine — but worth noting the ordering is load-bearing).
- **#7 (CA bootstrap):** once the tunnel is up, `curl -s http://mitm.it/cert/pem` returned a valid PEM (`-----BEGIN CERTIFICATE-----`, 1 cert). After `cp` to `/etc/pki/ca-trust/source/anchors/` + `update-ca-trust`, `curl https://www.google.com` returned **200 with full TLS verification** (no `-k`). **Confirmed** the in-guest fetch path works and depends on the tunnel + off-tunnel Endpoint route being in place first.
- **ADR-0005 (drop `DNS=`):** with the `DNS =` line removed, `wg-quick up` did **not** invoke resolvconf/resolvectl (grep for `resolv` in its output = none), the guest kept `nameserver 192.168.127.1` (gvproxy) in `/etc/resolv.conf`, and name resolution worked through the tunnel default route (`getent hosts github.com` → `140.82.116.3`, `curl` to resolved hosts succeeded). **Confirmed** — reliable bring-up, no resolvconf abort.

## mitmdump client-config emission (step 1 findings)

- **Stream:** client configs are emitted on **stdout** (captured cleanly with `>file 2>/dev/null`; stderr file was empty). Matches the corrected spec note (not stderr).
- **Buffering gotcha (new):** when stdout is redirected to a **file/pipe** (no TTY), Python fully block-buffers and the file stays **0 bytes** while the process runs — the configs only flush on exit/`SIGTERM`. Real `ppp` must read the child via a **PTY** (I used `script -q /dev/null …`) or set `PYTHONUNBUFFERED=1` / poll after flush; otherwise the capture parser sees an empty `proxy.log`. This is an implementation risk for the supervisor's "read proxy.log at startup" step.
- **Fence:** opening fence carries a `[HH:MM:SS.mmm]` timestamp prefix; closing fence is a **bare line of exactly 60 hyphens** (`awk length` = 60). Matches spec.
- **Order:** emission order is **non-deterministic** (one run emitted `:51821` before `:51820`). Correlate by the `Endpoint = host:<port>` line inside each block — **confirmed** (spec already says this).
- **Keys files:** auto-generated as JSON `{server_key, client_key}` when the path did not pre-exist. **Both are PRIVATE keys.** The client's `Peer.PublicKey` must be `wg pubkey(server_key)` — NOT the raw `server_key` from the JSON. (I hit `InvalidAeadTag` when I hand-built the config from the JSON `server_key`; using the emitted client-config block, which already contains the correct server *public* key, fixed it.) **Implication:** `ppp` should parse the emitted client-config block for the peer public key rather than deriving from the keys JSON, or explicitly run the pubkey transform.

## Working wg0.conf shape (parsed from the emitted block, then rewritten)

```ini
[Interface]
PrivateKey = <client_key from block>
Address = 10.0.0.1/32
Table = off

[Peer]
PublicKey = <server PUBLIC key from block>
AllowedIPs = 0.0.0.0/0
Endpoint = 192.168.127.254:51820
```
(DNS line removed; Table=off added; Endpoint rewritten to the gvproxy host alias.)

## Residual gaps / notes

- **`example.com` returned 502** from mitmproxy while `www.google.com`/`cloudflare.com` gave 200/301 and host-direct `example.com` gave 200. Appears to be a per-host upstream/altered-connection quirk in mitmproxy 12.2.3, **not** a ppp routing/interception failure (interception itself is proven by the addon logs + successful TLS to other hosts). Worth a follow-up but non-blocking.
- **Multiple WG ifaces in one guest (4e):** a single guest happily ran `wg0` (51820) **and** `wg1` (51821) simultaneously with distinct handshakes and distinct proxy-mode attribution. So the "one guest = one tunnel" assumption is not a hard limit — but in the real ppp model each *sandbox* is a separate VM, so this only mattered for exercising two instances; port identity on a real tunnel is confirmed either way.
- **IPv6:** not exercised end-to-end; spec's IPv4-only `AllowedIPs` + guest IPv6 disable step should still be validated.
- **Buffering/PTY capture** (above) is the biggest implementation risk surfaced.

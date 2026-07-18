# Diagnosis: is `ppp-e2e-manual` HTTPS intercepted by our mitmproxy, or bypassing to Zscaler?

**Date:** 2026-07-18
**Sandbox:** `ppp-e2e-manual` (libkrun VM, running since 01:08)
**Verdict:** **BYPASS.** The guest's HTTPS traffic is NOT going through our tunnel or our
mitmproxy. It egresses straight out `eth0` (gvproxy → host → Zscaler). The core isolation
guarantee is currently NOT in effect for this sandbox. It is **not** a stderr-capture test
artifact — the product is genuinely not intercepting.

Crucially, the two "contradictory" observations from the prompt are **not both currently true**.
Observation #1 (a `flows.jsonl` line for example.com) could NOT be reproduced — see below.

---

## Decisive correlated experiment (unique host `www.iana.org`, then re-confirmed on example.com)

Ran inside the guest via `ppp exec ppp-e2e-manual -- sh -c '...'`:

| Check | Result |
|---|---|
| `ip link show wg0` | **Device "wg0" does not exist** (WG0 ABSENT) |
| `ip route show default` | `default via 192.168.127.1 dev eth0` — **default is eth0, NOT wg0** |
| `ip route get 192.0.43.8` (iana) | `via 192.168.127.1 dev eth0` — off-tunnel |
| `ip route get 172.66.147.243` (example.com) | `via 192.168.127.1 dev eth0` — off-tunnel |
| `sudo wg show` (before & after curl) | **empty** — no WireGuard interface, no counters to rise |
| guest `curl -sv https://www.iana.org` issuer | `CN="Zscaler Intermediate Root CA (zscalergov.net)"` |
| guest `curl -sv https://example.com` issuer | `CN="Zscaler Intermediate Root CA (zscalergov.net)"` |
| `curl` result | `http_code=200 remote_ip=172.66.147.243 remote_port=443` — connects direct to origin IP:443 |
| New `flows.jsonl` line after curl | **NONE** |

There are no wg0 transfer counters to compare because there is no wg0 at all. The guest reaches
the real origin IP on :443 directly and sees a **Zscaler**-signed leaf, exactly what a host
NOT behind our proxy sees behind a corporate TLS-inspecting proxy.

---

## Why it is bypassing — three independent, mutually-confirming facts

### 1. The mitmdump proxy is not running and never successfully started this boot
- `ppp daemon status` → **"proxy not running"**.
- No `mitmdump`/python process in `ps aux`.
- `$PPP_DATA/proxy.log` (mtime **01:09**, never updated since) shows mitmdump started all
  the WG listeners then **died on startup**:
  ```
  [01:09:05.483] Failed to bind UDP socket to 0.0.0.0:51838
  Caused by: Address already in use (os error 48)
  Errors logged during startup, exiting...
  ```
  (also 51827.) mitmdump **exited** — so there is no listener on the WG endpoint ports and no
  addon to sign certs or write flows.
- **`$PPP_DATA/flows.jsonl` does not exist** (searched all of `$PPP_DATA` and the opencode
  temp tree). So Observation #1 in the report is stale/from an earlier run; it does not hold now.

### 2. The guest tunnel was never brought up (provision.sh never ran to completion)
`assets/provision.sh` installs `/etc/wireguard/wg0.conf`, runs `wg-quick up wg0`, then
`ip route replace default dev wg0`, then `trust_ca()` (drops `mitmproxy.crt` anchor), then
`mark_provisioned` (`/var/lib/ppp/.provisioned`). In the guest, **none** of that is present:
- `/etc/wireguard/` is **empty** (no `wg0.conf`).
- `wg0` link absent; `wg show` empty.
- default route is `via 192.168.127.1 dev eth0` (the untouched gvproxy DHCP default) —
  provision.sh's `ip route replace default dev wg0` never happened.
- `/etc/pki/ca-trust/source/anchors/mitmproxy.crt` is **ABSENT**.
- `/var/lib/ppp/.provisioned` marker **absent**.
- `/tmp/provision.sh` and `/tmp/ppp-wg0.conf` are **absent**.
- guest `journalctl` shows the ONLY provisioning-time actions were the host-CA copy +
  `update-ca-trust` at 01:09 (from the podman-machine CA import path), then nothing until
  today's manual `wg show`/`ls` probes. No `ppp-provision:` log lines at all.

So provision.sh either was never invoked, or aborted before `bring_up_tunnel` — consistent
in time with the proxy failing to start at 01:09 (a caller that saw the proxy fail would not
have a healthy tunnel to bring up).

### 3. Why the guest still SUCCEEDS with a Zscaler cert (not a TLS failure)
The guest's system trust contains the **host** CA bundle: provision copied
`.../ppp-e2e-manual/host-ca-certs.pem` → `/etc/pki/ca-trust/source/anchors/` and ran
`update-ca-trust`. That host bundle (177 certs) includes the **Zscaler Root CA** and GSA
roots (decoded subject: `O=Zscaler Inc., CN=Zscaler Root CA`; plus `CN=ECOH2S-ROOTCA01`
etc.). It contains **zero** mitmproxy certs (`grep -c -i mitmproxy` = 0). Therefore the guest
trusts Zscaler's re-signed leaf and TLS verifies — a direct egress that the corporate proxy
transparently inspects. If our tunnel/proxy were in play, the guest would instead see
`CN=mitmproxy`; it does not, because the mitm CA anchor was never installed and there is no
mitm proxy in the path anyway.

---

## Root cause (WHERE the failure is)

Primary failure = **the mitmdump daemon crashed on startup** with `EADDRINUSE` on WG UDP
ports **51827** and **51838**, so it never came up. With no proxy:
- no WireGuard server listeners exist on the host → the guest can never complete a handshake;
- provision's `bring_up_tunnel`/`wait_for_tunnel` would fail (30s no-handshake, fails closed),
  which is consistent with wg0 never existing in the guest.

Note the port-registry only has **51820** active for one sandbox, yet the supervisor launched
listeners for the entire pool 51820–51851 and two of those ports (51827/51838) were already in
use by *something else* on the host at 01:09 (nothing holds them now — transient). Because
mitmdump binds all `--mode wireguard` ports in one process, **one busy pool port takes down the
whole proxy** (all-or-nothing startup), including the port the live sandbox actually uses.

This is NOT a routing regression in provision.sh's route ordering, and NOT a
test-assertion/stderr-capture bug. The `curl | grep "issuer"` correctly captured stderr (`-v`
goes to stderr, `2>&1` merged it) and correctly shows Zscaler. The product really is not
intercepting because the proxy isn't running and the tunnel was never established.

---

## Proposed minimal fixes (not applied — diagnosis only)

1. **Make daemon startup resilient to a busy pool port (highest value).** A single
   `EADDRINUSE` on any pooled WG port currently kills the entire proxy and thus every
   sandbox. Options, smallest first:
   - Preflight-probe each WG UDP port before launch and **skip/reallocate** ports already in
     use (and never launch listeners for ports not in the active registry — here only 51820
     is active, so launching 51820–51851 is unnecessary and is what collided).
   - Or scope the launched `--mode wireguard` set to **registered/active ports only** rather
     than the whole pool. See `internal/proxy/supervisor/supervisor.go:111` (`buildArgs`) and
     its `Ports` input.
2. **Fail loudly, not silently.** `ppp exec`/`ppp ls` happily reported the sandbox as
   `running` while its egress is completely uncontrolled. A running sandbox whose tunnel/proxy
   is down should surface as unhealthy (the isolation guarantee is void), not `running`.
3. **Recovery for THIS sandbox:** restart the daemon (`ppp daemon start`) once the port
   collision is resolved, then re-run provisioning so `wg0` comes up, the default route moves
   to `dev wg0`, and `mitmproxy.crt` is trusted. Then re-run the decisive experiment; success
   criteria: `wg show` counters rise, a `flows.jsonl` line appears, and the guest sees
   `issuer: CN=mitmproxy`.

## Verification of "not a test artifact"
Re-ran the guest curl capturing merged stderr explicitly and via `curl -w` (which reads the
actual connection metadata, independent of stderr): both show a direct connection to the
origin IP on :443 with a Zscaler-issued leaf and no wg0 in the path. Definitive.

_No changes committed. `ppp-e2e-manual` left running for follow-up._

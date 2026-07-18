# RESEARCH — T1: In-guest WireGuard routing on macOS/libkrun (GSA-TTS/ppp #4)

**Type:** decision memo (no build). **Scope:** how the Fedora CoreOS (FCOS) guest of a
Podman Machine `libkrun` VM must be configured so `wg0` with `AllowedIPs = 0.0.0.0/0`
tunnels all egress to the host `mitmdump` WireGuard endpoint without a routing loop.

---

## TL;DR decision

- **Use `Table = off` in `[Interface]` + manual routes.** Do **not** rely on wg-quick's
  default `Table = auto` fwmark/`suppress_prefixlength` machinery. The default *can* be made
  to work, but the explicit-manual path is deterministic, auditable, and provider-portable —
  which is exactly what Risk #9 asks for.
- The gvproxy gateway is **`192.168.127.1`** for the default subnet, which Podman Machine
  does not override on the apple/libkrun backends — but treat it as **derived, not
  hard-coded**: read `ip route show default` and fall back to `192.168.127.1`.
- **DNS: v1 = drop `DNS = 10.0.0.53` and let gvproxy (`192.168.127.1`) resolve.** DNS *would*
  work through the tunnel in principle, but the `DNS=` line triggers wg-quick `resolvconf`
  which is fragile on FCOS and its failure aborts the whole `up`. The off-policy DNS leak is
  acceptable for v1 and is explicitly flagged as residual risk (see below). (This overrides
  the spec §5.2 "v1 keeps DNS in-tunnel" note; rationale below.)
- **IPv6: disable it in the guest via sysctl.** Correct posture — mitmproxy emits
  IPv4-only `AllowedIPs = 0.0.0.0/0` (no `::/0`), so any IPv6 egress bypasses the tunnel/policy.

---

## 1. The off-tunnel `/32` route to the WG `Endpoint`

### Is the gvproxy gateway always `192.168.127.1` on libkrun?

Effectively yes for the default configuration, but derive it rather than hard-code it:

- gvisor-tap-vsock defaults the virtual network to `--subnet 192.168.127.0/24`, and derives
  `gatewayIP = first usable = 192.168.127.1`, `deviceIP = 192.168.127.2` (the guest),
  `hostIP = last usable = 192.168.127.254`.
  Source: `containers/gvisor-tap-vsock` `cmd/gvproxy/config.go:124-127, 205-239`
  (`config.Stack.GatewayIP = ... // for default subnet: 192.168.127.1`,
  `config.Stack.DeviceIP = ... // 192.168.127.2`).
  Also `pkg/types/configuration.go` (`GatewayIP`, `DeviceIP`, `HostIP` fields).
- Podman's apple/libkrun backends start gvproxy via
  `apple.StartGenericNetworking(mc, cmd)` and only append the vfkit socket
  (`cmd.AddVfkitSocket(...)`); they do **not** pass `--subnet`/`--gatewayIP`, so gvproxy's
  defaults stand.
  Source: `containers/podman` `pkg/machine/libkrun/stubber.go:90-92` and
  `pkg/machine/applehv/stubber.go:106-108` (both call `apple.StartGenericNetworking`);
  `pkg/machine/apple/apple.go:361-371` (`StartGenericNetworking` → `AddVfkitSocket` only).
- Therefore in the guest `ip route show default` → `default via 192.168.127.1`.

**Robust rule:** parse the gateway from `ip route show default` (first `via <gw>`), and if
absent, default to `192.168.127.1`. Do not trust a compiled-in constant alone — a user could
have a subnet override, and other providers (WSL/qemu) differ (Risk #9).

### Exact command and ordering

Because we choose `Table = off`, wg-quick installs **no** routes and **no** fwmark/ip-rule
machinery (verified in wg-quick source: `add_route()` returns immediately when
`$TABLE == off`, and `add_default()` — which is the only thing that sets `fwmark`,
`ip rule ... not fwmark`, and `ip rule ... suppress_prefixlength 0` — is never reached).
Source: `git.zx2c4.com/wireguard-tools` `src/wg-quick/linux.bash`, functions
`add_route()`, `add_default()`, `cmd_up()`.

So we own all routing. Provision order (all idempotent):

```
# --- run BEFORE `wg-quick up` ---
# 1. Resolve endpoint IP + default gateway from the config and the live route table.
EP=$(awk -F'[ :]+' '/^Endpoint/{print $3}' /etc/wireguard/wg0.conf)      # endpoint host
GW=$(ip route show default | awk '/default/{print $3; exit}')             # e.g. 192.168.127.1
GW=${GW:-192.168.127.1}                                                   # fall back

# 2. Pin the encrypted-datagram path to the endpoint OFF-tunnel, via gvproxy. (/32 = longest prefix)
ip route replace "$EP/32" via "$GW"

# --- bring the tunnel up (Table=off => wg-quick adds NO routes/rules) ---
# 3.
wg-quick up wg0        # or: systemctl enable --now wg-quick@wg0

# --- run AFTER `wg-quick up` (we install the catch-all ourselves) ---
# 4. Send everything else into the tunnel. (/32 endpoint route above still wins by longest-prefix.)
ip route replace default dev wg0
```

Notes:
- Step 2 **must** precede step 4 (and precede any handshake attempt): the WireGuard handshake
  itself is UDP to `$EP:port`; if `default dev wg0` were installed first, the handshake packets
  would recurse into the not-yet-established tunnel → loop / no handshake. This is the exact
  failure mitmproxy documents ("cannot proxy its own host's traffic"; ppp-spec §3.1 table,
  "Host-self-proxy: Cannot proxy its own host's traffic (would loop)").
- With `Table = off` there is **no** `suppress_prefixlength`/`fwmark` interaction to reason
  about — the sole reason to consider them is gone. A plain `/32 via gw` + `default dev wg0`
  in the **main** table is unambiguous: longest-prefix match routes the endpoint out via
  gvproxy and everything else into `wg0`.

### Why `Table = off` over the default `Table = auto`

`Table = auto` *does* work in theory: wg-quick marks WG's own encrypted packets with an
`fwmark`, adds `ip rule add not fwmark <t> table <t>` (so only *un*marked traffic uses the
wg table) and `ip rule add table main suppress_prefixlength 0` (so `/0` in main is ignored but
a `/32` endpoint route is honored). WG's outbound datagrams carry the fwmark, so they escape
the wg table and follow the main table's route to the endpoint.
Source: wg-quick `add_default()` in `src/wg-quick/linux.bash`.

But:
- It relies on the guest kernel honoring `fwmark` + policy routing + `src_valid_mark`
  correctly (wg-quick even sets `net.ipv4.conf.all.src_valid_mark=1`). That's more moving
  parts to validate per-provider than Risk #9 wants.
- With `Table = auto` you'd *still* want the explicit endpoint route in some paths, so you get
  the complexity of both models.
- `Table = off` gives one mental model: "we install exactly two routes; longest prefix wins."
  Deterministic, greppable, portable to WSL/qemu (Risk #9 asks for per-provider validation).

**Decision: `Table = off` + the four steps above.** Update spec §5.2 step 3 accordingly (it
currently prefers the explicit-exception-route-under-`auto` approach and lists `Table = off`
only as an alternative).

---

## 2. DNS decision

### Facts

- mitmproxy's generated client config hard-codes `DNS = 10.0.0.53`
  (`mode_servers.py` `WireGuardServerInstance.client_conf()`), alongside
  `Address = 10.0.0.1/32` — both string literals, no option to change.
- mitmproxy **does** answer `10.0.0.53`: its default-loaded `DnsResolver` addon treats a
  WireGuard flow whose `server_conn.address == ("10.0.0.53", 53)` as a resolve request and
  answers it using `mitmproxy_rs.dns.DnsResolver` seeded from the **host's** system name
  servers (or `dns_name_servers` option), falling back to `getaddrinfo`.
  Source: `mitmproxy/addons/dns_resolver.py` `_should_resolve()` (the `== ("10.0.0.53", 53)`
  check) and `name_servers()`/`resolver()`.
  So DNS-through-the-tunnel is **functional** in principle.
- BUT wg-quick's handling of `DNS =` runs `resolvconf -a tun.wg0 -m 0 -x` on up.
  Source: wg-quick(8) man page (`DNS` bullet) and `set_dns()` in `src/wg-quick/linux.bash`.
  On FCOS this requires a working `resolvconf` (openresolv) or the systemd-resolved
  resolvconf shim to be present and wired to `/etc/resolv.conf`. If `resolvconf` is missing or
  misconfigured, `set_dns()` fails and, because it runs inside `cmd_up()` under
  `trap 'del_if; exit'`, it **tears the interface back down** — a hard `up` failure.
- gvproxy runs its own DNS server on `192.168.127.1` and FCOS's DHCP-provided
  `/etc/resolv.conf` already points there.
  Source: gvisor-tap-vsock `README.md` (Gateway/DNS sections: "nameserver 192.168.127.1").

### Trade-off

- **In-tunnel DNS (`DNS = 10.0.0.53`):** queries are visible to mitmproxy → on-policy, and
  DNS-rebinding defense (spec §5.4) can see them. Cost: depends on the fragile
  wg-quick/resolvconf path on FCOS; a failure blocks the whole tunnel.
- **gvproxy DNS (`192.168.127.1`):** rock-solid, zero extra config, but resolution happens on
  the host **outside** the mitmproxy policy layer → **DNS leaks off-policy** (names are not
  logged/inspected by the addon; DNS-rebinding defense at the DNS layer is bypassed). Note the
  actual *connections* still go through the tunnel and are policy-checked by host/IP, so this
  is a DNS-visibility leak, not an egress-policy bypass.

### Decision (v1)

**Drop the `DNS = 10.0.0.53` line; rely on gvproxy's resolver at `192.168.127.1`.**

Rationale: v1 prioritizes a tunnel that comes up reliably on FCOS over DNS observability. The
egress **policy boundary is intact** either way (all TCP/UDP to real destinations still
traverses `wg0` and is matched by the addon on host/CIDR). The residual gap is that hostnames
aren't resolved *inside* policy, so name-based allow/deny + DNS-rebinding defense operate on
the resolved IP the guest connects to, not on the DNS query. Mitigate by keeping the addon's
IP-based rebinding/allowlist checks (spec §5.4 already rejects allowlisted hostnames resolving
to private/loopback/metadata IPs at connect time).

**This is a deliberate deviation from spec §5.2's "v1 keeps DNS in-tunnel" note** and should
be recorded (a short ADR in `docs/decisions/`, since it touches the policy-visibility posture).
Revisit for v1.1: if we want on-policy DNS, keep `DNS =` but replace wg-quick's resolvconf path
with an explicit `PostUp`/manual `/etc/resolv.conf` write to `10.0.0.53` (avoids the
resolvconf dependency) — and validate it empirically.

---

## 3. IPv6 decision

**Disable IPv6 in the guest via sysctl. Correct.**

- mitmproxy's client config emits `AllowedIPs = 0.0.0.0/0` only — **no `::/0`**
  (`mode_servers.py` `client_conf()`), consistent with the ppp-spec §3.1 table note
  "IPv6 — Excluded from generated `AllowedIPs` by default".
- Consequence: if the guest has working IPv6, any AAAA-preferring client would send IPv6
  traffic that is **not** captured by `wg0` → it egresses off-tunnel, off-policy (or blackholes
  through gvproxy, which per its README does not forward ICMP and is IPv4-centric). Either way
  it's an uncontrolled path.
- Fail-closed posture: turn IPv6 off so everything is forced onto the IPv4 tunnel.

Command (already in spec §5.2 step 7), persist it:

```
sysctl -w net.ipv6.conf.all.disable_ipv6=1
sysctl -w net.ipv6.conf.default.disable_ipv6=1
# persist:
printf 'net.ipv6.conf.all.disable_ipv6=1\nnet.ipv6.conf.default.disable_ipv6=1\n' \
  > /etc/sysctl.d/99-ppp.conf
```

(Two-key form so newly-appearing interfaces are also covered.)

---

## Consolidated provision order (relative to `wg-quick up`)

1. `rpm-ostree install wireguard-tools` (first boot only; reboot if required). *(spec §5.2 s1)*
2. Write `/etc/wireguard/wg0.conf` (`chmod 600`) with `Table = off` added to `[Interface]`
   and the `DNS =` line **removed**.
3. `sysctl` disable IPv6 (both keys) + persist to `/etc/sysctl.d/99-ppp.conf`.
4. `EP=<endpoint ip from wg0.conf>`; `GW=$(ip route show default|awk '/default/{print $3;exit}')`;
   `GW=${GW:-192.168.127.1}`.
5. `ip route replace "$EP/32" via "$GW"`   ← **off-tunnel endpoint pin, BEFORE up**
6. `wg-quick up wg0` (or `systemctl enable --now wg-quick@wg0`) — installs no routes/rules.
7. `ip route replace default dev wg0`   ← catch-all into tunnel, AFTER up
8. `until wg show wg0 >/dev/null 2>&1; do sleep 1; done`  (handshake up)
9. `curl -s http://mitm.it/cert/pem -o /etc/pki/ca-trust/source/anchors/mitmproxy.crt && update-ca-trust`
   (must be after tunnel + DNS work).

Idempotency: steps 5 and 7 use `ip route replace`; step 3 is declarative; step 2 overwrites.
Gate step 1 (and any image pull) behind the `.provisioned` marker as in spec §5.2.

---

## Cited sources

- wg-quick behavior (Table off/auto, fwmark, suppress_prefixlength, DNS/resolvconf):
  - man page: https://www.man7.org/linux/man-pages/man8/wg-quick.8.html (`Table`, `DNS` bullets)
  - source: https://git.zx2c4.com/wireguard-tools/plain/src/wg-quick/linux.bash
    (`add_route()`, `add_default()`, `set_dns()`, `cmd_up()`)
- gvproxy default subnet/gateway/device IPs (192.168.127.1/.2/.254):
  - `containers/gvisor-tap-vsock` `cmd/gvproxy/config.go:124-127, 205-239`
  - `containers/gvisor-tap-vsock` `pkg/types/configuration.go` (GatewayIP/DeviceIP/HostIP)
  - README (DNS at 192.168.127.1; ICMP not forwarded):
    https://github.com/containers/gvisor-tap-vsock/blob/main/README.md
- Podman libkrun/applehv start gvproxy with defaults (no subnet/gateway override):
  - `containers/podman` `pkg/machine/libkrun/stubber.go:90-92`
  - `containers/podman` `pkg/machine/applehv/stubber.go:106-108`
  - `containers/podman` `pkg/machine/apple/apple.go:361-371`
- mitmproxy client config (`Address = 10.0.0.1/32`, `DNS = 10.0.0.53`, `AllowedIPs = 0.0.0.0/0`,
  no `::/0`) and DNS resolver behavior:
  - `mitmproxy/mitmproxy` `mitmproxy/proxy/mode_servers.py`
    (`WireGuardServerInstance.client_conf()`)
  - `mitmproxy/mitmproxy` `mitmproxy/addons/dns_resolver.py`
    (`_should_resolve()` == ("10.0.0.53",53); `name_servers()`/`resolver()`)
- ppp design context: `docs/explorations/ppp-spec.md` §3.1 (lines 64-113), §5.2 (lines 245-267),
  Risk #9 (line 907).

---

## Residual uncertainty → empirical spike needed

1. **Handshake-vs-route timing on gvproxy (highest value).** Confirm on a real macOS libkrun
   machine that with `Table = off`, `ip route replace $EP/32 via 192.168.127.1` *before* `up`
   and `ip route replace default dev wg0` *after*, `ip route get <endpoint>` resolves via
   gvproxy (not `wg0`) and the handshake completes (`wg show wg0` shows a recent handshake).
   This is the exact Risk #9 validation.
2. **gvproxy relaying WG UDP to a host-side mitmdump port.** gvproxy is userspace NAT; verify
   guest→`192.168.127.1`→host `mitmdump` UDP (the WG endpoint) actually round-trips (UDP
   forwarding + the `Endpoint = <host-reachable-ip>:<port>` rewrite the supervisor does in
   spec §5.2/§5.3). Endpoint host in the config is mitmproxy's auto-detected local IP; confirm
   that IP is reachable from inside the guest via gvproxy (it may need to be the gvproxy
   `hostIP`/NAT alias rather than the host's LAN IP).
3. **DNS leak acceptability sign-off.** The v1 "gvproxy DNS" decision needs a product/security
   nod (ADR) since it means DNS queries are not policy-inspected. If unacceptable, spike the
   `PostUp`-writes-`/etc/resolv.conf`-to-`10.0.0.53` variant and confirm resolution works
   through the tunnel on FCOS without wg-quick's resolvconf path.
4. **FCOS `resolvconf` reality.** Verify whether FCOS ships a working resolvconf shim at all —
   this is the crux that makes the "drop DNS=" decision necessary; confirm empirically before
   locking it in.

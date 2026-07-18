# Research T2 — WireGuard client in the FCOS guest (install + wg-quick vs userspace)

**Ticket:** GSA-TTS/ppp #5
**Type:** RESEARCH — decision memo, not a build.
**Scope:** macOS + libkrun provider (v1 primary). WSL out of scope.
**Date:** 2026-07-16
**Author:** agentic-coding-team (AI-assisted)

---

## TL;DR decision

1. **Do NOT `rpm-ostree install wireguard-tools`.** It is already present in the
   Podman `machine-os` base image. The whole "reboot vs `--apply-live`" question
   is **moot** for the primary (macOS/libkrun) path.
2. **Use kernel WireGuard via `wg-quick@wg0` (systemd).** The `wireguard.ko`
   module ships in the guest kernel, loads, and creates a real kernel WG
   interface. **No userspace client (wireguard-go / boringtun) is needed** on
   macOS/libkrun.
3. **Provision becomes simpler:** drop the install step entirely; guard the
   provision only on an idempotency marker for the truly one-shot bits (CA
   fetch is idempotent by overwrite; `podman pull` is the real one-shot cost).

This directly **retires Risk #2** (rpm-ostree layering semantics for WG install)
for the macOS path, and de-scopes the userspace-fallback branch to
Windows/WSL only (already tracked as Risk #1, out of scope for v1).

---

## Evidence (live spike, primary source)

Ran a throwaway Podman Machine on this host and tore it down afterward
(approved). Provider confirmed **libkrun** (`krunkit` 1.3.2 / libkrun 1.19.4).

**Image:** `quay.io/podman/machine-os:6.0` pulled by `podman machine init`
(matches installed `podman 6.0.1`). Guest identity from `/etc/os-release`:

```
VERSION="44.20260621.3.1 (CoreOS)"      VERSION_ID=44
VARIANT="Podman Machine OS"             VARIANT_ID=podman-machine-os
OSTREE_VERSION='44.20260621.3.1'        IMAGE_VERSION='44.20260621.3.1'
```

**Kernel:** `7.0.12-201.fc44.aarch64`

### Q1 — Is `wireguard-tools` present? Does install need a reboot?

`wireguard-tools` is **already installed in the base image** — no layering, no
reboot, nothing to do:

```
$ command -v wg        → /usr/bin/wg
$ command -v wg-quick  → /usr/bin/wg-quick
$ rpm -q wireguard-tools → wireguard-tools-1.0.20260223-1.fc44.aarch64
```

It is part of the **immutable base commit**, not a layered package:
- `/usr/bin/wg` lives on the read-only `/usr` mount
  (`/dev/vda4 on /usr type xfs (ro,relatime,...)`).
- The deployment origin is a plain container-image ref with **no layered
  packages**: `container-image-reference=ostree-unverified-registry:quay.io/podman/machine-os:6.0`.
- The `wg-quick@.service` systemd template is shipped
  (`/usr/lib/systemd/system/wg-quick@.service`).

> Note on *why*: `wireguard-tools` is a standard Fedora package and is pulled in
> transitively by the machine-os build (Fedora CoreOS base + the podman-machine-os
> package set; see `podman-image/build_common.sh`). Even though the machine-os
> `build_common.sh` PACKAGES array doesn't name it explicitly, the running 6.0
> image ships it — verified live. **Provisioning must not assume its absence, but
> also must not depend on it being layerable at runtime.**

**Reboot question is therefore N/A on this path.** For completeness (in case a
future machine-os drops it), the rpm-ostree reboot rules are:
- `rpm-ostree install <pkg>` is **offline by default** — it stages a *new
  deployment* that only takes effect on **reboot** (rpm-ostree administrator
  handbook: "every rpm-ostree operation is 'offline' … will only take effect
  when you reboot").
- `rpm-ostree install -A --apply-live <pkg>` (or `install <pkg>` then
  `apply-live`) can live-apply **pure package additions** with no reboot, via a
  transient overlayfs on `/usr` (rpm-ostree apply-live architecture doc).
  `wireguard-tools` is a pure userspace addition, so `--apply-live` *would* work
  without reboot **if** it were ever missing.
- Detection, if ever needed: after `rpm-ostree install`, check
  `rpm-ostree status` (or `--json`: compare booted `checksum` vs pending
  deployment / `packages`). But **do not build this into v1** — it's dead code
  for the shipping image.

### Q2 — Kernel WireGuard available, or userspace required?

**Kernel WireGuard is fully available.** No userspace client required.

```
# module ships in the kernel tree:
/usr/lib/modules/7.0.12-201.fc44.aarch64/kernel/drivers/net/wireguard/wireguard.ko.xz
$ modinfo wireguard → version: 1.0.0, author: Jason A. Donenfeld, license: GPL v2

# loads cleanly:
$ sudo modprobe wireguard   → OK
$ lsmod | grep wireguard    → wireguard 102400 0  (+ libcurve25519, ip6_udp_tunnel, udp_tunnel)

# real kernel WG interface can be created directly:
$ sudo ip link add dev wgtest type wireguard   → OK
$ sudo wg show wgtest                           → interface: wgtest

# full wg-quick round-trip with a real config:
$ sudo wg-quick up wg0     → ip link add dev wg0 type wireguard; ... ; UP OK
$ sudo wg show wg0         → interface up, listening port assigned
$ sudo wg-quick down wg0   → OK
```

`/dev/net/tun` also exists (`crw-rw-rw- 10,200`), so a userspace client *could*
run if ever needed — but it is **not needed** here. Userspace WireGuard
(wireguard-go / boringtun) is only the fallback for environments whose kernel
lacks the module (e.g. some WSL2 kernels — Risk #1, out of scope for v1).

**No change to the provision for macOS.** `systemctl enable --now wg-quick@wg0`
against the kernel module is the correct, simplest path, exactly as spec §5.2
step 4 assumes.

### Q3 — Idempotency: one-shot vs every-boot

Because there is no package install, the one-shot set shrinks. Recommended
gating (marker `/var/lib/ppp/.provisioned`, which lives on writable `/var` and
survives reboots but not `podman machine rm`):

| Step | Cost | Cadence | Rationale |
|---|---|---|---|
| ~~`rpm-ostree install wireguard-tools`~~ | — | **removed** | already in base image |
| `modprobe wireguard` | cheap | every boot (idempotent no-op if loaded) | belt-and-suspenders; `wg-quick up` also auto-loads it |
| write `/etc/wireguard/wg0.conf` + `chmod 600` | cheap | every boot | config may change on reattach; overwrite is idempotent |
| pin off-tunnel `/32` route to endpoint via gvproxy gw | cheap | every boot | routes are per-boot kernel state; use `ip route replace` |
| `systemctl enable --now wg-quick@wg0` | cheap | every boot (idempotent) | `enable` is idempotent; `--now` no-ops if active |
| `until wg show wg0; do sleep 1; done` | cheap | every boot | liveness gate |
| fetch mitm CA → anchors + `update-ca-trust` | medium | every boot OK, but **safe to gate** | overwrite-idempotent; must run *after* tunnel up (mitm.it is in-tunnel) |
| `sysctl` disable IPv6 (+ `/etc/sysctl.d/99-ppp.conf`) | cheap | every boot | declarative; matches mitmproxy IPv4-only AllowedIPs |
| `podman pull ghcr.io/ppp/opencode:latest` | **expensive** | **one-shot** (gate on marker) | the real reason to have a marker at all |
| `touch /var/lib/ppp/.provisioned` | — | after first success | gates the pull |

**Net:** the only genuinely one-shot step left is the **image pull** (step 8).
Everything WireGuard-related is every-boot and inherently idempotent. The marker
is now essentially "have we already pulled the agent image", not "have we
installed WireGuard". Keeping the CA fetch every-boot is fine (cheap, overwrite),
but it may be gated too since the CA is stable per host.

---

## Recommended provision (macOS/libkrun, v1)

Exact command shape (runs every boot via `podman machine ssh <name> -- bash /tmp/provision.sh`):

```bash
#!/bin/bash
set -euo pipefail
MARKER=/var/lib/ppp/.provisioned

# WireGuard client is ALREADY installed in machine-os base image — do NOT rpm-ostree install.
# (Optional safety net: if a future image ever drops it, live-apply without reboot.)
if ! command -v wg-quick >/dev/null 2>&1; then
  sudo rpm-ostree install -A --apply-live --allow-inactive wireguard-tools
fi

sudo modprobe wireguard  # no-op if already loaded; wg-quick would load it anyway

# wg0.conf is delivered per-sandbox by the host (podman machine cp) — write + lock down.
sudo install -m 600 /tmp/wg0.conf /etc/wireguard/wg0.conf

# Keep the encrypted WG datagrams OFF the tunnel (avoid the AllowedIPs=0.0.0.0/0 loop).
ENDPOINT_IP=$(awk -F'[ :]+' '/^Endpoint/{print $3}' /etc/wireguard/wg0.conf)
GW=$(ip route show default | awk '{print $3; exit}')   # gvproxy gw, typically 192.168.127.1
sudo ip route replace "${ENDPOINT_IP}/32" via "${GW}"

sudo systemctl enable --now wg-quick@wg0
until sudo wg show wg0 >/dev/null 2>&1; do sleep 1; done

# CA + IPv6 (every boot; cheap + idempotent). mitm.it is in-tunnel, so AFTER tunnel is up.
curl -sf http://mitm.it/cert/pem | sudo tee /etc/pki/ca-trust/source/anchors/mitmproxy.crt >/dev/null
sudo update-ca-trust
echo 'net.ipv6.conf.all.disable_ipv6=1' | sudo tee /etc/sysctl.d/99-ppp.conf >/dev/null
sudo sysctl -w net.ipv6.conf.all.disable_ipv6=1

# One-shot: agent image pull (the only expensive step).
if [ ! -f "$MARKER" ]; then
  podman pull ghcr.io/ppp/opencode:latest
  sudo mkdir -p "$(dirname "$MARKER")" && sudo touch "$MARKER"
fi
```

**Spec edits implied (do NOT apply in this ticket — decision only):**
- §5.2 step 1 and the §4.2 / diagram line "`rpm-ostree install wireguard-tools
  (+ reboot if needed)`" should be replaced with: *"WireGuard is preinstalled in
  machine-os; only load the module / start `wg-quick`."* Keep an optional
  `--apply-live` safety net for image drift, but note it is not on the hot path.
- §5.2 idempotency-gate note: gate **only** the `podman pull` (step 8) behind the
  marker; WG steps are every-boot.
- Risk #2 (§13) can be **downgraded/closed for macOS**: rpm-ostree layering is
  not on the WG path. It remains relevant only if a future machine-os removes the
  package (low likelihood) or for the WSL path (Risk #1).

---

## Cited sources

- **Live spike (primary):** `podman machine init/start ppp-wg-spike` on this host,
  provider libkrun (`krunkit 1.3.2`, `libkrun 1.19.4`), image
  `quay.io/podman/machine-os:6.0`, guest FCOS 44 / kernel `7.0.12-201.fc44.aarch64`.
  Commands + outputs quoted inline above; machine destroyed after.
- **Podman machine-os build:** `podman-container-tools/podman-machine-os`
  `podman-image/Containerfile.COREOS` + `build_common.sh` (FCOS base + package
  set; confirms container-image origin, read-only `/usr`).
- **quay.io tags:** `quay.io/api/v1/repository/podman/machine-os/tag/` — `6.0`
  (matching podman 6.0.1), `6.1`, `next` all present.
- **rpm-ostree Administrator Handbook** (coreos.github.io/rpm-ostree/administrator-handbook):
  install is offline/reboot by default; `install -yA` / `apply-live` live-applies
  pure package additions; `status --json` fields for detecting layered state.
- **rpm-ostree apply-live architecture** (coreos.github.io/rpm-ostree/apply-live):
  transient overlayfs over `/usr`, distinguishes additions vs upgrades.
- **WireGuard install docs** (wireguard.com/install): Fedora ships
  `wireguard-tools` via `dnf`; kernel module is in-tree for modern kernels
  (userspace wireguard-go only where no kernel module).
- **ppp-spec.md** §3.2 (Podman Machine / FCOS), §5.2 (provision), §13 Risk #1/#2.

---

## Residual uncertainty (spike candidates)

1. **End-to-end tunnel to a real mitmproxy WG server** was **not** exercised in
   this spike — only interface up/down with a self-generated key and no peer. The
   load-bearing integration test (spec §13) — guest `wg0` peering with a
   host-side `mitmdump --mode wireguard@<port>`, off-tunnel `/32` route holding,
   `mitm.it` CA fetch resolving through the tunnel, and DNS via the hardcoded
   `10.0.0.53` in-tunnel resolver — still needs a dedicated spike.
2. **Image-version drift:** verified on `machine-os:6.0`. Confirm `wireguard-tools`
   remains in `6.1` / `next` (very likely; FCOS base package) and pin a supported
   machine-os range so the "preinstalled" assumption can't silently regress. The
   optional `--apply-live` safety net covers this but is unproven against a real
   drop.
3. **`--apply-live` path itself untested** here (never needed). If it's kept as a
   safety net, exercise it once against an image that lacks the package to confirm
   no reboot and correct `--allow-inactive` behavior.
4. **gvproxy gateway assumption** (`192.168.127.1`) read from `ip route` in the
   provision rather than hardcoded — confirmed the default-route parse works, but
   validate under the real WG-up scenario (item 1).

## Environment side effects from this research (for cleanup awareness)

- Installed via Homebrew (approved): tapped `slp/krun`, installed `krunkit`
  (+ deps `dtc`, `libepoxy`, `libkrunfw`, `molten-vk`, `virglrenderer`, `libkrun`,
  `gvproxy`). Needed to boot any libkrun machine on this host; left installed.
- Pulled `quay.io/podman/machine-os:6.0` into the local podman image store.
- Created and **removed** the throwaway machine `ppp-wg-spike` (no machines remain).

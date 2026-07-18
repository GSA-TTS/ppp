# `ppp` — Podman Plus Proxy

An isolated coding-agent sandbox runtime assembled from mature, well-tested OSS solutions — Podman Machine for microVMs, mitmproxy for traffic interception, WireGuard for transparent tunneling, and the host OS keychain for secret storage. `ppp` is a thin Go binary and embedded Python addon that orchestrates these tools to produce a CLI faithful in spirit to Docker's `sbx` ("Docker Sandboxes") — covering the same operational surface (sandbox lifecycle, network policy, secret injection, kits, templates) without reimplementing any of the underlying primitives.

The goal is **composition over invention**: every critical piece (microVM isolation, filesystem read-only and read-write mounts, network egress policy, credential injection) is delegated to a commoditized OSS tool that is good at what it does and has a track record and community behind it. `ppp` owns only the lifecycle glue, the policy engine, the credential-rewrite layer, and the developer UX.

| Critical isolation piece | OSS solution we delegate to | Why this tool |
|---|---|---|
| MicroVM with separate Linux kernel | **Podman Machine** (libkrun on macOS, WSL2 on Windows, qemu on Linux) | Mature VM lifecycle with first-class macOS and Windows support; backed by the Red Hat / containers ecosystem; no Docker dependency |
| Filesystem mounts (read-only + read-write) | **Podman Machine** (`podman run -v`/`podman machine ssh`) | Same surface as Podman itself — well-trodden mount paths |
| Transparent network interception | **mitmproxy** in WireGuard mode | Userspace (no root), cross-platform, mature TLS MITM with an addon pipeline; 9+ years of development; widely used in testing, security, and ML contexts |
| Secure tunnel transport for all VM traffic | **WireGuard** (via mitmproxy's userspace WireGuard server) | Modern, audited, minimal protocol; mitmproxy ships a userspace impl so no kernel module on the host |
| Secret storage on the host | **OS keychain** (macOS Keychain / Windows Credential Manager / Linux Secret Service) via `go-keyring` | The OS-native, encrypted, user-scoped store; nothing custom to maintain |
| Policy engine, addon logic, kit identity | `ppp` itself | The one place we don't delegate — coherence across the other four pieces is the actual product |

For more on `ppp`'s place in the broader agentic-coding-quickstart ecosystem alongside Docker's `sbx` and microsandbox's `msb`, see `acq-v2-design.md` (the `acq` quickstart wrapper that pluggably selects among the three isolation backends).

---

## 1. Goals & Non-Goals

### Goals

- Provide a CLI `ppp` with all top-level subcommands of the real Docker `sbx` **except `login` and `logout`**, with behavior faithful in spirit (not binary-compatible).
- Each sandbox is an isolated Linux micro-VM (**exactly one dedicated Podman Machine per sandbox — VMs are never shared between sandboxes**), with a separate kernel.
- All VM egress traffic is transparently tunneled through a **single** host-side mitmproxy process via WireGuard — no application-level proxy env, no per-app config.
- A single mitmproxy instance audits network policy across **all** sandboxes simultaneously. Each sandbox is assigned a distinct WireGuard server instance (unique UDP port + keypair) within the same mitmdump process, and a distinct inner tunnel IP (`10.0.0.1`–`10.0.0.N`). The addon distinguishes sandboxes by the **receiving WireGuard instance** — i.e. the flow's proxy-mode listen port (`flow.client_conn.proxy_mode.listen_port()` on mitmproxy 12.2.3; **not** `flow.client.sockname`, which surfaces the inner destination on that version). This binding is cryptographic (each port has its own keypair) and cannot be forged from inside the guest. (The inner tunnel IP is *not* a reliable discriminator — mitmproxy hardcodes `Address = 10.0.0.1/32` in every generated client config, and even after we rewrite it per sandbox a `sudo`-capable agent could reassign its own `wg0` address; the listen port cannot be changed by the guest.)
- Policies (allow/deny) and secrets (API keys, custom tokens) are enforced/injected **from the host** — secrets never enter the sandbox.
- Supported host platforms: **macOS and Windows** (primary); Linux works but is best-effort.
- Supported agent: **`opencode`** only at v1 (agent registry is extensible).
- Go binary, Cobra CLI tree, shells out to `podman` and `mitmdump` with an embedded Python addon.

### Non-Goals

- GPU passthrough.
- Multi-tenant / remote "org" policy source (`--source org` is a no-op stub).
- The closed-source "Gordon" assistant / TUI assistant persona.
- Docker Desktop integration (standalone product CLI, not a `docker` subcommand).

---

## 2. What Docker's `sbx` Is (Background)

Docker's `sbx` is their standalone CLI for **Docker Sandboxes** — a product for running AI coding agents (Claude, Codex, Copilot, Cursor, Gemini, etc.) inside isolated, policy-controlled sandbox environments "powered by Docker." Per Docker's own architecture docs (`docs.docker.com/ai/sandboxes/`):

> "Each sandbox runs inside a lightweight microVM with its own Linux kernel. Unlike containers, which share the host kernel, a sandbox VM cannot access host processes, files, or resources outside its defined boundaries."

Key subsystems the real product provides:

- **Agent workloads** — `sbx run AGENT PATH` launches a supported agent inside a containerized sandbox with the host workspace mounted (or, with `--clone`, against a private in-container git clone wired back via a `sandbox-<name>` git remote).
- **`sandboxd` daemon** — a host-side process that enforces network policy and proxies sandbox traffic.
- **Network policy** — `sbx policy allow/deny network <resources>` with deny-wins, `**` = block-all, exact/wildcard domains, IPs, optional `:port`, global or per-sandbox (`--sandbox`).
- **Secrets** — `sbx secret set anthropic …` stores API keys on the host; the proxy authenticates outbound requests on the agent's behalf so keys never enter the sandbox. Custom secrets (`set-custom`) use a placeholder that the proxy swaps on outbound headers.
- **Kits** — declarative YAML artifacts that extend an agent with extra credentials, network policies, env vars, startup commands, and files. Distributable via OCI registries.
- **Templates** — saved sandbox snapshots reused via `sbx run -t TAG`.
- **Lifecycle** — `create/run/stop/rm/exec/cp/ls/ports/diagnose/reset/setup/tui/version/completion`.

Our clone replaces the "containerized sandbox inside Docker" with a **Podman Machine VM per sandbox**, and replaces `sandboxd` with a **single mitmdump process** running multiple WireGuard server instances.

---

## 3. Verified External Dependencies

### 3.1 mitmproxy (WireGuard mode)

Verified against `docs.mitmproxy.org/stable/concepts/modes/`, `howto-wireguard/`, and the Python source (`mitmproxy/proxy/mode_servers.py`, `mode_specs.py`).

| Property | Value | Source |
|---|---|---|
| Min version | 9.0.0 (Oct 2022) — promoted to "Recommended" in current docs | CHANGELOG |
| Invocation | `mitmdump --mode wireguard:<keys.conf>@<port> -s <addon.py>` | modes docs |
| Default UDP port | `51820` (configurable via `@<port>` or `listen_port`) | `mode_specs.py` `default_port = 51820` |
| Host privileges | **None** — userspace WireGuard server, no root/admin | docs: "no administrative privileges are necessary" |
| Platform support | Linux, macOS, Windows | modes docs (no platform-restriction tag); `mitmproxy_rs` ships per-platform packet sources |
| Transparency | Truly transparent — intercepts TCP **and UDP** (not just HTTP/S). Top layer is `TransparentProxy`. | `mode_servers.py` `make_top_layer` |
| CA cert endpoint | `http://mitm.it/cert/pem` (magic hostname, intercepted locally) | `addons/onboardingapp/__init__.py` |
| Addon API | Full event-based pipeline (`request`, `response`, `tls_start_client`, `tcp_message`, `udp_message`, …) | `docs.mitmproxy.org/stable/addons/overview/` |
| IPv6 | Excluded from generated `AllowedIPs` by default (docs note limited IPv6 support) | modes docs "Limited support for IPv6 traffic" |
| Host-self-proxy | **Cannot proxy its own host's traffic** (would loop) | modes docs |
| Multi-instance | **One process can run multiple WG servers** via repeatable `--mode wireguard:<path>@<port>` | `options.py` `mode: Sequence[str]` "Can be passed multiple times"; docs show `--mode wireguard:wg-keys-1.conf --mode wireguard:wg-keys-2.conf@51821` |

#### Generated client config (from `WireGuardServerInstance.client_conf()`)

```ini
[Interface]
PrivateKey = <client_key>
Address = 10.0.0.1/32      # HARDCODED — no option to change
DNS = 10.0.0.53            # hardcoded

[Peer]
PublicKey = <server_pubkey>
AllowedIPs = 0.0.0.0/0
Endpoint = <auto-detected local IP>:<port>
```

**Critical detail: `10.0.0.1/32` is a string literal** in `client_conf()` (`mode_servers.py:406`) — no option, no flag, no config field exists to change it. The Go proxy supervisor **rewrites** `Address = 10.0.0.1/32` to `Address = 10.0.0.N/32` for sandbox N before writing `wg0.conf` (useful for readability, per-sandbox routing sanity, and disambiguating flows in logs), but **the inner IP is NOT the security-critical sandbox discriminator** — see below. Each sandbox gets its own keypair (from its own `--mode wireguard:<keys.conf>@<port>`), so there's no crypto-state collision.

**Sandbox identification — use the listen port (`sockname`), not the inner IP (`peername`).** Verified by reading the Rust/Python data path and by a live two-instance spike:
- Inside `mitmproxy_rs`, the decrypted **inner IPv4 source address** (`packet.src_addr()`, `wireguard.rs:261/267`) becomes the connection's `src_addr`, which surfaces to the addon as `flow.client.peername` / `flow.client.address` (`tcp.rs:120,153` → `task.rs:77` → `server.py:475`). That value is whatever address `wg-quick` assigned to the guest's `wg0` — i.e. the `Address =` line we wrote. A `sudo`-capable agent (the Docker `sbx` model gives the agent sudo; §5.7) could `ip addr change wg0` to another sandbox's `10.0.0.M` and thereby inherit that sandbox's policy + injected secrets. **So `flow.client.address` is spoofable and must not be the trust anchor.**
- The **server-side socket** is per-instance: each `--mode wireguard@<port>` binds its own UDP socket (`create_and_bind_udp_socket(self.listen_addr)`, `wireguard.rs:85`), and the receiving instance is identified by the flow's **proxy mode** — `flow.client_conn.proxy_mode.listen_port()` / `.full_spec` (verified on mitmproxy 12.2.3 via live e2e spike; `flow.client.sockname` is the inner *destination* on that version, not the listen port). Because each port has a distinct keypair and the guest cannot move its traffic to a different host UDP port without the corresponding server private key, **the listen port is a cryptographically-bound, unspoofable discriminator.** The addon maps `listen_port → sandbox_name`.
- **Spike confirmation** (`mitmdump 12.2.3`, two instances `@51820` + `@51821` in one process): both client configs were emitted, each fenced by a line of exactly 60 hyphens; keys files were auto-generated as JSON `{server_key, client_key}` when the paths did not pre-exist (confirming the "don't pre-create empty files" fix); and both configs carried the hardcoded `Address = 10.0.0.1/32` with distinct `Endpoint = <ip>:51820` / `:51821` lines — confirming the port is the only per-instance distinguisher present in the config.

**⚠️ `process_outgoing_packet` fallback:** when no peer matches a destination IP, `mitmproxy_rs` logs `"No peer found for IP …, falling back to first peer"` and routes to the first peer (`wireguard.rs:326-333`). With 80 instances in one process this is worth exercising under load; per-instance keypairs make cross-routing unlikely, but the addon should never rely on return-path IP identity.

**How the config is emitted (verified against `mode_servers.py:377-379`):** each WG instance logs its client config once at startup via `logger.info("-" * 60 + "\n" + conf + "\n" + "-" * 60)`. This means:
- The config is written to the mitmdump process's **stdout** (verified via a live 12.2.3 spike, `2>&1`-merged capture; the earlier "stderr/INFO" note was wrong). The supervisor captures the child's combined stdout/stderr to `$PPP_DATA/proxy.log` and parses from there.
- The opening fence line carries a `[timestamp]` prefix; the **closing fence is a bare line of exactly 60 hyphens** (`-` × 60), **not** `---` (`od -c`-verified). The capture parser (`internal/proxy/capture.go`) should match the 60-hyphen fence and tolerate the timestamp prefix on the opener.
- **Blocks are NOT emitted in `--mode` flag order — emission order is non-deterministic.** The supervisor MUST correlate each block to its port by reading the `Endpoint = host:<port>` line **inside** the block, never by flag order. (This corrects the earlier "correlate by flag order" guidance.)

#### Cannot manually add peers to a single WG instance without forking

The Python `WireGuardServerInstance.start_udp_based_server()` passes `[self.pubkey]` — a single-element list — to the Rust `start_wireguard_server()`. There is no CLI flag or config to add additional peer public keys to one instance. **The documented and supported approach is multiple `--mode wireguard` flags**, each creating a separate server instance with distinct keys. This is what we do.

### 3.2 Podman Machine

Verified against `docs.podman.io/en/latest/markdown/podman-machine.1.html`, `podman-machine-init.1.html`, and the `containers/gvisor-tap-vsock` README.

| Property | Value | Source |
|---|---|---|
| macOS providers | `libkrun` (default), `applehv` (via vfkit/Virtualization.framework) | `podman-machine.1.html` provider table |
| Windows providers | `wsl` (default), `hyperv` (optional) | same |
| Linux providers | `qemu` (default) | same |
| Guest OS | Fedora CoreOS (custom image at `quay.io/podman/machine-os`); WSL uses a custom Fedora image | `podman-machine-init.1.html` |
| Isolation | **Real VM with separate Linux kernel** — "podman machine init initializes a new Linux virtual machine where containers are run" | `podman-machine.1.html` |
| macOS hypervisor | Both `libkrun` and `applehv` use Apple HVF (Hypervisor.framework) — same kernel isolation boundary as Lima's `vz` | libkrun README, vfkit README |
| `--import-native-ca` | **Real flag** — imports host trusted CAs into guest trust store. Also available via `podman machine set`. Default `false`. | `podman-machine-init.1.html`, `cmd/podman/machine/init.go` |
| `--user-mode-networking` | Forces gvproxy to relay traffic through the user's active Windows session (bypasses VPN routing issues). Only meaningful on WSL provider. | `podman-machine-init.1.html` |
| Guest customization | `--image` flag exists but restricted to Podman-provided bootable images (FCOS-shaped). Cannot drop in arbitrary distro. | `podman-machine-init.1.html` |
| gvproxy networking | gvisor-tap-vsock — userspace, "uses regular syscalls to connect to external endpoints," obeys host routing table | gvisor-tap-vsock README |
| `podman machine ssh` | SSH into the machine for exec commands | `podman-machine.1.html` |
| `podman machine list` | Lists all machines — used by `ppp ls` | same |

**Note:** Podman Machine does **not** support `vz` (Apple Virtualization.framework directly) — that's a Lima concept. But `libkrun` and `applehv` both use HVF under the hood, so the hypervisor boundary is equivalent. QEMU is not available as a macOS provider for Podman Machine (only `libkrun` and `applehv`).

**WSL2 caveat:** WSL2 uses Microsoft's custom Linux kernel — a real separate kernel from Windows, but **shared across all WSL distros** on the host. This is weaker per-sandbox isolation than macOS (where each Podman Machine gets its own kernel). Documented limitation.

### 3.3 Platform matrix for the clone

| Host | Podman Machine provider | Isolation | WG client in guest | Status |
|---|---|---|---|---|
| macOS (Apple Silicon) | `libkrun` (default) | Full (separate kernel via HVF) | `wg-quick` in FCOS guest | **Primary** |
| macOS (Intel) | `libkrun` or `applehv` | Full | same | **Primary** |
| Windows | `wsl` (default) | Partial (WSL2 kernel shared across distros) | `wg-quick` (if WSL kernel has module) or `wireguard-go` fallback | **Primary** |
| Linux | `qemu` (default) | Full (KVM) | `wg-quick` | Best-effort |

### 3.4 Comparison: Lima vs Podman Machine (why we chose Podman)

The decision was driven by the Q4 answer: **Mac and Windows are primary**.

| Factor | Lima | Podman Machine |
|---|---|---|
| macOS support | First-class (`vz`, `qemu`, `krunkit`) | First-class (`libkrun`, `applehv`) |
| Windows support | **"Untested"** in install docs; WSL2 driver experimental, requires `tar` rootfs, omits many options | **First-class** (`wsl` is default provider, production-supported) |
| macOS isolation | Separate kernel via HVF | Separate kernel via HVF (same boundary) |
| Declarative provisioning | `provision:` block in lima.yaml (runs every boot via cloud-init) | `podman machine init` flags + SSH post-start scripts (less declarative) |
| CA injection | Manual (`curl http://mitm.it/cert/pem` in provision script) | **`--import-native-ca` flag** (imports host trusted CAs into guest) |
| Prior art for mitmproxy | **None** (Firecracker/Lima + mitmproxy = zero results) | **4+ open-source projects** (pi-container, opencli-container, HeartGarden, leash) |
| Guest OS | Customizable (Ubuntu, Fedora, Alpine, etc.) | Fedora CoreOS only (not easily customizable) |
| Per-sandbox model | Natural (one `limactl` instance per sandbox) | Supported (`podman machine init <name>`) |

Lima is more customizable and has better declarative provisioning, but Podman Machine's first-class Windows support and the abundance of prior art for Podman + mitmproxy made it the stronger foundation for a Mac+Windows-primary project.

---

## 4. Architecture

### 4.1 Topology

```
Host (macOS or Windows)
├── ONE mitmdump process (the "daemon")
│   ├── --mode wireguard:$PPP_DATA/sandboxes/ppp-red-bird/wg.conf@51820
│   ├── --mode wireguard:$PPP_DATA/sandboxes/ppp-blue-fox/wg.conf@51821
│   ├── --mode wireguard:$PPP_DATA/sandboxes/ppp-green-owl/wg.conf@51822
│   ├── ... (up to 80 pre-allocated WG server instances, ports 51820-51899)
│   ├── -s addon.py --set ppp_state_dir="$PPP_DATA"
│   └── shared flow log ($PPP_DATA/flows.jsonl) + shared addon (all sandboxes)
│
├── Podman Machine "ppp-red-bird"  → WG client (10.0.0.1, Endpoint:host:51820)
│     └── Fedora CoreOS guest
│           ├── wg0 interface (AllowedIPs=0.0.0.0/0 → all traffic via tunnel)
│           ├── mitmproxy CA trusted
│           ├── IPv6 disabled
│           └── opencode container (mounts workspace, runs agent)
│
├── Podman Machine "ppp-blue-fox"   → WG client (10.0.0.2, Endpoint:host:51821)
│     └── (same structure)
│
└── Podman Machine "ppp-green-owl"  → WG client (10.0.0.3, Endpoint:host:51822)
      └── (same structure)
```

### 4.2 Single mitmproxy, multiple WG instances

One `mitmdump` process starts with up to 80 `--mode wireguard` flags, one per pre-allocated port (51820–51899). Each WG instance has its own keys file at `$PPP_DATA/wg/keys-<port>.conf`. When a sandbox starts, it claims the next free port; when it stops, the port is freed. Unused WG instances sit idle (no connected client = no traffic = negligible overhead).

The addon reads the flow's proxy-mode listen port (`flow.client_conn.proxy_mode.listen_port()`, e.g. `51821`; **not** `flow.client.sockname` on 12.2.3) to identify which sandbox a flow belongs to, then applies that sandbox's policy and scoped secrets. This is unspoofable — each port has its own keypair, so the agent inside the VM cannot move its traffic to another sandbox's WG instance without that instance's server private key. (The inner tunnel IP from `flow.client.address` is logged for readability but is *not* the trust anchor; see §3.1.)

**Port pool lifecycle:**
```
ppp daemon start:
  ├─ ensure 80 keys files exist in $PPP_DATA/wg/ (leave missing paths absent; mitmdump generates them — never pre-create empty files)
  ├─ build mitmdump command with 80 --mode wireguard flags
  ├─ spawn mitmdump as child process
  ├─ capture the mitmdump stdout (to proxy.log): parse client config blocks, each closed by a line of 60 hyphens ("-" × 60), correlate by the Endpoint port inside each block
  ├─ store client configs in $PPP_DATA/wg/client-confs.json (indexed by port)
  └─ write PID to $PPP_DATA/proxy.pid

ppp run opencode ./myrepo:
  ├─ allocate next free port (e.g., 51820) + inner IP (10.0.0.1)
  ├─ take the pre-generated client config for port 51820 from client-confs.json
  ├─ rewrite Address = 10.0.0.1/32 (already correct for first sandbox)
  ├─ rewrite Endpoint = 192.168.127.254:51820  (gvproxy host alias — NOT the host LAN IP)
  ├─ write wg0.conf into $PPP_DATA/sandboxes/<name>/wg0.conf
  ├─ podman machine init <name> --import-native-ca + other flags
  ├─ podman machine start <name>
  ├─ podman machine ssh <name> -- provision.sh (installs WG, starts wg-quick, disables IPv6, fetches CA)
  ├─ podman machine ssh <name> -- podman run -v <workspace>:/workspace ... opencode
  └─ stream stdout/stderr to user terminal
```

---

## 5. Sub-Routines

### 5.1 SandboxVM (one Podman Machine per sandbox)

Dependencies: `podman` CLI.

> **Invariant — strict 1:1 sandbox ↔ Podman Machine.** Every sandbox owns exactly one dedicated Podman Machine VM, and no Podman Machine is ever shared between sandboxes. The machine name is derived from (and stored with) the sandbox: `machine_name` in `sandbox.json` maps 1:1 to `<name>`. This is the isolation boundary of the whole product — sharing a VM would let two sandboxes share a kernel, filesystem, container runtime, and WireGuard interface, collapsing both the security boundary and the per-sandbox network-policy/secret-scoping model. Consequences:
> - `ppp run`/`ppp create` always `podman machine init <name>` a **new, uniquely-named** machine; they never attach an agent to an existing sandbox's machine. (`ppp run --name <existing>` reattaches to *that sandbox's own* machine, not a shared one.)
> - `ppp rm <name>` always `podman machine rm <name>` — destroying one sandbox never affects another's VM.
> - `ppp` does **not** reuse Podman's implicit default machine (`podman-machine-default`); every machine it manages is `ppp`-named and `ppp`-owned. Names should be namespaced (e.g. prefixed `ppp-…`) so `ppp` never touches machines a user created outside `ppp`.
> - The agent container still runs *inside* its sandbox's VM (§5.7) — two-layer isolation — but that container is likewise not shared across sandboxes because the VM hosting it is not shared.

- `podman machine init <name> --import-native-ca` — creates the VM, imports host CAs. `--import-native-ca` is a real flag (verified against `podman` v6.0.1), also settable via `podman machine set`.
- Provider selection: `libkrun` on macOS (default — verified: `podman machine info` reports `vmtype: libkrun`), `wsl` on Windows (default), `qemu` on Linux. No `--provider` needed — Podman auto-detects (the flag exists as `--provider` if an override is ever needed).
- **Unit translation (important):** `ppp`'s `sbx`-style flags accept binary-unit strings, but the underlying `podman machine init` flags take plain integers: `--cpus uint`, `-m/--memory uint` (in **MiB**, podman default 2048), `--disk-size uint` (in **GiB**, podman default 100). `ppp` parses its own `--cpus` / `-m`/`--memory` (binary units, default 50% host RAM capped at 32 GiB per §6.1) / `--memory`/`--disk-size` and converts to podman's integer MiB/GiB before shelling out. It does **not** pass unit-suffixed strings (e.g. `8g`) to podman.
- `podman machine start <name>` — boots the VM.
- `podman machine stop <name>` — stops the VM (state preserved).
- `podman machine rm <name>` — destroys the VM and its disk.
- `podman machine ssh <name> -- <command>` — exec inside the VM.
- `podman machine list --format json` — query running state.

### 5.2 Prov (idempotent provision script)

Embedded `assets/provision.sh`, executed via `podman machine ssh <name> -- bash /tmp/provision.sh`. The script is copied into the VM via **`podman machine cp <local> <name>:/tmp/provision.sh`** (a real subcommand, verified against `podman` v6.0.1). Runs **every boot** (we re-run it on `ppp run --name <existing>` if the WG interface isn't up). Must be idempotent.

> **Provisioning path decision (wayfinder #6):** v1 uses `ssh -- provision.sh` (copied via `podman machine cp`), **not** `podman machine init --playbook`. Reasons: no Ansible dependency in the guest, easier to debug (plain shell + `machine.log`), and it runs identically on re-attach. `--playbook` may be revisited later if provisioning grows.

Steps (verified against a live `machine-os:6.0` / FCOS 44 spike, wayfinder #5):
1. **Ensure WireGuard is available (no install, no reboot on macOS).** `wireguard-tools` is already present in the Podman `machine-os` base image and the kernel `wireguard` module loads, so provisioning does **not** run `rpm-ostree install` on the primary path (this retires spec Risk #2 for macOS). Just ensure the module is loaded (`modprobe wireguard`). Keep `rpm-ostree install --apply-live wireguard-tools` **only** as a drift safety-net if a future image ever lacks the package.
2. Copy `wg0.conf` into `/etc/wireguard/wg0.conf` + `chmod 600`. The written config sets **`Table = off`** in `[Interface]` and **omits the `DNS =` line** (see step 3 and the DNS note).
3. **Routing — `Table = off` + manual routes (prevents a routing loop; wayfinder #4).** With `AllowedIPs = 0.0.0.0/0`, `wg-quick` would otherwise route *all* traffic — including the encrypted WireGuard datagrams to the host `Endpoint` — into the tunnel, which loops. Setting `Table = off` stops `wg-quick` from installing its own `fwmark`/`suppress_prefixlength` default-routing (verified: `add_default()` in wg-quick, the sole source of those rules, is skipped when `Table=off`). We then manage routes explicitly:
   - Determine the endpoint IP from `wg0.conf`'s `Endpoint =` line and the guest default gateway (`ip route show default`; on libkrun/gvproxy this is `192.168.127.1` — derive it, fall back to that literal).
   - **Before** bringing up wg0: `ip route replace <endpoint-ip>/32 via <gvproxy-gateway>` (off-tunnel exception, longest-prefix wins).
   - Bring up: `wg-quick up wg0`.
   - **After** up: `ip route replace default dev wg0` (send everything else through the tunnel).
4. Wait for tunnel to come up: `until wg show wg0; do sleep 1; done`.
5. **Fetch mitmproxy CA (must run AFTER the tunnel is up; wayfinder #7):** `curl -s http://mitm.it/cert/pem -o /etc/pki/ca-trust/source/anchors/mitmproxy.crt && update-ca-trust`. `mitm.it` is a magic hostname intercepted by the proxy, so this only works once wg0 is up and the off-tunnel endpoint route is in place. This is belt-and-suspenders on top of host-side `--import-native-ca` (see §6.9 / wayfinder #7).
6. Disable IPv6: `sysctl -w net.ipv6.conf.all.disable_ipv6=1` and `...default.disable_ipv6=1` (persisted in `/etc/sysctl.d/99-ppp.conf`). Matches mitmproxy's IPv4-only `AllowedIPs`.
7. Pull opencode container image: `podman pull <our-registry/opencode:latest>`.
8. Write `/var/lib/ppp/.provisioned` marker (gates step 7 on subsequent boots).

Idempotency gate: only step 7 (`podman pull`) is behind `if [ ! -f /var/lib/ppp/.provisioned ]`. Steps 1–6 run every boot (all inherently idempotent: module load is a no-op if loaded, `ip route replace` is declarative, the CA fetch overwrites, sysctl is declarative). Note vs. earlier drafts: there is no longer a one-shot package-install step to gate, because WireGuard ships in the base image.

**DNS note (decided — ADR-0005):** the written `wg0.conf` **omits** the `DNS = 10.0.0.53` line. Keeping it makes `wg-quick` invoke the fragile `resolvconf` path, whose failure aborts the whole `up` on FCOS. The guest instead uses gvproxy's resolver (`192.168.127.1`). Egress **policy enforcement is unchanged** — the addon still intercepts and policies every connection regardless of which resolver returned the address; only DNS *lookups* are not visible in the flow log. See `docs/decisions/0005-guest-dns-off-tunnel-via-gvproxy.md`.

### 5.3 ProxySup (single mitmdump, multi-WG)

Dependencies: Python 3.9+, `mitmdump` on PATH (bundled or pip-installed on first run), embedded `assets/addon.py`.

- **Port pool:** pre-allocate ports 51820–51899 (80 sandboxes max). Generate one WG keys file per port at first daemon start. **Do not pre-create empty files** — mitmdump only writes a keys file when the path does *not* exist, and an empty (or otherwise non-JSON) file makes it raise `ValueError: Invalid configuration file` (`mode_servers.py:352-369`). Two valid approaches: (a) let mitmdump create each missing file on start (leave the path absent), or (b) have `ppp` write a valid `{"server_key": ..., "client_key": ...}` JSON file itself (keys generated via the same `mitmproxy_rs.wireguard.genkey()` scheme). v1 uses approach (a): pass paths that do not yet exist and let mitmdump populate them on first boot.
- **Start:** `mitmdump --mode wireguard:$PPP_DATA/wg/keys-51820.conf@51820 --mode wireguard:$PPP_DATA/wg/keys-51821.conf@51821 ... -s <embedded_addon.py> --set ppp_state_dir="$PPP_DATA"`
- **Capture client configs:** each WG instance writes its client config once at startup to **stdout**, the opening fence carrying a `[timestamp]` prefix and the closing fence a bare line of **exactly 60 hyphens** (`-` × 60) (verified via live 12.2.3 spike; `mode_servers.py` logs `"-"*60 + conf + "-"*60`). Read `$PPP_DATA/proxy.log` (the captured child stdout/stderr), match the 60-hyphen fences, parse each `[Interface]`/`[Peer]` block, and **correlate each block to a port by its `Endpoint = host:<port>` line — NOT by `--mode` flag order, which is non-deterministic.** Store indexed by port in `$PPP_DATA/wg/client-confs.json`.
- **Rewrite client config per sandbox:** when a sandbox claims port N, take the client config for port N, rewrite `Address = 10.0.0.1/32` → `Address = 10.0.0.<sandbox-ip-octet>/32`, rewrite `Endpoint = ...` → `Endpoint = 192.168.127.254:<port>` (the gvproxy host alias reachable from the guest — verified: the host LAN IP silently drops WG handshakes). Write to `$PPP_DATA/sandboxes/<name>/wg0.conf`.
- **Lifecycle:** the daemon process starts once (via `ppp daemon start` or lazily on first `ppp run`) and stays running. `ppp daemon stop` kills it. `ppp daemon status` checks `$PPP_DATA/proxy.pid`.
- **Log:** `$PPP_DATA/proxy.log` (mitmdump stdout/stderr).
- **Upstream (proxy→server) TLS verification (ADR-0006):** mitmproxy verifies real-server certs with OpenSSL 3, which does not read the macOS Keychain and strict-rejects a non-conformant interception root (e.g. Zscaler's, whose BasicConstraints is not marked critical). ppp therefore passes mitmproxy a bundle = the host OS trust store minus OpenSSL-illegal CA certs (`internal/catrust`, written to `$PPP_DATA/wg/upstream-ca.pem`; `PPP_UPSTREAM_CA` overrides), and the addon's `tls_start_server` hook installs a verify **callback** that tolerates only the "no usable trust anchor" errors and only when the presented chain is cryptographically authorized by the host trust store. Verification is never disabled — expiry/hostname/self-signed/untrusted-root still fail. Works unchanged on normal networks; self-heals across interception-cert rotation (no probe, no `ppp setup`).

### 5.4 Addon (mitmproxy Python addon)

Embedded `assets/addon.py`. Loaded by the single mitmdump process. Handles flows from **all** WG instances.

- **Sandbox identification:** `flow.client_conn.proxy_mode.listen_port()` gives the **listen port** of the receiving WG instance (e.g., `51821`) — verified on mitmproxy 12.2.3; `flow.client.sockname` is the inner destination on that version, so do NOT use it for identity. The addon maintains an in-memory map `{port → sandbox_name}` loaded from `$PPP_DATA/port-registry.json` at startup and refreshed on SIGHUP. The inner IP (`flow.client.address`) is recorded in flow logs but is not used for identity (spoofable by a sudo agent; see §3.1).
- **Policy enforcement (`request` hook):** look up sandbox_name from the flow's proxy-mode listen port. Load per-sandbox policy from `$PPP_DATA/sandboxes/<name>/policy.yaml` + global policy from `$PPP_CONFIG/policies.yaml`. Evaluate rules (deny wins, `**` = block-all, glob/regex host match, CIDR IP match). Deny → HTTP 403 (or 444 to kill connection). Log to `$PPP_DATA/flows.jsonl`.
- **Secret injection (`request` hook, post-policy):** if the request is to a known service host (api.anthropic.com, api.openai.com, github.com, etc.), query the Go parent for the secret via UDS, inject `Authorization` / `x-api-key` / `x-github-token` header, strip any client-supplied key. For custom secrets (placeholder → real value), regex-substitute in outbound headers.
- **DNS-rebinding defense:** reject allowlisted hostnames that resolve to private/loopback/metadata IPs (incl. `169.254.169.254`) — from opencli-container.
- **Exfil-size gating:** 413 on outbound payloads > 1MB, 413 on inbound > 10MB — from opencli-container.
- **Response redaction:** scrub the real key from response headers/bodies before logging — from opencli-container.
- **Flow export:** each completed flow written as one JSON line to `$PPP_DATA/flows.jsonl` with `{sandbox, timestamp, host, method, status, decision, matched_rule, bytes_out, bytes_in}`.
- **Secret cache:** secrets cached in memory with 60s TTL, refreshed on SIGHUP. The UDS query to the Go parent is only made on cache miss.
- **Config refresh:** SIGHUP to the mitmdump process reloads policies + secret cache + port registry without restart.

### 5.5 Policy (rules engine)

Shared between the CLI (`internal/policy/`, used by `ppp policy allow/deny/check/ls/inspect/rm/reset`) and the addon (which loads the same YAML at runtime).

```yaml
# $PPP_CONFIG/policies.yaml (global) or $PPP_DATA/sandboxes/<name>/policy.yaml (per-sandbox)
rules:
  - id: <uuid>
    decision: allow | deny
    type: network
    resource: "api.anthropic.com" | "*.example.com" | "10.0.0.0/8" | "host:port" | "**"
    created_at: <iso8601>
    source: local | kit   # org is stubbed
```

Matching precedence: **deny > allow > default**. Deny wins. `**` matches any host. Wildcard `*.example.com` matches subdomains. Port suffix `:443` is optional. Host matching: glob (`*`/`?`) + regex (auto-detected by presence of regex metacharacters) — from pi-container's `allowlist.py` pattern. IP matching: single IPs + CIDR (v4 + v6) — from pi-container. First matching rule wins.

Presets for `ppp policy init`:
- `allow-all`: default action `allow`, no rules.
- `balanced`: default action `block`, allow common dev hosts (pypi, npm, github, apt mirrors, common LLM APIs — same defaults as pi-container's seeded allowlist).
- `deny-all`: default action `block`, no rules.

### 5.6 Secret (keychain + UDS RPC)

- **Storage:** `go-keyring` (macOS Keychain / Windows Credential Manager / Linux Secret Service) with an `age`-encrypted fallback file at `$PPP_DATA/secrets.age` when no keychain backend is available.
- **Service secrets:** stored under keychain service `ppp.anthropic`, `ppp.openai`, `ppp.github`, etc. The addon queries the Go parent via UDS for each request (on cache miss). The Go parent reads from the keychain. The secret never touches the sandbox filesystem or the mitmproxy Python process memory for longer than the IPC response.
- **Per-sandbox scoping (USAi charge-back support):** a sandbox-scoped key always takes precedence over the global key for the same service. Stored under `ppp.<sandbox>.<service>` (e.g., `ppp.opencode-my-app.usai`). This enables USAi's anticipated billing-code model: each sandbox can carry its own USAi API key associated with a specific billing code, so usage is attributed to the right cost center. The agent never sees either key — both are injected by the MITM addon on outbound requests. Set via `ppp secret set usai --host api.gsa.usai.gov` (global) or `ppp secret set usai --host api.gsa.usai.gov --sandbox <name>` (per-sandbox).
- **Custom secrets:** `{placeholder, value, host[]}` tuples stored in keychain under `ppp.custom.<name>`. The addon regex-substitutes the placeholder in outbound headers when `flow.request.host` matches one of `host[]`.
- **Registry secrets:** (`ghcr.io`, etc.) used for `podman pull` of private agent images. Not proxied at runtime. Stored in keychain under `ppp.registry.ghcr.io`. Injected into `podman machine`'s auth config.
- **IPC:** UDS socket at `$PPP_DATA/secret.sock` (mode `0600`, created inside a `0700` parent dir so the socket is never briefly world-reachable). Newline-delimited JSON, one request/response per connection. The Go parent listens; the Python addon connects and sends `{"service": "anthropic", "sandbox": "ppp-red-bird", "host": "api.anthropic.com"}`. The parent responds with the **header name and value** to inject, not a bare key, so the addon needn't hardcode per-provider header logic: `{"ok": true, "header": "x-api-key", "value": "sk-ant-..."}`. A miss is `{"ok": false, "reason": "no-secret"}`; a locked/undecryptable store is `{"ok": false, "reason": "locked"}`; a custom-secret lookup (empty `service`) returns `{"ok": true, "substitutions": [{"placeholder": "...", "value": "..."}]}`. If the secret is sandbox-scoped, the parent checks `ppp.<sandbox>.<service>` first, then falls back to `ppp.<service>` (global).

### 5.7 Agent (agent registry, opencode only at v1)

Built-in registry maps agent name → `{default_image, install_command, env, default_template_tag}`.

v1 ships exactly one entry:
```yaml
opencode:
  default_image: ghcr.io/ppp/opencode:latest
  default_template_tag: opencode-default
  install: |  # runs inside the container, not the VM
    npm install -g @opencode-ai/opencode
  env:
    OPENCODE_SANDBOX: "1"
```

`--template`/`-t` overrides the image URL. The image is built from a Containerfile that layers Node.js, git, openssh, ca-certificates, and a sudoless `ppp` user onto Ubuntu 24.04. Published to GHCR via CI.

The opencode agent runs as a **container inside the Podman Machine VM** (`podman run -v <workspace>:/workspace ... opencode`), not directly in the VM. This means the agent shares the VM's kernel (namespaces) but the VM has its own kernel separate from the host — two-layer isolation matching Docker's `sbx`'s model.

### 5.8 State (XDG Base Directory paths)

`ppp` follows the [XDG Base Directory](https://specifications.freedesktop.org/basedir-spec/latest/) convention with two root directories. All paths below use these environment-resolved defaults; no `~/.ppp/` hidden directory exists.

**Config root** — `$PPP_CONFIG` (default: `$XDG_CONFIG_HOME/ppp` → `~/.config/ppp`):
- `config.yaml` — CLI config (default policy init, log level, port pool range)
- `policies.yaml` — global network policies

**Data root** — `$PPP_DATA` (default: `$XDG_DATA_HOME/ppp` → `~/.local/share/ppp`):
- `sandboxes/<name>/` — per-sandbox state:
  - `wg0.conf` (rewritten from mitmdump's client config)
  - `policy.yaml` (per-sandbox policies)
  - `sandbox.json` (`{name, agent, workspace, status, created_at, cpus, memory, port, inner_ip, kit_refs, template_tag, machine_name}`)
  - `machine.log` (provision script output)
  - `addon-config.yaml` (secret rewrite rules for the mitmproxy addon)
- `port-registry.json` — UDP port ↔ sandbox mapping
- `proxy.pid` — mitmdump PID
- `proxy.log` — mitmdump stdout/stderr
- `flows.jsonl` — unified flow log (all sandboxes)
- `wg/` — WireGuard keys (`keys-51820.conf` … `keys-51899.conf`) and generated client configs (`client-confs.json`)
- `secrets.age` — age-encrypted fallback secret store (when no OS keychain)
- `secret.sock` — UDS for addon ↔ Go parent IPC
- `state.lock` — flock for concurrent CLI operations
- `kits/` — cached kit artifacts

**Cache root** — `$PPP_CACHE` (default: `$XDG_CACHE_HOME/ppp` → `~/.cache/ppp`):
- `templates/` — saved template images

Throughout this document, `$PPP_DATA` is used as the state base. Replace it with the actual resolved path at runtime (default `~/.local/share/ppp`).

---

## 6. Subcommand Surface

18 top-level subcommands (all of Docker's `sbx` except `login` and `logout`): `completion`, `cp`, `create`, `daemon`, `diagnose`, `exec`, `kit`, `ls`, `policy`, `ports`, `reset`, `rm`, `run`, `secret`, `setup`, `stop`, `template`, `tui`, `version`. The binary is named `ppp`; the subcommand surface mirrors Docker's `sbx`.

### 6.1 `ppp run [flags] AGENT PATH [PATH...] [-- AGENT_ARGS...]`

1. Validate `AGENT` is `opencode` (v1 — error otherwise with "agent not supported in this version").
2. If `--name` is provided and a sandbox with that name exists, attach: start the Podman Machine if stopped, ensure the daemon is running, re-run provision if WG is down, exec the agent.
3. Otherwise:
   a. Allocate a sandbox name (`--name` or `ppp-<adjective>-<noun>`).
   b. Acquire `$PPP_DATA/state.lock`; allocate next free port from port-registry + inner IP (`10.0.0.<N>`, N = port - 51819).
   c. Ensure the daemon is running (start if not — `ppp daemon start` under the hood).
   d. Fetch the pre-generated client config for this port from `$PPP_DATA/wg/client-confs.json`. Rewrite `Address` to `10.0.0.<N>/32` and `Endpoint` to `192.168.127.254:<port>` (gvproxy host alias). Write `wg0.conf`.
   e. `podman machine init <name> --import-native-ca --cpus <N> --memory <MiB> --disk-size <GiB>` (memory in MiB, disk in GiB — integers; see §5.1 unit translation). MAY use `--now` to fold in the start step.
   f. `podman machine start <name>`.
   g. Copy provision script into VM + execute: `podman machine ssh <name> -- bash /tmp/provision.sh`.
   h. If `--clone`, `git clone` the workspace's remote into the VM and add a `sandbox-<name>` remote on the host pointing back.
   i. If `--kit`, apply kit YAML before starting the agent.
   j. `podman machine ssh <name> -- podman run -i -t -v <workspace>:/workspace --workdir /workspace <image> opencode <AGENT_ARGS...>`.
4. Stream the agent's stdout/stderr to the host terminal. On Ctrl-C, stop the agent but keep the VM running.

Flags: `--clone`, `--cpus` (0 = auto-all), `--kit` (repeatable), `-m`/`--memory` (binary units, default 50% of host RAM, max 32 GiB), `--name`, `-t`/`--template`. Inherited: `--debug`/`-D`.

### 6.2 `ppp create [flags] AGENT PATH [PATH...]`

Like `ppp run` steps 3a–3i but does not launch the agent. Prints the sandbox name (`-q` for name only).

### 6.2.1 `ppp create opencode PATH [PATH...]`

Hardcoded shortcut for `ppp create --template <opencode_default_image> opencode PATH...`.

### 6.3 `ppp ls`

Reads `$PPP_DATA/sandboxes/*/sandbox.json` + `podman machine list --format json`. Prints: NAME | AGENT | STATUS | PORTS | WORKSPACE. `--json` for machine output, `-q` for names only.

### 6.4 `ppp stop SANDBOX [SANDBOX...]`

- `podman machine stop <name>`.
- Update `sandbox.json` status to `stopped`.
- The WG port stays allocated (so `ppp run --name` can reattach).

### 6.5 `ppp rm [SANDBOX...] [flags]`

- `--all`: enumerate all sandboxes.
- For each: stop if running, `podman machine rm <name>`, remove `$PPP_DATA/sandboxes/<name>/`, free the port in port-registry.
- `-f`/`--force`: skip confirmation. Even remove if machine is unresponsive.

### 6.6 `ppp exec [flags] SANDBOX COMMAND [ARG...]`

`podman machine ssh <name> -- <command> <args...>` (or `podman exec` into the agent container if it's running). Flags mirror `docker exec`: `-d`/`--detach`, `-e`/`--env`, `--env-file`, `-i`/`--interactive`, `--privileged`, `-t`/`--tty`, `-u`/`--user`, `-w`/`--workdir`.

### 6.7 `ppp cp [flags] SRC DST`

Either SRC or DST is `SANDBOX:PATH`. Translates to `podman machine ssh` + `podman cp` or rsync. `-L`/`--follow-link` follows symlinks.

### 6.8 `ppp ports SANDBOX [flags]`

- No flags: list published ports (from `sandbox.json` + `podman machine inspect`). `--json`.
- `--publish [HOST_IP:]HOST_PORT:]SANDBOX_PORT[/PROTOCOL]` (repeatable): adds port forwarding rules to the Podman Machine + agent container, restarts the agent container.
- `--unpublish`: removes rules, restarts.
- Protocols: `tcp/tcp4/tcp6/udp/udp4/udp6`. Default `tcp`.

### 6.9 `ppp setup`

Detects host config: `podman` installed? `mitmdump` installed? Python ≥ 3.9? Keychain available? Agent image present? Walks through fixes via a Bubbletea TUI. Installs the mitmproxy CA into the host keychain (so `podman machine init --import-native-ca` imports it into the guest). Optionally imports detected agent secrets from host env vars (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GH_TOKEN`, …).

### 6.10 `ppp reset [flags]`

Destructive: stop all sandboxes, `podman machine rm` them, remove `$PPP_DATA/sandboxes/`, `$PPP_CACHE/templates/`, `$PPP_DATA/kits/`, `$PPP_CONFIG/policies.yaml`, port registry. Stop the daemon. `-f`/`--force` skips confirmation. `--preserve-secrets` keeps keychain entries + `secrets.age`.

### 6.11 `ppp diagnose [flags]`

Collects: host OS, `podman --version`, `mitmdump --version`, `python3 --version`, keychain availability, `podman machine list`, running sandboxes + their machine/proxy status, `$PPP_DATA/proxy.log` (last 100 lines), `$PPP_DATA/flows.jsonl` (last 100 entries), port registry. `-o json` / `-o github-issue`. `--upload` stubs.

### 6.12 `ppp tui`

Bubbletea dashboard: list of sandboxes (name, agent, status, ports, workspace, inner IP) with keybindings to start/stop/rm/exec/attach. Live-updates from `$PPP_DATA/sandboxes/*/sandbox.json` + `podman machine list`.

### 6.13 `ppp version`

Prints CLI version + `podman --version` + `mitmdump --version` + detected host platform + provider.

### 6.14 `ppp completion <shell>`

Cobra-autogenerated. `bash`/`fish`/`powershell`/`zsh`. `--no-descriptions`.

### 6.15 `ppp daemon ...`

Manages the single mitmdump process (all WG instances).

#### `ppp daemon start [flags]`
- If `$PPP_DATA/proxy.pid` exists and process is alive: no-op.
- Otherwise: ensure keys files exist for all ports in the pool (leave missing paths absent so mitmdump generates them; never pre-create empty files). Build the mitmdump command with all `--mode wireguard` flags. Spawn as child process. Read `$PPP_DATA/proxy.log` to capture the logged client configs (fenced by `-` × 60). Write PID. Write `$PPP_DATA/wg/client-confs.json`.
- `-d`/`--detach`: nohup the process.
- `--policy <allow-all|balanced|deny-all>`: run `ppp policy init <preset>` first if no global policy exists.

#### `ppp daemon stop`
- Kill the mitmdump process by PID. Clean up `proxy.pid`. Update port-registry (mark all ports as `free` but keep sandboxes' assigned ports so they can reattach).

#### `ppp daemon status [--json]`
- Is mitmdump alive? How many WG instances are listening? How many have active clients? Last log line? Per-sandbox: proxy active for this sandbox?

#### `ppp daemon log-level [target] [level]`
- `proxy`: sets `--set console_event_level=<level>` (requires mitmdump restart — or SIGHUP if supported).
- `general`: Go binary log level (in `$PPP_CONFIG/config.yaml`).
- `all`: both.

#### `ppp daemon log-level set <target> <level>`
- Persist setting in `$PPP_CONFIG/config.yaml` + apply (SIGHUP or restart).

### 6.16 `ppp kit ...` (experimental)

Parent group for kit artifacts (declarative YAML extensions).

#### `ppp kit add SANDBOX REFERENCE`
Validate REFERENCE (dir/zip/OCI/git), apply its `secrets`/`network`/`env`/`startup`/`files` sections: append to `policy.yaml`, append to `sandbox.json`'s `env`, copy `files` into the VM, run `startup` via `podman machine ssh`. Restart the agent container.

#### `ppp kit inspect REFERENCE [--json]`
Load and display kit metadata.

#### `ppp kit pack DIRECTORY [-o FILE]`
Validate kit dir (must contain `kit.yaml`), zip it.

#### `ppp kit pull REFERENCE [-o FILE]`
Pull a kit from an OCI registry to a local `.zip`.

#### `ppp kit push DIRECTORY REFERENCE`
Pack + push to OCI registry.

#### `ppp kit validate REFERENCE`
Schema-validate `kit.yaml`.

Kit schema (v1):
```yaml
apiVersion: ppp.kit/v1
kind: Kit
metadata:
  name: my-toolkit
  version: 0.1.0
spec:
  secrets:
    - service: anthropic
  network:
    allow:
      - "api.anthropic.com"
    deny:
      - "**"
  env:
    TOOL_CONFIG_PATH: /etc/tool/config.json
  startup:
    - command: ["sh", "-c", "echo kit active"]
  files:
    - path: /etc/tool/config.json
      content: <base64 or inline string>
```

### 6.17 `ppp policy ...`

Parent group. See Sub-Routine 5.5 for the rules engine.

#### `ppp policy init <allow-all|balanced|deny-all>`
Initialize `$PPP_CONFIG/policies.yaml` with the requested default. Idempotent.

#### `ppp policy allow network [--sandbox SANDBOX] RESOURCES`
Append `decision: allow` rules. `RESOURCES` is comma-separated; `**` = all.

#### `ppp policy deny network [--sandbox SANDBOX] RESOURCES`
Same but `decision: deny`.

#### `ppp policy check network [--sandbox SANDBOX] TARGET [--json] [--verbose]`
Evaluate the rule engine against `TARGET` without modifying state. Returns allow/deny + matched rule.

#### `ppp policy ls [SANDBOX] [--decision ...] [--include-inactive] [--json] [--source local|org|kit] [--type] [--wide]`
List active rules. `--source org` is stubbed.

#### `ppp policy inspect <policy-or-rule>`
Show full detail by ID or name.

#### `ppp policy log [SANDBOX] [--json] [--limit N] [-q] [--type all|network|filesystem]`
Reads `$PPP_DATA/flows.jsonl`, filtered by sandbox (if `SANDBOX` arg). Aggregates across sandboxes if no arg.

#### `ppp policy reset [-f]`
Remove all custom rules. SIGHUP the daemon to reload.

#### `ppp policy rm network [--sandbox SANDBOX] [--id X | --resource R]`
Remove rules by ID or resource match.

### 6.18 `ppp secret ...`

Parent group. See Sub-Routine 5.6 for storage.

#### `ppp secret set [-g | SANDBOX] [SERVICE]`
Interactive prompt for the secret value (or `--password-stdin`/`-t`/`--token`). `-g`/`--global` stores in `ppp.<service>` keychain entry; without `-g` stores in `ppp.<sandbox>.<service>`. `--registry` marks it as a registry secret. `--oauth` triggers an OAuth flow (OpenAI/global only — v1 stub). Services at v1: `anthropic`, `openai`, `github`, `google`, `groq`, `mistral`, `nebius`, `openrouter`, `xai`.

**Per-sandbox USAi keys (charge-back support):** a per-sandbox key always takes precedence over the global key for the same service. This supports USAi's anticipated billing-code model — each sandbox can carry its own USAi API key so usage is attributed to the right cost center:
```bash
# Global USAi key (default for all sandboxes)
ppp secret set usai --host api.gsa.usai.gov
# Per-sandbox USAi key (overrides global for this sandbox)
ppp secret set usai --host api.gsa.usai.gov --sandbox my-project-sandbox
```
The addon resolves per-sandbox first (`ppp.<sandbox>.usai`), then falls back to global (`ppp.usai`). The agent never sees either key — both are injected by the MITM addon on outbound requests.

#### `ppp secret set-custom [-g | sandbox] [--placeholder TOKEN] [--host HOST...] [--value VAL | -t TOKEN] [--env NAME]` (experimental)
Store a custom `placeholder → value` mapping scoped to `host[]`.

#### `ppp secret import [SERVICE] [--all] [--dry-run] [-f]`
Detect host env vars and import into the global keychain. `--dry-run` prints what would be imported. `-f` overwrites existing.

#### `ppp secret ls [SANDBOX] [-g] [--service S]`
List stored secrets (redacted). `--service` filters.

#### `ppp secret rm [-g | SANDBOX] [SERVICE] [-f] [--registry]`
Delete from keychain. `-f` skips confirmation.

### 6.19 `ppp template ...`

#### `ppp template save SANDBOX TAG [-o FILE]`
Snapshot the Podman Machine's disk image as a qcow2 with the tag name. Optionally export to a tar.

#### `ppp template load FILE`
Import a saved template into Podman's image store; register under `TAG` in `$PPP_CACHE/templates/`.

#### `ppp template ls [--json]`
List registered templates.

#### `ppp template rm TAG|ID`
Remove the template file and registry entry.

---

## 7. Repo Layout

```
ppp/
  cmd/ppp/main.go                      # cobra root, wire-up
  internal/cli/                        # one file per top-level command
    run.go create.go ls.go stop.go rm.go exec.go cp.go ports.go
    setup.go reset.go diagnose.go tui.go version.go
    daemon.go kit.go policy.go secret.go template.go
    completion.go                      # cobra auto-gen wrapping
  internal/podman/                     # podman machine wrapper, provider detection
    machine.go                         # init/start/stop/rm/ssh per-sandbox machine
    provider.go                        # libkrun on Mac, wsl on Windows, qemu on Linux
  internal/proxy/                      # single mitmdump process supervisor, port pool
    supervisor.go capture.go           # captures client configs from proxy.log ("-"×60 fences), rewrites Address/Endpoint
    portpool.go                        # port + inner IP allocation
  internal/policy/                     # rules engine (shared CLI + addon)
    rule.go matcher.go parse.go
  internal/secret/                     # keychain + age fallback + UDS RPC server
    keychain.go server.go
  internal/agent/                      # agent registry (opencode only at v1)
    registry.go opencode.go
  internal/sandbox/                    # state dir, lifecycle, resources
    state.go lifecycle.go
  internal/tui/                        # bubbletea dashboard
  assets/
    addon.py                            # embedded mitmproxy addon (policy + secret injection, flow export)
    provision.sh                        # embedded idempotent provision script (WG + CA + IPv6 disable)
    opencode.Containerfile              # our minimal opencode agent container image
  .goreleaser.yml                      # multi-platform builds + GHCR image push
  go.mod
  docs/explorations/ppp-spec.md        # this file
```

---

## 8. Data Flows

### 8.1 `ppp run opencode ./myrepo` — happy path

```
Host CLI                          Podman Machine VM              Single mitmdump (host)
─────────                         ────────────────              ─────────────────────
ppp run opencode ./myrepo
  ├─ acquire state.lock
  ├─ allocate port 51820 + inner IP 10.0.0.1
  ├─ ensure daemon running (start if needed: spawn mitmdump with 80 WG instances)
  │                                                                ├─ mitmdump starts, logs
  │                                                                │  80 client configs (stdout)
  │  ◄──────────────────────────────────────────────────────────────┤  (each fenced by "-" × 60)
  ├─ store client-confs.json
  ├─ take client config for port 51820
  ├─ rewrite Address = 10.0.0.1/32, Endpoint = 192.168.127.254:51820
  ├─ write wg0.conf → $PPP_DATA/sandboxes/ppp-red-bird/wg0.conf
  ├─ podman machine init ppp-red-bird --import-native-ca ...
  ├─ podman machine start ppp-red-bird
  │     ┌─ VM boots (Fedora CoreOS, separate kernel) ──►
  │     │
  │     │  copy provision.sh into VM + execute:
  │     │  ├─ modprobe wireguard (already in machine-os; no rpm-ostree install / reboot)
  │     │  ├─ cp wg0.conf → /etc/wireguard/wg0.conf  (Table=off, no DNS= line)
  │     │  ├─ ip route replace <endpoint-ip>/32 via 192.168.127.1  (off-tunnel: avoid WG loop)
  │     │  ├─ wg-quick up wg0 ; ip route replace default dev wg0
  │     │  ├─ curl http://mitm.it/cert/pem → /etc/pki/ca-trust/source/anchors/mitmproxy.crt
  │     │  ├─ update-ca-trust
  │     │  ├─ sysctl disable IPv6
  │     │  ├─ podman pull ghcr.io/ppp/opencode:latest  (one-shot, gated)
  │     │  └─ touch /var/lib/ppp/.provisioned
  │     │
  │     │  wg0 tunnel is up → all VM traffic routes to host:51820
  │     │                                          ──────────►
  │     │                                                        ├─ mitmdump WG server on :51820
  │     │                                                        │  decrypts WG, parses TCP/UDP
  │     │                                                        ├─ addon.request(flow):
  │     │                                                        │    proxy_mode.listen_port() = 51820
  │     │                                                        │    → sandbox = "ppp-red-bird"
  │     │                                                        │    → policy check (allow api.anthropic.com)
  │     │                                                        │    → secret: UDS query Go parent
  │     │                                                        │      for ppp.anthropic key
  │     │                                                        │    → set Authorization: Bearer <key>
  │     │                                                        │    → strip client-supplied key
  │     │                                                        ├─ forward to upstream
  │     │                                                        ├─ response decrypted with
  │     │                                                        │  mitmproxy CA (trusted in guest)
  │     │  ◄────────────────────────────────────────────────────┤
  │     │
  │     └─ podman run -v ./myrepo:/workspace --workdir /workspace opencode
  │        opencode makes HTTPS request to api.anthropic.com
  │        → routed via wg0 → tunnel → mitmproxy → injected + forwarded
  │        → response returns through same path
  │
  └─ stream stdout/stderr to user terminal
```

### 8.2 `ppp policy deny network --sandbox ppp-red-bird "**"`

```
ppp policy deny network --sandbox ppp-red-bird "**"
  ├─ append to $PPP_DATA/sandboxes/ppp-red-bird/policy.yaml:
  │     - decision: deny, type: network, resource: "**", id: <uuid>
  ├─ SIGHUP the mitmdump process (addon reloads policies on SIGHUP)
  └─ print "Rule added: deny ** for sandbox ppp-red-bird"
```

### 8.3 `ppp secret set anthropic`

```
ppp secret set anthropic
  ├─ prompt: "Enter Anthropic API key: " (no echo)
  ├─ keyring.Set(service="ppp.anthropic", user="default", value=<key>)
  │     (or age-encrypt to $PPP_DATA/secrets.age if no keychain backend)
  ├─ SIGHUP mitmdump (addon clears secret cache, will re-query on next request)
  └─ print "Secret stored: anthropic"
```

### 8.4 Multiple sandboxes — single mitmdump, distinct WG instances (identified by listen port)

```
mitmdump process (single):
  WG server :51820 ← ppp-red-bird  (10.0.0.1)  → policy.allow("api.anthropic.com") → inject anthropic key
  WG server :51821 ← ppp-blue-fox   (10.0.0.2)  → policy.deny("**")                  → all traffic 403
  WG server :51822 ← ppp-green-owl  (10.0.0.3)  → policy.allow("github.com")        → inject github token

addon sees (identity = receiving WG instance's listen port; inner IP logged but not trusted):
  proxy_mode.listen_port() = 51820 (addr 10.0.0.1)  → sandbox = "ppp-red-bird"   → apply red-bird policy + secrets
  proxy_mode.listen_port() = 51821 (addr 10.0.0.2)  → sandbox = "ppp-blue-fox"    → apply blue-fox policy + secrets
  proxy_mode.listen_port() = 51822 (addr 10.0.0.3)  → sandbox = "ppp-green-owl"   → apply green-owl policy + secrets

All flows logged to $PPP_DATA/flows.jsonl with sandbox identified (by port).
```

---

## 9. Daemon Lifecycle

The daemon (single mitmdump process) is the core of the proxy layer. It manages all WG server instances.

### 9.1 Start

```
ppp daemon start [flags]
  ├─ check $PPP_DATA/proxy.pid → if alive, no-op
  ├─ ensure 80 keys files exist in $PPP_DATA/wg/ (keys-51820.conf ... keys-51899.conf)
  │     (leave missing paths absent so mitmdump generates them on start;
  │      NEVER pre-create empty files — mitmdump errors on non-JSON content)
  ├─ build mitmdump command:
  │     mitmdump \
  │       --mode wireguard:$PPP_DATA/wg/keys-51820.conf@51820 \
  │       --mode wireguard:$PPP_DATA/wg/keys-51821.conf@51821 \
  │       ... \
  │       --mode wireguard:$PPP_DATA/wg/keys-51899.conf@51899 \
  │       -s <embedded_addon.py> \
  │       --set ppp_state_dir="$PPP_DATA" \
  │       --set console_event_level=info
  ├─ spawn as child process, redirect stdout/stderr to $PPP_DATA/proxy.log
  ├─ wait for all WG instances to log their client configs (read proxy.log, match "-" × 60 fences)
  ├─ store client configs indexed by port in $PPP_DATA/wg/client-confs.json
  ├─ write PID to $PPP_DATA/proxy.pid
  └─ print "daemon started, 80 WireGuard servers listening on ports 51820-51899"
```

### 9.2 Stop

```
ppp daemon stop
  ├─ read $PPP_DATA/proxy.pid
  ├─ SIGTERM the process
  ├─ wait for graceful shutdown (mitmdump closes all WG servers)
  ├─ remove proxy.pid
  ├─ mark all ports in port-registry as "free" (but keep sandbox→port mapping so reattach works)
  └─ print "daemon stopped"
```

### 9.3 Status

```
ppp daemon status [--json]
  ├─ is $PPP_DATA/proxy.pid alive? (kill -0)
  ├─ how many WG instances are listening? (parse proxy.log or check UDP sockets)
  ├─ how many have active clients? (from port-registry: count "allocated" ports)
  ├─ last log line from proxy.log
  └─ per-sandbox: is the WG tunnel for this sandbox's port active?
      (check if the inner IP has recent flows in flows.jsonl)
```

### 9.4 Lazy start

If no daemon is running when `ppp run` or `ppp create` is called, the CLI automatically runs `ppp daemon start` first. This keeps the user experience simple — they don't need to manage the daemon manually unless they want to.

---

## 10. Cross-Cutting Concerns

### 10.1 Idempotency

- Provision script gates one-shot steps (package install, opencode install) behind `/var/lib/ppp/.provisioned`.
- Every-boot steps (start wg-quick, ensure CA cert, disable IPv6) run unconditionally but are idempotent.
- `podman machine init` is idempotent (fails gracefully if machine exists).

### 10.2 Concurrency

- One mitmdump process for all sandboxes — no shared mutable state in the proxy layer (policies are read-only from the addon's perspective; secrets are read lazily from keychain).
- `$PPP_DATA/state.lock` (flock) guards concurrent CLI operations (create/rm/stop/port-allocation).

### 10.3 Crash recovery

- **mitmdump dies:** `ppp daemon status` reports dead → `ppp daemon start` restarts it. All WG keys files are persisted on disk, so sandboxes' tunnels reconnect automatically (WireGuard's roaming behavior: the client re-handshakes when the server comes back).
- **Podman Machine dies:** `ppp run --name <name>` re-starts the machine and reattaches. The WG tunnel re-establishes.
- **Provision fails:** `ppp diagnose` points to `$PPP_DATA/sandboxes/<name>/machine.log` and `$PPP_DATA/proxy.log`; `ppp rm -f <name>` and start over.
- **WG tunnel won't establish:** Check `wg0.conf` `Endpoint` resolves from inside the VM; check the off-tunnel `/32` route to the endpoint is present (`ip route get <endpoint-ip>` should go via gvproxy `192.168.127.1`, *not* `wg0` — a missing exception route causes a handshake loop); check UDP port reachable from guest; check `ppp daemon status` shows the port as active.
- **TLS errors in agent:** CA cert not installed; re-run provision step or `ppp diagnose` (which checks CA trust).

### 10.4 Telemetry

- `ppp diagnose` aggregates: `podman --version`, `mitmdump --version`, `python3 --version`, `podman machine list`, `$PPP_DATA/proxy.log` (last 100 lines), `$PPP_DATA/flows.jsonl` (last 100 entries), port registry state, keychain availability.
- `--upload` stubs to a POST endpoint defined later.

---

## 11. Out-of-Scope / Future

- Remote org policy source (`--source org`).
- GPU passthrough.
- Multi-agent support beyond `opencode` (registry is extensible; v1.1 adds `claude`, `gemini`, etc.).
- The "Gordon" assistant persona.
- `sbx login` / `sbx logout` (intentionally excluded per the brief).
- IPv6 egress (mitmproxy WG mode excludes it; provision disables it in guest).
- Dynamic mitmdump server add/remove without restart (v1 pre-allocates a port pool; v2 could use the mitmproxy Python API to add WG servers at runtime).
- UDP application protocol decoding beyond DNS (v1 only handles HTTP/S + DNS; custom UDP addons are v2).

---

## 12. Prior Art Analysis

### 12.1 `mikkovihonen/pi-container` (Jul 2026, MIT)

Open-source, actively maintained. The closest thing to a working ppp-equivalent. Key architecture:
- **`--internal` no-gateway Docker/Podman network** — agent container single-homed on isolated network; proxy container dual-homed (`eth0` upstream + `eth1` isolated).
- **iptables REDIRECT** in the proxy container's entrypoint: `PREROUTING -i eth1 -p tcp --dport 80/443 -j REDIRECT --to-port 8080` (mitmproxy transparent mode), `--dport 53 -j REDIRECT --to-port 5353` (mitmproxy DNS mode). `FORWARD` policy `DROP` with explicit opt-ins for SSH/SMTP/git/NTP/custom ports.
- **Three reusable mitmproxy addons:**
  - `allowlist.py` (20KB) — glob + regex host matching, CIDR IP matching, allow/block modes, 403 or 444 (kill connection) on deny, dry_run mode.
  - `token_replacer.py` (46KB) — two-phase detection (all matchers scan original data, then modifications applied grouped by content type to prevent cross-rule interference). `${ENV:VAR}` resolution from proxy container's env. Strategies: static/hash/uuid. Operates on body.json (JSONPath), body.form, body.query, body.raw (regex), headers.
  - `flow_export.py` — per-client-IP JSONL flow log.
- **Per-workspace isolation** via flock refcount: concurrent runs from the same workspace share the proxy; torn down when last exits. State in `<project>/.pi-container/`.
- **No CLI subcommands, no policy/secret/template/kit/exec/cp/ports/TUI/daemon.** A single `run` command with config files. We'd reuse its internals and build the full CLI on top.

### 12.2 `albertdobmeyer/opencli-container` (MIT)

The secret-injection reference implementation. A `vault-proxy` mitmproxy sidecar substitutes placeholder strings with real API keys:
```python
if host == "api.anthropic.com" or host.endswith(".api.anthropic.com"):
    api_key = os.environ.get("ANTHROPIC_API_KEY", "")
    if api_key:
        flow.request.headers["x-api-key"] = api_key
        flow.request.headers["anthropic-version"] = ANTHROPIC_API_VERSION
```
Also includes: DNS-rebinding defense (rejects allowlisted hostnames resolving to private/loopback/metadata IPs including `169.254.169.254`), exfil-size gating (413 on >1MB outbound / >10MB inbound), response redaction (scrubs real key from headers/bodies before logging). Documents a "Docker Desktop sandbox plugin" alternative explicitly as weaker (key lives in container env). Has a `phase2-vm-isolation/` folder — treats VM isolation as the next tier up from containers.

### 12.3 `sniper-fly/HeartGarden`

Uses mitmproxy's built-in `--modify-headers` flag with `@<path>` file-value syntax for zero-addon secret injection:
```
--modify-headers "/~d api.search.brave.com & ~u /res/v1/.*/X-Subscription-Token/@/run/secrets/brave-search-api-key"
```
Secrets pulled from AWS SSM into podman secret store, mounted at `/run/secrets/<name>`. Documents limitations honestly: no hot-reload, header-only (query-param auth needs custom addon), values needing prefixes must be stored pre-prefixed.

### 12.4 `np6126/leash` (Jul 2026)

Podman Quadlet (systemd `.container`) deployment + audit log on a volume the agent VM can't reach. Three modes (enforce/audit/blocklist), hot-reloadable policy, fail-closed startup (empty rule set = reject everything until policy loads). Uses explicit proxy mode (`HTTP_PROXY` env in the agent) rather than transparent interception — weaker for our use case but the deployment pattern is reusable.

### 12.5 `b-m-f/wg-pod`

WireGuard inside Podman containers — a Go tool that joins a Podman container/pod into a WireGuard network on spawn. Creates a WG interface in the host namespace, moves it into the container namespace, routes `AllowedIPs` through it. Companion `bsd-ac/pns` does the same rootless in 60 lines of shell + nftables. Proves WireGuard-in-Podman is already solved and small, but for containers not VMs.

### 12.6 Docker `sbx` (proprietary)

Not open-sourced (`docker/sbx-releases` is proprietary, binary-only). The open artifact is `dockersamples/sbx-quickstart` which documents the behavior contract:
- "lightweight microVM with its own Linux kernel" (full hypervisor isolation)
- "host-side proxy that enforces network policy and injects API credentials"
- "Claude never sees raw credentials"
- Secrets stored in OS keychain, injected at request time by the proxy
- Network policies: Open / Balanced (default-deny + common dev hosts) / Locked Down
- Escape hatch: `/etc/sandbox-persistent.sh` inside the VM (weaker, secret lives in VM)

### 12.7 RogueSecurity — "Intercept and Monitor TLS Traffic with mitmproxy Using Podman"

Concrete walkthrough of mitmproxy WG mode + Podman containers as WG clients (`--cap-add=NET_ADMIN --cap-add=SYS_MODULE`). Uses `--network container:wireguard` so the app container shares the WG client's network namespace. CA installed via `update-ca-certificates`. Proves the WG+Podman path works in practice.

---

## 13. Open Risks

1. **WSL2 WireGuard kernel module** — the WSL2 custom kernel may not ship the `wireguard` module. Fallback: `wireguard-go` (userspace, needs TUN device + `NET_ADMIN`). Needs testing before v1 ship.
2. **FCOS limitation** — Podman Machine's guest is Fedora CoreOS (rpm-ostree, ignition-based). Custom provisioning scripts run via `podman machine ssh` post-start, not declaratively at boot like Lima's `provision:` block. Must handle rpm-ostree layering semantics for package installation (e.g., `rpm-ostree install wireguard-tools` may require a reboot).
3. **Pre-allocated port pool** — starting mitmdump with 80 WG instances may have startup latency or memory overhead. Unused WG instances should be negligible (no clients = no traffic = idle sockets), but needs benchmarking. If problematic, fall back to dynamic add/remove (requires mitmdump restart or the mitmproxy Python API to add WG servers at runtime).
4. **Inner IP exhaustion** — 80 ports × 80 IPs is plenty for a desktop tool, but if a user creates/destroys sandboxes rapidly without freeing ports, the pool could exhaust. `ppp rm` must reliably free ports even on crash.
5. **mitmproxy addon ↔ keychain IPC** — if `go-keyring` Python bindings aren't available, the addon shells out to `ppp secret _get <service>` per request. This is a subprocess call per HTTP request — acceptable for a dev tool but not ideal. Mitigation: addon caches secrets in memory with a TTL (e.g., 60s), refreshed on SIGHUP.
6. **`--import-native-ca` reliability** — the flag imports host-trusted CAs at `podman machine init` time. If the host doesn't trust the mitmproxy CA yet (first run), the guest won't either. Mitigation: `ppp setup` installs the mitmproxy CA into the host keychain first, then `podman machine init --import-native-ca` picks it up. Belt-and-suspenders: provision script also fetches from `http://mitm.it/cert/pem` after tunnel up.
7. **Single mitmdump blast radius** — if mitmdump crashes, all sandboxes lose their proxy. Mitmproxy is mature and rarely crashes, but for production-grade reliability, consider a supervisor (systemd unit on Linux, launchd plist on macOS, Windows service) that auto-restarts. v1 uses the Go binary as supervisor.
8. **`10.0.0.1/32` hardcoding workaround fragility** — the `Address =` rewrite relies on the text of mitmproxy's generated client config. If a future mitmproxy version changes the config format, the parser could break. Mitigation: pin the mitmproxy version in `ppp setup` and test against it; use a robust INI parser rather than string replacement. Note that since sandbox *identity* is now keyed on the listen port (§3.1), a wrong `Address` rewrite degrades routing clarity but does not break the security boundary.
9. **WG endpoint routing loop (RESOLVED in design, needs testing)** — `AllowedIPs = 0.0.0.0/0` would route WG's own datagrams into the tunnel. The provision script (§5.2 step 3) pins an off-tunnel `/32` route to the endpoint via the gvproxy gateway (`192.168.127.1`). Must be validated on each provider (libkrun/gvproxy on macOS, WSL, qemu) — the gateway IP and default-route behavior may differ, especially under `--user-mode-networking` on WSL.
10. **Sudo agent inner-IP spoofing (MITIGATED)** — a `sudo`-capable agent could reassign its `wg0` address to impersonate another sandbox's inner IP. Resolved by keying identity on the receiving WireGuard instance's **listen port** (via `flow.client_conn.proxy_mode.listen_port()` on mitmproxy 12.2.3 — **not** `flow.client.sockname`, which is the inner destination on that version), cryptographically bound to a per-sandbox keypair, rather than `flow.client.address`. Confirmed via source trace, a two-instance mitmdump spike, and a live macOS/libkrun end-to-end tunnel with two instances. Residual: the `process_outgoing_packet` "fall back to first peer" path (`wireguard.rs:326-333`) should be exercised under many-sandbox load to confirm no return-path cross-talk.
11. **WireGuard endpoint host address (RESOLVED via live spike)** — the guest must use the **gvproxy host alias `192.168.127.254`** as the WireGuard `Endpoint`, not the host LAN IP; the LAN-IP path silently drops handshake packets on libkrun. The supervisor rewrites `Endpoint` accordingly.
12. **mitmdump stdout buffering (impl note)** — mitmdump block-buffers stdout when redirected to a file, so `proxy.log` can stay empty while running; the supervisor must capture via a PTY or set `PYTHONUNBUFFERED=1` to read client-config blocks promptly.
13. **WireGuard peer public key (impl note)** — the per-port keys file holds *private* keys; the peer **public** key for `wg0.conf` must be taken from the emitted client-config block, or the handshake fails (`InvalidAeadTag`).
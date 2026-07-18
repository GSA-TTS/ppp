# Concurrent microVMs on macOS: where the "only one VM can be active" limit comes from

**Context:** `ppp` wants one dedicated microVM per sandbox, concurrently. On macOS with
Podman Machine (libkrun/krunkit, podman 6.0.1), starting a second machine while one is
running fails:

> `Error: unable to start "vm2": vm1 already starting or running: only one VM can be active at a time`

**Verdict up front:** The limit is a **Podman Machine policy decision, not a hypervisor
constraint.** It is a single per-provider boolean gate. The underlying macOS hypervisor
(Virtualization.framework / HVF via libkrun) runs many concurrent VMs fine — proven by Lima,
which runs N concurrent VMs on the exact same substrate. A future `ppp` rev can achieve
concurrent per-sandbox microVMs by dropping below `podman machine` (drive `krunkit`/`vfkit`
directly, or delegate to Lima).

Podman source refs below are from `containers/podman` @ `ecf88eb` (v6.1.0-dev, current `main`;
same code paths exist in 6.0.1).

---

## 1. Where the limit lives — it's Podman policy, per-provider

**The error string** is produced by `ErrMultipleActiveVM` in
`pkg/machine/define/errors.go:49-60`:

```go
type ErrMultipleActiveVM struct { Name string; Provider string }
func (err *ErrMultipleActiveVM) Error() string {
    // "%s already starting or running%s: only one VM can be active at a time"
}
```

**It is raised by** `checkExclusiveActiveVM()` in `pkg/machine/shim/host.go:358-388`, which
iterates all known machines, calls `Provider.State()` (which probes the *live* hypervisor),
and errors if any machine is `Running` or `Starting`.

**It is gated by one boolean** in `Start()` — `pkg/machine/shim/host.go:537-573`:

```go
// Don't check if provider supports parallel running machines
if mp.RequireExclusiveActive() {
    startLock, _ := lock.GetMachineStartLock() // global lockfile
    startLock.Lock()
    if err := checkExclusiveActiveVM(mp, mc); err != nil { return err }
} else {
    // only guard against starting the SAME machine twice
}
```

**`RequireExclusiveActive()` is per-provider** (interface in `pkg/machine/vmconfigs/config.go:79`):

| Provider | `RequireExclusiveActive()` | File |
|---|---|---|
| applehv (Virtualization.framework / vfkit) | **`true`** | `pkg/machine/applehv/stubber.go:39` |
| libkrun (krunkit) | **`true`** | `pkg/machine/libkrun/stubber.go:130` |
| qemu | `true` | `pkg/machine/qemu/stubber.go:49` |
| hyperv | `true` | `pkg/machine/hyperv/stubber.go:58` |
| **wsl** | **`false`** | `pkg/machine/wsl/stubber.go:190` |

WSL already runs multiple machines concurrently *because it returns false*. The lock comment
(`pkg/machine/lock/lock.go:22-24`) says the quiet part out loud:

> "This is required as **most providers support at max 1 running VM** and to check this race
> free we cannot allow starting two machine."

So the gate is a *global* mechanism in the shim, but the *decision to enforce it* is
per-provider. On macOS, **both** macOS providers (applehv and libkrun) opt in to exclusivity.
Docs restate it as policy: `podman-machine-start.1.md.in:23` — "Only one Podman managed VM can
be active at a time."

**Maintainer's stated rationale** (issue #26281, comment by `baude`, Podman machine lead):

> "Podman machine is only designed to run one machine at a time. There are reasons deeper than
> podman, as some of the supporting applications do not do a great job of supporting this."

The "supporting applications" are the **host-side plumbing** (gvproxy forwarder + the
well-known `docker.sock`/`podman.sock` singletons), *not* the hypervisor. See §4.

---

## 2. Per-provider hypervisor reality on macOS

### libkrun / krunkit
- `podman machine` (libkrun) shells out to one **`krunkit`** process per VM
  (`pkg/machine/libkrun/stubber.go:21` `krunkitBinary = "krunkit"`;
  `pkg/machine/apple/apple.go:112-160` builds the argv and `exec`s it).
- `krunkit` itself is a **plain, single-VM-per-process VMM** with no global singleton: its CLI
  (`libkrun/krunkit` `docs/usage.md`) takes `--restful-uri`, `--pidfile`, per-VM `--device`
  disks/net/vsock, etc. Nothing in its design forbids launching it N times. `--pidfile`
  explicitly notes "does not provide any form of locking." Each krunkit embeds its own libkrun
  VMM instance (HVF). **libkrun/krunkit does NOT forbid multiple concurrent microVMs** — the
  serialization is entirely podman's `RequireExclusiveActive()` wrapper.
- Strong external proof: **Lima** ships a `krunkit` driver and runs multiple krunkit VMs
  concurrently (Lima krunkit docs; each `limactl start --vm-type=krunkit <name>` is its own VM).

### applehv (Apple Virtualization.framework via vfkit)
- Same shape: `podman machine` (applehv) shells out to one **`vfkit`** process per VM
  (`pkg/machine/applehv/stubber.go:25` `vfkitCommand = "vfkit"`; started via the same
  `apple.StartGenericAppleVM` path).
- Podman imposes the **same** one-active limit for applehv (`RequireExclusiveActive()` → `true`
  at `applehv/stubber.go:39`). So on macOS, switching providers to applehv does **not** avoid
  the limit — it is not libkrun-specific.
- But `vfkit` / Virtualization.framework itself allows many concurrent VMs: `vfkit` is
  one-VM-per-process (crc-org/vfkit) and multiple processes each create their own
  `VZVirtualMachine`. **Lima's default macOS driver is `vz`** (Virtualization.framework) and
  routinely runs many instances at once (Lima VZ docs; `vz` is default since Lima v1.0). That is
  direct evidence AVF has no host-wide single-VM restriction.

**Conclusion for §1–2:** The limit is podman-machine policy applied at the provider level.
Neither macOS hypervisor path (HVF-via-libkrun, AVF-via-vfkit) has an intrinsic single-VM limit.

---

## 3. Is it configurable/removable?

- **No user-facing flag or config** toggles it. It is hard-coded per provider via
  `RequireExclusiveActive()` returning `true`. There is no `--allow-multiple`,
  `containers.conf` key, or env var.
- **Open feature request:** containers/podman **#26281** "Allow multiple Podman Machines to run"
  (open, `kind/feature`, `machine`, currently `stale-issue`). Maintainer response: designed for
  one; would reconsider with a compelling use case. No PR merged.
- A detailed community analysis on that issue (the "Full Claude analysis" comment) confirms the
  block is "**one gate, not deep architectural coupling**," and that most host-side plumbing is
  already namespaced per machine (see §4). It proposes an opt-in flag as the minimal ask.
- Notably, that same thread contains a comment from a developer building **exactly a ppp-style
  tool** — "sandboxed dev environments … secretless by default … injecting credentials via HTTP
  header using mitmproxy on the host" — who hit this same wall wanting a second concurrent VM.

---

## 4. Lower-abstraction workaround for a future ppp rev

### What's already per-machine (so it wouldn't collide across VMs)
Podman already namespaces almost all host-side plumbing per machine:
- gvproxy listening socket `{name}-gvproxy.sock` (`pkg/machine/vmconfigs/sockets.go:11-12`)
- API socket `{name}-api.sock`, per-machine SSH port, per-machine vfkit/krunkit REST endpoint
  (random port; `pkg/machine/vmconfigs/config_darwin.go`)
- Named connections switchable via `podman system connection default <name>`

The genuinely shared/singleton bits are: the gvproxy pidfile, and the well-known
`/var/run/docker.sock` + global `podman.sock`. And even `docker.sock` **already degrades
gracefully** — first machine to start claims it, later machines fall back to their own
machine-local socket (`pkg/machine/shim/networking_unix.go:86-93`, `claimDockerSock()`). Those
singletons are just "which machine is the default target," which ppp doesn't need since it drives
each sandbox explicitly.

### Option A — drive `krunkit` directly (one process per sandbox)
**Feasible.** `krunkit` is a standalone VMM binary designed to be launched with a full CLI
(`--cpus/--memory`, `--device virtio-blk/net/fs/vsock`, `--restful-uri` for lifecycle,
`--pidfile`). Launch N of them, each with its own disk image, its own gvproxy/vmnet socket, and
its own vsock. Nothing in krunkit serializes across processes. This is the closest analog to what
`podman machine` (libkrun) does under the hood, minus the exclusivity gate. It also unlocks GPU
(Venus/Vulkan) if ever wanted. Requires macOS 14+, Apple Silicon.

### Option B — drive `vfkit` directly (one process per sandbox)
**Feasible**, same shape but using Apple Virtualization.framework (crc-org/vfkit). One `vfkit`
process per VM, each its own `VZVirtualMachine`. This is precisely what Lima's `vz` driver does N
times. Slightly more mature/standard than krunkit on macOS; no GPU passthrough.

### Option C — delegate to **Lima** (recommended lowest-effort path)
Lima already solves the whole problem: N concurrent VMs on `vz` (default) or `krunkit`, each with
per-instance networking, mounts, port-forwarding, and a **podman template** so each VM runs a real
podman service exposing a libpod socket. You stay in pure-podman-remote land; ppp switches
`CONTAINER_HOST` / named connection per sandbox. Multiple concurrent instances are *normal* in
Lima, not a workaround.

### What ppp loses by leaving `podman machine`
1. **Lifecycle glue** — init/start/stop/rm, ignition first-boot provisioning, image pull of the
   Fedora CoreOS disk, SSH key injection, config persistence. ppp (or Lima) must own all of this.
2. **gvproxy networking** — the user-mode NAT + host↔guest forwarding (`gvproxy`,
   `gvisor-tap-vsock`) is wired up by podman today. Driving krunkit/vfkit directly means ppp runs
   and wires its own gvproxy (or vmnet-helper) per VM — *doable* (krunkit takes
   `--device virtio-net,type=unixgram,path=<gvproxy.sock>`), but it's now ppp's responsibility.
   **This is actually convenient for ppp's WireGuard-through-mitmproxy egress model**, since ppp
   already intends to own the network path.
3. **Image management** — pulling/caching the guest OS disk image and applying overlays.

Lima gives most of #1–#3 back for free (it's the reason Lima exists); direct krunkit/vfkit means
ppp reimplements them.

### Plausible v2 direction? **Yes, not a dead end.**
The hypervisor supports concurrency; only podman's wrapper serializes. Concrete ranked options:
- **v2a (lowest risk):** delegate to Lima (`vz` or `krunkit`), one instance per sandbox, podman
  runs inside each. Keeps the "one dedicated VM per sandbox, VMs never shared" invariant cleanly.
- **v2b:** drive `krunkit`/`vfkit` directly, one process per sandbox, ppp owns networking (fits
  the mitmproxy/WireGuard egress design) and lifecycle. Most control, most work.
- **v2c (upstream):** contribute the opt-in flag proposed in #26281 (flip
  `RequireExclusiveActive()` behind a config) — smallest code change to podman itself, but depends
  on upstream acceptance and the explicit-connection-selection model.

---

## 5. What comparable tools do (evidence the limit is policy)

- **Lima (limactl):** runs **N concurrent VMs by design** on macOS. Default driver is `vz`
  (Virtualization.framework, default since Lima v1.0); also supports `krunkit` (libkrun) and
  `qemu`. Each `limactl start --name <x>` is an independent, concurrently-running instance with
  per-instance networking/mounts/forwarding. Since Lima runs many VMs on the *same* vz/krunkit
  substrate podman uses, this is decisive: **the substrate is not the limiter; podman's policy
  is.**
- **Colima / Docker Desktop / Rancher Desktop** all build on Lima-style single-tool-per-VM VMMs;
  none has a host-wide single-VM hypervisor restriction.

---

## Bottom line

- **(a) Where the limit comes from:** Podman Machine *policy* — one per-provider boolean
  (`RequireExclusiveActive()`), enforced by `checkExclusiveActiveVM()` in
  `pkg/machine/shim/host.go`, backed by a global start-lock. **Not** a hypervisor constraint.
- **(b) Does applehv avoid it?** **No.** applehv also returns `true`; the limit applies to both
  macOS providers. Switching provider inside `podman machine` won't help. (The vfkit/AVF
  *hypervisor* itself has no such limit.)
- **(c) Can a lower-abstraction future rev get concurrent per-sandbox microVMs?** **Yes.** Drive
  `krunkit` or `vfkit` directly (one process per sandbox), or delegate to Lima — all run N
  concurrent VMs on the same macOS hypervisor. Proven by Lima today.
- **(d) Recommended stance:**
  - **v1:** accept the one-active-VM limit; do **not** hack around it inside `podman machine`.
    Either serialize sandboxes (one live VM at a time) or, if concurrency is needed now, use Lima
    under the hood. Do not violate the "one dedicated VM per sandbox, never shared" invariant to
    fake concurrency.
  - **v2:** move below `podman machine`. Preferred: **Lima-per-sandbox** (`vz` or `krunkit`) for
    lifecycle/networking glue with minimal reinvention; alternative: **direct krunkit/vfkit** if
    ppp wants full control of the network path (which aligns with the WireGuard→mitmproxy egress
    design). Optionally pursue the upstream opt-in flag (#26281) in parallel.

## Sources
- containers/podman @ ecf88eb (v6.1.0-dev): `pkg/machine/define/errors.go`,
  `pkg/machine/shim/host.go`, `pkg/machine/lock/lock.go`, `pkg/machine/vmconfigs/config.go`,
  `pkg/machine/{applehv,libkrun,qemu,hyperv,wsl}/stubber.go`, `pkg/machine/apple/apple.go`,
  `pkg/machine/shim/networking_unix.go`, `docs/source/markdown/podman-machine-start.1.md.in`
- containers/podman issue #26281 "Allow multiple Podman Machines to run" (open) — maintainer
  rationale + community analysis of the single gate.
- libkrun/krunkit README + `docs/usage.md` (krunkit CLI; one-VM-per-process, no locking).
- Lima docs: VM types (vz default since v1.0), Krunkit driver (experimental, macOS 14+ arm64),
  lima-vm.io — concurrent instances are the normal model.

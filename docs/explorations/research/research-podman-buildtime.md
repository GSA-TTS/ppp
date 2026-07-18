# Podman from-source build: time & complexity (research)

Read-only investigation of `containers/podman` (github.com/containers/podman) and podman.io
build docs. Question: realistic wall-clock time and complexity to build the podman **client
binary** from source (v6.0.1) in a Docker image (Ubuntu 26.04, Go 1.26) vs. Homebrew.

> Note: The repo `go.mod` on `main` targets `module go.podman.io/podman/v6`, `go 1.25.9`.
> v6.0.1 will be materially the same shape. Go 1.26 satisfies the toolchain requirement.

---

## 1. What `make podman` actually compiles

From the Makefile:

- `make podman` -> target `bin/podman`, which is a **single `go build ... -o bin/podman ./cmd/podman`**.
  It is one Go build of one main package (the whole CLI + libpod compiled in).
- `make binaries` (the default via `all`) builds **more** than just podman on Linux:
  `podman podman-remote podman-testing podmansh rootlessport quadlet`. That's ~5 separate
  `go build` invocations (podmansh is just a symlink to podman). So do **not** run
  `make` / `make all` / `make binaries` if you only want the client — that roughly
  multiplies build work (podman-remote and podman-testing each recompile large overlapping
  package sets; the Go build cache helps, but it's still extra).
- `make all` = `binaries docs` — `docs` additionally generates man pages (see §4).
- **To build only the client binary: `make podman` (or `go build ./cmd/podman`).**

Code size / dependency surface (proxy for compile work — this is a *large* Go project):
- go.mod: ~85 direct requires + ~95 indirect = **~180 modules**, all **vendored** in-tree
  (`vendor/` includes buildah, containers/{common,image,storage}, moby/buildkit, grpc,
  opentelemetry, sigstore, k8s yaml, etc.). Podman statically links buildah/skopeo-level
  functionality, so the client binary pulls in a very large chunk of the containers ecosystem.
- Net effect: first-ever compile is heavy (thousands of packages incl. vendored deps);
  the resulting binary is ~50 MB+.

## 2. CGO requirement (affects build)

- Makefile: `CGO_ENABLED ?= 1`. On **Linux, CGO is required** ("Podman does not work w/o
  CGO_ENABLED, except in some very specific cases"). Only the darwin/windows **podman-remote**
  client forces `CGO_ENABLED=0`.
- Default Linux `BUILDTAGS` auto-detect and include: `seccomp`, `systemd` (journald),
  apparmor, btrfs, sqlite, libsubid. These pull in C libraries via cgo, so a working C
  toolchain + `-dev` headers + `pkg-config` are mandatory at build time.
- Consequence: CGO means gcc is invoked; the build is slower and less cacheable than a pure-Go
  static build, and it cannot be trivially cross-compiled.

## 3. Non-Go build dependencies (Debian/Ubuntu, from podman.io "Building from Source")

`apt-get install` of the documented build deps:

```
btrfs-progs gcc git golang-go go-md2man iptables \
libassuan-dev libbtrfs-dev libc6-dev libdevmapper-dev libglib2.0-dev \
libgpgme-dev libgpg-error-dev libprotobuf-dev libprotobuf-c-dev \
libseccomp-dev libselinux1-dev libsystemd-dev make netavark passt \
pkg-config runc uidmap
```

For a **build-only** image you can drop pure-runtime packages (netavark, passt, iptables,
runc, uidmap, btrfs-progs) and go-md2man (see §4). The load-bearing *build* deps are:
`gcc`, `make`, `pkg-config`, `libc6-dev`, and the cgo `-dev` headers:
`libseccomp-dev`, `libgpgme-dev`, `libassuan-dev`, `libgpg-error-dev`, `libbtrfs-dev`,
`libdevmapper-dev`, `libglib2.0-dev`, `libsystemd-dev`, `libselinux1-dev`.
(protobuf `-dev` only needed if regenerating protos, not for a plain build.)

`apt-get install` time for this set on a warm mirror: **~20–60 s** (these are small dev
headers, not big toolchains). `apt-get update` adds ~5–15 s. Rounding: **~30–90 s** total
for the apt layer.

## 4. go-md2man / docs — NOT needed for the binary

- `go-md2man` is invoked only by the `docs` / man-page targets and pulled in via
  `.install.md2man` (built from `test/tools`). It is a dependency of `make install`
  (`install.man`) and `make all` (`docs`), **not** of `make podman`.
- **You can build only the client and skip docs.** Use `make podman` (or `go build ./cmd/podman`)
  and copy the resulting `bin/podman`; do not run `make install` (which would trigger man-page
  generation). This saves the go-md2man build + man-page generation entirely.

## 5. Wall-clock estimate (build the podman client binary in a Docker layer)

Assumptions: modern multi-core CI/dev machine (8–16 cores), warm apt + Go module cache is
NOT assumed (Docker layer = cold Go build cache the first time).

| Step | Cold (first build) | Warm (cached go-build) |
|---|---|---|
| `apt-get update` + install build deps | 30–90 s | 30–90 s |
| `git clone` at tag v6.0.1 (shallow) | 10–40 s | 10–40 s |
| `make podman` (single cgo `go build ./cmd/podman`) | **90–210 s** | 15–45 s |
| **Total** | **~2.5–6 min** | **~1–3 min** |

- The dominant cost is the first `go build`: it's a big project (~180 vendored modules,
  large static binary, cgo enabled). On a strong multi-core box expect **~1.5–3.5 min**
  for the compile alone; on a small/constrained CI runner (2 vCPU) it can push toward
  **5+ min**. With a persistent Go build cache mounted, rebuilds drop to well under a minute.
- Because it's cgo, the compile does not parallelize the C bits as cleanly and is less
  portable across layers than a pure-Go build.

### Bottom line

Building the podman **client** from source is **not exotic but not free**: one `go build`
of a large, cgo-enabled, heavily-vendored project. Realistic Docker-layer wall-clock is
**~2.5–6 minutes cold** (apt deps + clone + `make podman`), dominated by the ~1.5–3.5 min
first Go compile; **~1–3 min** with a warm build cache. Complexity is meaningfully higher
than a prebuilt bottle/binary: you must install a C toolchain + ~9 `-dev` header packages,
have `pkg-config` and Go present, and keep the build/runtime dependency split correct.

A prebuilt route (Homebrew bottle, or the official GitHub release binary / distro package)
is **seconds to ~1 min**, no compiler, no `-dev` headers, no cgo pitfalls, and the version is
pinned/verified upstream — far simpler for a reproducible Docker image. From-source only pays
off if you need a specific patch, custom BUILDTAGS, or a version not yet packaged.

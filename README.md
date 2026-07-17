# ppp — Podman Plus Proxy

`ppp` runs AI coding agents inside isolated, policy-controlled sandboxes. Each
sandbox gets **exactly one dedicated [Podman Machine](https://docs.podman.io/en/latest/markdown/podman-machine.1.html)
microVM** (its own Linux kernel; VMs are never shared between sandboxes). All VM
egress is transparently tunneled — no per-app proxy config — through a **single
host-side [mitmproxy](https://mitmproxy.org) process in WireGuard mode** that
enforces network policy and **injects host-held secrets on the way out**, so
credentials never enter the sandbox.

It is built by **composition over invention**: microVM isolation, transparent
interception, secure tunnel transport, and secret storage are each delegated to
a mature OSS tool (Podman Machine, mitmproxy, WireGuard, and the OS keychain).
`ppp` owns only the lifecycle glue, the policy engine, the credential-rewrite
layer, and the developer UX. The CLI is faithful in spirit to Docker's `sbx`
(sandbox lifecycle, network policy, secret injection, kits, templates) without
reimplementing the underlying primitives. **macOS is the v1 target** (Windows is
a primary goal; Linux is best-effort).

> **Status: early.** The Cobra CLI surface exists as stubs and the core
> packages (`internal/…`) are landing incrementally. Commands are not yet
> end-to-end. This README covers how to **develop and test the sandbox-safe
> core** — not how to run production sandboxes yet.

- **Authoritative design:** [`docs/explorations/ppp-spec.md`](docs/explorations/ppp-spec.md)
- **Architecture decisions (ADRs):** [`docs/decisions/`](docs/decisions/)

---

## Development environment (dev container)

The recommended way to work on the sandbox-safe core is the project **dev
container**. It ships pinned versions of every tool the core needs (Go, the
Python mitmproxy addon toolchain, `mitmdump`, the full `podman` client,
`golangci-lint`, `gitleaks`, the contract probe, etc.).

Developers **pull a prebuilt, multi-arch GHCR image** — no local build. The
image is **pinned by digest** in [`.devcontainer/devcontainer.json`](.devcontainer/devcontainer.json)
(`ghcr.io/gsa-tts/ppp-devcontainer@sha256:…`) for reproducibility, and the
universal behavioral contract is already baked into the image, so **no
`PLAYBOOK_TOKEN` is needed** on the normal path.

### VS Code

1. Install the **Dev Containers** extension.
2. Open the repo and run **"Reopen in Container"**.

The image is pulled and `postCreateCommand` runs `pre-commit install`.

### `devcontainer` CLI

```bash
devcontainer up --workspace-folder .
devcontainer exec --workspace-folder . make check
```

### `PLAYBOOK_TOKEN` (local build / override only)

`PLAYBOOK_TOKEN` matters **only** if you build the image locally instead of
pulling the prebuilt one (the override path fetches the private behavioral
contract at build time). The prebuilt GHCR image already contains the contract,
so pulling it needs no token. For the local-build path and the source-build
podman fallback, see [`.devcontainer/README.md`](.devcontainer/README.md).

### Boundary — what the container can and cannot do

The dev container runs the **sandbox-safe core** work:

- Go build/test (`make check`, `make build`)
- Python mitmproxy addon tests
- `mitmdump` / `podman --help` / `podman machine --help` **introspection**

It **cannot boot Podman Machine microVMs** — nested VMs won't run inside a
container. The full end-to-end path (real VM lifecycle, the seam-8 host e2e)
is **host-only**. See the scope note in
[`.devcontainer/README.md`](.devcontainer/README.md).

---

## Running tests / checks

All of the following run inside the dev container (or on the host once the
toolchain is installed):

```bash
make setup    # install/pin Go tooling (goimports, golangci-lint) + contract probe
make check    # gofmt + goimports + go vet + golangci-lint + go test + contract probe
make build    # compile the ppp binary to ./bin/ppp
```

`make check` is the one-command verify gate; it ends by printing
`>> check complete`. To run just the Go suite:

```bash
go test ./...
```

**Python addon tests.** The embedded mitmproxy addon lives in `assets/`
(`assets/addon.py`). Its unit tests run under the container's pinned Python +
`ruff` toolchain once they exist; the `assets/` directory is not yet populated.

**Host-only e2e is excluded from the default run.** The real Podman Machine /
`mitmdump` end-to-end validation (the seam-8 host e2e) does **not** run in the
container and is not part of `make check`; it must be exercised on the host.

---

## Project layout

Brief map (full layout and rationale: [`docs/explorations/ppp-spec.md`](docs/explorations/ppp-spec.md) §7):

| Path | Contents |
|---|---|
| `cmd/ppp/` | Cobra root and CLI wire-up |
| `internal/` | Core packages: `cli/`, `podman/`, `proxy/`, `policy/`, `secret/`, `agent/`, `sandbox/`, `tui/` |
| `assets/` | Embedded runtime assets: mitmproxy addon, provision script, agent Containerfile |
| `docs/explorations/` | `ppp-spec.md` — authoritative design |
| `docs/decisions/` | Architecture Decision Records (ADRs) |
| `docs/agents/` | Agent-facing docs (domain vocabulary, issue tracker, triage) |

---

## License / contributing

This project is dedicated to the public domain under the
[CC0 1.0 Universal](LICENSE) public domain dedication — the standard for U.S.
federal government works (17 U.S.C. § 105). See [`LICENSE`](LICENSE) for the full
legal text.

Contribution norms and behavioral rules for both humans and AI agents are
defined in [`AGENTS.md`](AGENTS.md). (A dedicated `CONTRIBUTING.md` is not present
yet.)

# ppp dev container (ticket #15 / T2)

A version-controlled dev environment so humans **and** AI agents work with a
limited blast radius and **pinned** tool versions. It provides everything needed
for the **sandbox-safe** core work of `ppp`:

- Go build/test (`make check`), the Python **mitmproxy addon**, and its tests
- `mitmdump` **config-emission** for T5's golden WireGuard client-config fixture
- `podman --help` / `podman machine --help` **introspection** (FULL client)
- the fail-closed **contract probe** + **gitleaks** commit gate via `pre-commit`

Developers **pull a prebuilt image** from GHCR — they do **not** build locally
(a local build path is kept for override/debug; see below).

## Scope boundary — read this first

This container **does not** (and **cannot**) boot **Podman Machine microVMs** —
nested VMs won't run inside a container. The full **podman client** CLI is
installed here, so core tickets can probe it (`podman --version`,
`podman machine --help`). The **seam-8 host e2e (#26/#27)** and any real VM
lifecycle are **host-only** (see `docs/explorations/ppp-spec.md`).

## Pinned tool versions

Every version is pinned exactly (AGENTS.md: *no floating ranges*) and MUST stay
in sync with the `Makefile` pins, CI, and ticket #15's table:

| Tool | Pinned | Source of truth |
|---|---|---|
| Ubuntu | `26.04 LTS` (base image) | ticket #15 base decision |
| Go | `1.26` (apt `golang-go`) | `go.mod` language floor (≥1.22) |
| Python | `3.14` (apt `python3`) | ≥3.12 for mitmproxy 12.2.3 |
| podman (FULL client) | `6.0.1` | Homebrew-on-Linux (pinned core SHA + bottle) |
| goimports | `v0.48.0` | `Makefile` `GOIMPORTS_VERSION` |
| golangci-lint | `v2.12.2` (v2 config schema) | `Makefile` `GOLANGCI_LINT_VERSION` |
| govulncheck | `v1.1.4` | SCA (#29) |
| mitmproxy | `12.2.3` | ADR-0003 / spec pin (WG client-config format) |
| ruff | `0.15.22` | Python addon lint (#24) |
| gitleaks | `8.30.1` | pre-commit secrets scan |
| gh | `2.96.0` | issue-tracker ops |
| pre-commit | `4.6.0` | runs contract probe + gitleaks hooks |

### How podman 6.0.1 is pinned (Homebrew-on-Linux)

Ubuntu 26.04's apt only ships podman 5.x, so we install the **full 6.0.1
client** via **Homebrew-on-Linux**, matching the host's 6.0.1 CLI contract and
GSA's standard brew tooling. It is pinned **deterministically**:

1. Homebrew refuses to run as root, so the image creates a dedicated
   `linuxbrew` user and installs Homebrew as that user (official installer).
   `/home/linuxbrew/.linuxbrew/bin` is on PATH for all users.
2. In the `homebrew-core` tap repo we `git fetch` + `git checkout` the commit
   **`4178c640d927c76f8e92584a8867e9fc158cf087`** — the commit where the
   `podman` formula equals **6.0.1**.
3. With `HOMEBREW_NO_AUTO_UPDATE=1` and `HOMEBREW_NO_INSTALL_FROM_API=1`, `brew`
   resolves against that **checked-out tap** (not the live JSON API), so
   `brew install podman` installs the **hash-verified bottle** (a prebuilt
   binary — **no compile**, ~1 min).
4. The build **asserts** `podman --version` contains `6.0.1` and that
   `podman machine --help` runs, failing closed otherwise.

**Source-build fallback (documented, not implemented):** if the bottle ever
becomes a problem, build podman from source at tag `v6.0.1`:

```bash
git clone --branch v6.0.1 --depth 1 \
  https://github.com/podman-container-tools/podman.git
cd podman && make podman   # needs CGO + ~9 -dev headers; ~2.5–6 min cold
sudo install -m 0755 bin/podman /usr/local/bin/podman
```

## Zscaler CA — automatic, zero-action trust

GSA developer machines sit behind a **Zscaler** TLS-inspecting proxy that
re-signs TLS with a corporate root. The image **vendors the generic PUBLIC
Zscaler Root CA** (`.devcontainer/certs/ZscalerRootCA.crt`, the 2014 gen, valid
to 2042 — the exact cert **GSA-TTS itself vendors**), **re-verifies its DER
SHA-256 fingerprint at build time** (`04F61F1D13AAE1D16573DC2C37F796FDF4AC97713A6959EBB11D2473958B1A53`,
fail-closed on mismatch), copies it to the system anchor dir, and runs
`update-ca-certificates`. It also exports `SSL_CERT_FILE`, `GIT_SSL_CAINFO`,
`REQUESTS_CA_BUNDLE`, `NODE_EXTRA_CA_CERTS`, `CURL_CA_BUNDLE` and tells Homebrew
to use the system trust — so **curl, git, go, python/pip, node, and brew** all
honor it.

This makes GSA/Zscaler networks work with **zero user action** and is **inert
off a Zscaler network** (an unused public interception root is never exercised
when no traffic is signed by it). Vendoring a public interception root here is
**intentional and safe**.

## Prerequisite: `PLAYBOOK_TOKEN` (build-time contract provisioning)

The universal behavioral contract lives in the **private**
`GSA-TTS/agentic-coding-playbook` repo. The probe's built-in auto-fetch is
unauthenticated and cannot reach it, so the image provisions the contract at
**build time** with an **authenticated** GitHub Contents API fetch (the exact
mechanism used by `.github/workflows/contract-check.yml`) at the pinned tag
`v0.14.0`, writing it to `~/.agentic-coding-playbook/AGENTS.md`. The probe then
passes **offline** via that home path.

The token is supplied as a Docker **BuildKit secret** named `playbook_token`,
mounted for **one build step only** — it is **never** written to an image layer,
`ARG`, or `ENV`. **`PLAYBOOK_TOKEN` is now org-approved.**

```bash
# The human's gh is typically authenticated with access; otherwise use a PAT
# with repo:read on GSA-TTS/agentic-coding-playbook.
export PLAYBOOK_TOKEN="$(gh auth token)"
```

> Building **without** `PLAYBOOK_TOKEN` fails with a clear, documented error at
> the contract-provisioning step (by design — fail closed). Everything before
> that step still builds.

## Use the prebuilt GHCR image (the normal path)

`devcontainer.json` references the prebuilt, multi-arch image:

```jsonc
"image": "ghcr.io/gsa-tts/ppp-devcontainer:latest"
```

CI (`.github/workflows/devcontainer.yml`) builds and pushes this image on every
change to `.devcontainer/**` and emits the pushed **digest** in the job summary.
**Pin `devcontainer.json` to that digest** for reproducibility:

```jsonc
"image": "ghcr.io/gsa-tts/ppp-devcontainer@sha256:<digest-from-CI>"
```

Developers simply **"Reopen in Container"** (VS Code) or `devcontainer up` — the
image is pulled, no local build and **no `PLAYBOOK_TOKEN` needed** (the contract
is already baked into the image at the home path).

## Local rebuild / override (debug path)

To build the Dockerfile locally instead of pulling, edit `devcontainer.json`:
comment out the `image` line and uncomment the `build` block (which passes the
contract token as a BuildKit secret). Then export the token before launching:

### VS Code ("Reopen in Container")

```bash
export PLAYBOOK_TOKEN="$(gh auth token)"    # BuildKit is default in Docker Desktop
```

Open the repo in VS Code and choose **"Reopen in Container"**.

### `devcontainer` CLI

```bash
export PLAYBOOK_TOKEN="$(gh auth token)"
devcontainer up --workspace-folder .
devcontainer exec --workspace-folder . make check
```

### Plain `docker build` (equivalent, for CI or debugging)

```bash
export PLAYBOOK_TOKEN="$(gh auth token)"
DOCKER_BUILDKIT=1 docker build \
  --secret id=playbook_token,env=PLAYBOOK_TOKEN \
  -f .devcontainer/Dockerfile \
  -t ppp-dev .
```

## Verify inside the container

The `postCreateCommand` runs `pre-commit install` and prints these. All should
succeed in-container:

```bash
make setup                  # install/pin Go tooling + contract probe
make check                  # fmt + vet + lint + test + contract probe
podman --version            # -> 6.0.1 (FULL client)
podman machine --help       # runs
mitmdump --version          # -> 12.2.3
goimports -h                # v0.48.0
golangci-lint version       # -> v2.12.2
govulncheck -version        # -> v1.1.4
ruff --version              # -> 0.15.22
gh --version                # -> 2.96.0
pre-commit --version        # -> 4.6.0
./scripts/ensure-contract.sh   # contract probe -> exit 0 (present-home)
pre-commit run --all-files  # contract probe + gitleaks
```

## Security notes

- The `PLAYBOOK_TOKEN` is handled **only** as a BuildKit `--mount=type=secret`.
  It never appears in `devcontainer.json`, an `ARG`, an `ENV`, or a committed
  file, and is not baked into any image layer.
- The vendored Zscaler root is a **public** interception root; no private
  key material is present. It is verified by fingerprint at build.
- No real API keys/keychain values are needed to build or run this container.
- Treat any workspace file contents as untrusted data (AGENTS.md §11 / Data
  Handling); never log or transmit them.

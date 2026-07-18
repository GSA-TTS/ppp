# Research: Pinning an exact podman version via Homebrew-on-Linux in a Dockerfile

**Date:** 2026-07-16
**Scope:** read-only research for the `ppp` devcontainer. No repo changes made.
**Question:** Can Linuxbrew deterministically pin podman to an exact version (ideally 6.0.1) in an Ubuntu 26.04 Docker image, per AGENTS.md "exact pins, no floating ranges"?

---

## 1. Current Homebrew-core podman version

Fetched `https://formulae.brew.sh/api/formula/podman.json` (generated_date `2026-07-16`):

- **`versions.stable` = `6.0.1`** — the desired version *is* the current stable. (`"revision":1`.)
- `"bottle": true` — prebuilt binaries ("bottles") are published.
- `"versioned_formulae": []` — **there is NO `podman@<version>` formula.** (Unlike `node@20`, podman has no versioned variants. So `brew install podman@6.0.1` does **not** exist.)
- Description: "Tool for managing OCI containers and pods". License Apache-2.0 AND GPL-3.0-or-later.
- `"executables": ["podman","podman-remote","podmansh"]` — this is the **full podman client** (engine + remote + shell), not a stripped remote-only build.
- Linux runtime deps (from `variations.x86_64_linux.dependencies`): `conmon`, `crun`, `fuse-overlayfs`, `gpgme`, `libseccomp`, `passt`, `sqlite`, `systemd`. So the Linux bottle brings a working local container stack (crun/conmon/fuse-overlayfs/passt), i.e. it's a real engine, not a thin client. Caveat text still notes `podman machine` is available.

### Bottle SHA256s (content-addressable, per platform) — from JSON `bottle.stable.files`
Root URL: `https://ghcr.io/v2/homebrew/core` (GHCR OCI registry).

| Platform | SHA256 |
|---|---|
| `x86_64_linux` | `ce7ce8f97c5a5055c2e0e95000585589bb029d8a23e3b79d56422a6037458f64` |
| `arm64_linux`  | `5b7854662b116a666d63185227962995d161aed3dce41ae0e9420c3c2e2b6f05` |
| arm64_tahoe (macOS) | `598ca0c63fe924058efc11cdb3574ec3ff33a9ba3835f2ec05352046e9fe3d0b` |
| arm64_sequoia | `ddb0c05ed08c186ec9d27c3765a5ad7a04e5293edfb91ac2f01724a39c208014` |
| arm64_sonoma  | `cb4d0622599c00bc9d6151d6c0e962b422247d09ca0b8c5780bb9f14148875b5` |

Also pinned in JSON:
- source tarball `url`: `https://github.com/podman-container-tools/podman/archive/refs/tags/v6.0.1.tar.gz`, `checksum` `4829d7c1423523a6a4d5537dea7968ae7f6c22ed7f1d5f416638fd81c83caa47`
- `tap_git_head`: `39ef41c33e47822bd514eba5d76d2dcc0dd439de` (homebrew-core commit that produced this formula state)
- `ruby_source_path`: `Formula/p/podman.rb`, `ruby_source_checksum.sha256`: `717431c862f9fef22522d8cf6723e5524e2c9b14beebc7ffd71c79a44f8734ed`

**Bottles are content-addressable:** each bottle's download URL literally contains its own sha256 digest, and Homebrew verifies it against the formula's recorded sha256 on install. So a bottle install is hash-verified end-to-end.

---

## 2. Pinning approaches evaluated

### 2a. `brew pin` — does NOT do what "pin a version" implies
From `brew(1)` Manpage:
> `pin` … "Pin the specified package, preventing it from being upgraded when issuing the `brew upgrade` command."

- `brew pin` only operates on an **already-installed** formula and only **prevents `brew upgrade`** from touching it. It does **not** let you choose or install an arbitrary older version.
- Confirmed: this is not a mechanism to *select* a version. It's a "don't upgrade this" lock, and it doesn't protect against `brew install` itself upgrading (`$HOMEBREW_NO_INSTALL_UPGRADE` note) or a moved formula. **Not a reproducibility tool for our purpose.**

### 2b. Versioned formula `brew install podman@6.0.1` — DOES NOT EXIST
`"versioned_formulae": []` in the JSON. Homebrew-core does not ship `podman@N` formulae (contrast with `node@20`, `python@3.12`, etc.). So this route is unavailable.

### 2c. `brew extract` — the documented "install a specific version" mechanism
From `brew(1)` Manpage:
> `extract [--version=] [--git-revision=] [--force] formula tap`
> "Look through repository history to find the most recent version of *formula* and create a copy in *tap*. …the command will create the new formula file at *tap*`/Formula/`*formula*`@`*version*`.rb`."
> `--version` "Extract the specified *version* of *formula* instead of the most recent."
> `--git-revision` "Search for the specified *version* of *formula* starting at *revision* instead of HEAD."

- `brew extract podman <tap> --version=6.0.1` walks homebrew-core git history, finds the commit where podman was 6.0.1, and writes a standalone `<tap>/Formula/podman@6.0.1.rb` into a tap you own. You then `brew install <tap>/podman@6.0.1`.
- This is the officially documented way to install/keep a specific historical version.
- **Determinism caveat:** the extracted `.rb` reproduces that version's source URL + checksum, but the resulting install may **build from source** if no matching bottle is poured for the extracted `@version` formula name (bottles are keyed to the original `podman` formula, not `podman@6.0.1`). Since 6.0.1 is *current*, extract will produce a formula identical to today's, but the extracted-formula path can miss the bottle and compile from source (slow: pulls `go`, `rust`, `protobuf`, autotools as build deps). Verify with `brew install --dry-run` whether it pours a bottle.

### 2d. Install from a pinned homebrew-core commit / raw .rb URL
- You can `git clone` homebrew-core, `git checkout 39ef41c33e47822bd514eba5d76d2dcc0dd439de` (the `tap_git_head`), and `brew install ./Formula/p/podman.rb`, or fetch the raw `podman.rb` at that SHA. Pinning to the SHA makes the formula file deterministic.
- Same bottle-vs-source caveat: installing from a local `.rb` file may not pour the official bottle (Homebrew is picky about matching a bottle to a tapped formula). Often ends up building from source.

### 2e. `HOMEBREW_NO_AUTO_UPDATE` + verifying the bottle digest
- Since 6.0.1 is the current stable **right now**, the simplest deterministic install today is: `HOMEBREW_NO_AUTO_UPDATE=1 brew install podman` after pinning the homebrew-core checkout to a known SHA. The bottle is pulled from GHCR by digest (`x86_64_linux` = `ce7ce8…8f64`) and hash-verified.
- The reproducibility hinge is: **which homebrew-core commit `brew` has checked out at build time.** If you don't pin that, a later `brew` update will move podman past 6.0.1 and a fresh `docker build` yields a different version — this is the floating-range failure AGENTS.md forbids.

---

## 3. Most deterministic Dockerfile approach

The real determinism problem with Linuxbrew is that `brew install podman` resolves against **whatever homebrew-core commit is checked out**, which drifts. To make the build reproducible you must pin *that*. Two viable strategies:

### Strategy A (recommended while 6.0.1 == current stable): pin the homebrew-core commit, install the bottle
```dockerfile
# base: ubuntu:26.04, with build-essential, procps, curl, file, git, ca-certificates
# create non-root user 'linuxbrew' (brew refuses to run as root — see §4)
USER linuxbrew
ENV HOMEBREW_NO_AUTO_UPDATE=1 \
    HOMEBREW_NO_ANALYTICS=1 \
    HOMEBREW_NO_INSTALL_UPGRADE=1 \
    HOMEBREW_NO_ENV_HINTS=1
# install brew (NONINTERACTIVE=1), then pin homebrew-core to the commit that has podman 6.0.1:
RUN cd "$(brew --repository homebrew/core)" \
    && git fetch --depth=1 origin 39ef41c33e47822bd514eba5d76d2dcc0dd439de \
    && git checkout 39ef41c33e47822bd514eba5d76d2dcc0dd439de
RUN brew install podman
# verify the exact version — fail the build if it drifts:
RUN test "$(podman --version)" = "podman version 6.0.1"
```
Notes:
- Pinning homebrew-core to `39ef41c33e47822bd514eba5d76d2dcc0dd439de` freezes the formula so a rebuilt image resolves podman 6.0.1 regardless of upstream moving on.
- With that commit checked out and auto-update off, `brew install podman` pours the **official bottle** (`ce7ce8…8f64`), which is hash-verified from GHCR — fast, no compile.
- The `test` line is a cheap deterministic guard (AGENTS.md-friendly): the build fails loudly if the version isn't exactly 6.0.1.
- Homebrew-core is a huge shallow-history repo; you may need `git fetch --unshallow` or a full clone for the target SHA to be reachable. In CI, cloning full homebrew-core is heavy (~GB); consider `git clone --filter=blob:none` or fetching the single commit.

### Strategy B (needed once upstream moves past 6.0.1): `brew extract`
```dockerfile
USER linuxbrew
RUN brew tap-new local/pin --no-git || true
RUN brew extract --version=6.0.1 podman local/pin
RUN brew install local/pin/podman@6.0.1
RUN test "$(podman --version)" = "podman version 6.0.1"
```
- This retrieves 6.0.1 from history even after homebrew-core advances. **But** the extracted `podman@6.0.1` formula frequently **builds from source** (no bottle keyed to that name), pulling `go`/`rust`/`protobuf`/autotools — slow and itself less reproducible (compiler/toolchain drift). Verify with `--dry-run`; if it insists on source, Strategy A (pinned SHA + official bottle) is strictly better for determinism *and* speed.

### Rejected: `brew install podman` + `brew pin`
Not reproducible. `brew pin` doesn't select a version and doesn't stop the initial `brew install` from grabbing whatever the current formula says. A rebuild after upstream bumps podman produces a different version. **Flagged as non-deterministic — do not use for the pin requirement.**

---

## 4. Cost / footprint & root gotchas

- **brew refuses to run as root.** Homebrew hard-errors ("Don't run this as root!") when `whoami == root`. Standard Dockerfile pattern: create a `linuxbrew` user + group, `chown` `/home/linuxbrew/.linuxbrew`, `USER linuxbrew`, and add `eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"` to the environment/PATH. Anything needing root (apt build deps) is done as root *before* switching user.
- **Prerequisites (apt):** Linuxbrew needs `build-essential`, `procps`, `curl`, `file`, `git`, `ca-certificates` installed via apt as root first.
- **Footprint:** a full brew prefix under `/home/linuxbrew/.linuxbrew` plus its cloned taps (homebrew-core git history is the bulk — hundreds of MB to ~1GB depending on shallow/full). podman + its Linux deps (`conmon`, `crun`, `fuse-overlayfs`, `gpgme`, `passt`, `sqlite`, `systemd`, `libseccomp`) add more. Expect the brew layer to add **several hundred MB to ~1GB+** to the image. Mitigate with `brew cleanup -s`, `rm -rf "$(brew --cache)"`, and shallow/partial clones — but note aggressive homebrew-core pruning can conflict with the pinned-SHA approach.
- **This is materially heavier** than installing podman from Ubuntu's own apt repo or the upstream `.deb`/static build.

---

## 5. Version availability summary

- **podman 6.0.1 is the current homebrew-core stable as of 2026-07-16** (`versions.stable: 6.0.1`, revision 1). So the desired exact version is available *right now* both as source and as a hash-verified Linux bottle.
- No `podman@6.0.x` versioned formula exists (`versioned_formulae: []`).
- If/when homebrew-core moves past 6.0.1, `brew extract --version=6.0.1 podman <tap>` can still retrieve it from git history (documented), at the cost of likely building from source.

---

## Recommendation

**Yes — Linuxbrew *can* deterministically pin podman to an exact version, but only if you also pin the homebrew-core checkout; `brew` alone does not give you a version selector, and `brew pin` is the wrong tool.**

- **Best method (today, 6.0.1 = current):** Strategy A — pin homebrew-core to commit `39ef41c33e47822bd514eba5d76d2dcc0dd439de`, `HOMEBREW_NO_AUTO_UPDATE=1 brew install podman` (pulls the official hash-verified Linux bottle `sha256:ce7ce8f97c5a5055c2e0e95000585589bb029d8a23e3b79d56422a6037458f64` on x86_64), and add a `podman --version` == `6.0.1` build assertion.
- **Fallback (after upstream advances):** Strategy B — `brew extract --version=6.0.1`; accept it may build from source.
- **Honest caveat:** Linuxbrew is a heavy, moving dependency and gives *conditional* determinism (only as reproducible as the pinned homebrew-core SHA + a still-hosted GHCR bottle). If the only goal is "exact, reproducible podman in a Dockerfile," **the upstream podman static binary / official `.deb` pinned by version+sha256 is simpler and more deterministic** than dragging in a full brew prefix. Prefer Homebrew here mainly if GSA-standard tooling parity (same `brew` UX on Mac and Linux) is an explicit requirement that outweighs the footprint and the SHA-pinning complexity.

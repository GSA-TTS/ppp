# syntax=docker/dockerfile:1.7
#
# ppp (Podman Plus Proxy) dev container — ticket #15 / T2.
#
# A version-controlled, blast-radius-limited environment for humans and agents
# to do the SANDBOX-SAFE core work: Go build/test, the Python mitmproxy addon,
# `mitmdump` config-emission (T5's golden WireGuard client-config fixture), and
# `podman --help` / `podman machine --help` introspection.
#
# SCOPE BOUNDARY: this image does NOT (and cannot) boot Podman Machine microVMs
# — nested VMs won't run in a container. The seam-8 host e2e (#27) and any real
# VM lifecycle are host-only. Only the FULL podman *client* CLI is installed
# here so core tickets can probe it (`podman machine --help` works).
#
# Every tool version is PINNED EXACTLY (AGENTS.md: no floating ranges) and MUST
# stay in sync with the Makefile pins and CI:
#   Ubuntu        26.04 LTS  (base image, "Resolute Raccoon")
#   Go            1.26       (apt: golang-go, candidate 2:1.26~1)
#   Python        3.14       (apt: python3, candidate 3.14.x)
#   podman        6.0.1      (FULL client via Homebrew-on-Linux, pinned bottle)
#   goimports     v0.48.0    -> Makefile GOIMPORTS_VERSION
#   golangci-lint v2.12.2    -> Makefile GOLANGCI_LINT_VERSION (v2 schema)
#   govulncheck   v1.1.4     -> SCA (#29); go>=1.22 compatible
#   mitmproxy     12.2.3     -> ADR-0003/spec pin (WG client-config format)
#   ruff          0.15.22    -> Python addon lint (#24)
#   gitleaks      8.30.1     -> pre-commit secrets scan
#   gh            2.96.0     -> issue-tracker ops
#   pre-commit    4.6.0      -> runs contract probe + gitleaks hooks
#
# Contract provisioning = OPTION A (build-time). The universal behavioral
# contract lives in the PRIVATE GSA-TTS/agentic-coding-playbook repo, so the
# probe's unauthenticated auto-fetch cannot reach it. We fetch it at BUILD TIME
# with an AUTHENTICATED GitHub Contents API call (mirroring
# .github/workflows/contract-check.yml) into the dev user's
# ~/.agentic-coding-playbook, then the probe passes offline via the home path.
# The token is supplied as a BuildKit --mount=type=secret (id=playbook_token)
# and is NEVER written to a layer, ARG, or ENV.

FROM ubuntu:26.04

# --- Pinned tool versions (keep in sync with Makefile / CI / ticket #15) ------
ARG GOIMPORTS_VERSION=v0.48.0
ARG GOLANGCI_LINT_VERSION=v2.12.2
ARG GOVULNCHECK_VERSION=v1.1.4
ARG MITMPROXY_VERSION=12.2.3
ARG PODMAN_VERSION=6.0.1
ARG RUFF_VERSION=0.15.22
ARG GITLEAKS_VERSION=8.30.1
ARG GH_VERSION=2.96.0
ARG PRECOMMIT_VERSION=4.6.0
# Pinned universal-contract release tag (matches CI's PLAYBOOK_REF).
ARG PLAYBOOK_REF=v0.14.0
# homebrew-core commit where the podman formula == 6.0.1 (pins the bottle).
ARG HOMEBREW_CORE_PIN=4178c640d927c76f8e92584a8867e9fc158cf087

# Non-root dev user (uid/gid 1000) — declared early so paths resolve below.
ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=1000

# The vendored Zscaler root cert's SHA-256 fingerprint (of the DER form) is
# re-verified at build time (hardcoded in the trust step below); the build fails
# closed on mismatch. It is the generic PUBLIC Zscaler Root CA (2014 gen) that
# GSA-TTS itself vendors.

# Homebrew brings up its own tooling; keep it non-interactive and reproducible.
# HOMEBREW_NO_AUTO_UPDATE + HOMEBREW_NO_INSTALL_FROM_API force brew to use the
# pinned, checked-out homebrew-core tap (not the live JSON API) so `brew install
# podman` resolves the exact pinned formula and pulls its hash-verified bottle.
ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    GOTOOLCHAIN=local \
    GOPATH=/go \
    GOFLAGS=-buildvcs=false \
    HOMEBREW_PREFIX=/home/linuxbrew/.linuxbrew \
    HOMEBREW_NO_AUTO_UPDATE=1 \
    HOMEBREW_NO_INSTALL_FROM_API=1 \
    HOMEBREW_NO_ANALYTICS=1 \
    HOMEBREW_NO_ENV_HINTS=1 \
    PATH=/home/linuxbrew/.linuxbrew/bin:/home/linuxbrew/.linuxbrew/sbin:/usr/local/go/bin:/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin

# --- OS packages (single layer) -----------------------------------------------
# Go 1.26 and Python 3.14 both ship in the Ubuntu 26.04 archive (no uv, no
# separate CPython). Plus build essentials, git/make/curl, gh + brew build
# prerequisites (git, curl, procps, file), sudo, and CA machinery.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        curl \
        file \
        git \
        golang-go \
        gnupg \
        make \
        procps \
        python3 \
        python3-pip \
        python3-venv \
        sudo \
        xz-utils; \
    rm -rf /var/lib/apt/lists/*; \
    go version | grep -qE 'go1\.(2[6-9]|[3-9][0-9])'; \
    python3 --version

# --- Zscaler CA (vendored, verified, unconditionally trusted) -----------------
# WHY VENDOR A PUBLIC INTERCEPTION ROOT: GSA developer machines sit behind a
# Zscaler TLS-inspecting proxy that re-signs TLS with this root. Trusting it here
# means brew/curl/go/python/pip all work on GSA networks with ZERO user action.
# It is the generic PUBLIC Zscaler Root CA (2014 gen, valid to 2042) that
# GSA-TTS itself vendors — deliberately public and safe: off a Zscaler network
# the root is simply never exercised (no traffic is signed by it). We re-verify
# its DER SHA-256 fingerprint at build time and FAIL CLOSED on any mismatch.
COPY .devcontainer/certs/ZscalerRootCA.crt /usr/local/share/ca-certificates/ZscalerRootCA.crt
RUN set -eux; \
    expected="04f61f1d13aae1d16573dc2c37f796fdf4ac97713a6959ebb11d2473958b1a53"; \
    actual="$(openssl x509 -in /usr/local/share/ca-certificates/ZscalerRootCA.crt -outform DER | sha256sum | cut -d' ' -f1)"; \
    if [ "$actual" != "$expected" ]; then \
        echo "ERROR: Zscaler CA fingerprint mismatch." >&2; \
        echo "  expected: $expected" >&2; \
        echo "  actual:   $actual" >&2; \
        exit 1; \
    fi; \
    update-ca-certificates; \
    test -f /etc/ssl/certs/ca-certificates.crt

# Point every toolchain's CA lookup at the system trust bundle so the Zscaler
# root (folded into it above) is honored uniformly by curl, git, go, python,
# pip, node, and brew. Inert off a Zscaler network.
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt \
    SSL_CERT_DIR=/etc/ssl/certs \
    GIT_SSL_CAINFO=/etc/ssl/certs/ca-certificates.crt \
    REQUESTS_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt \
    NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt \
    CURL_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt \
    HOMEBREW_SYSTEM_CA_CERTIFICATES_TRUST=1

# --- Non-root dev user + linuxbrew owner --------------------------------------
# Homebrew refuses to run as root, so we install it as a dedicated `linuxbrew`
# user. `vscode` (uid/gid 1000) is the interactive dev user with passwordless
# sudo. Both share the brew prefix on PATH (set in ENV above).
#
# Ubuntu 26.04 ships a stock `ubuntu` user at uid/gid 1000; remove it so we can
# claim 1000 for `vscode` (the conventional devcontainer uid).
RUN set -eux; \
    if getent passwd ${USER_UID} >/dev/null; then \
        userdel -r "$(getent passwd ${USER_UID} | cut -d: -f1)" 2>/dev/null || true; \
    fi; \
    if getent group ${USER_GID} >/dev/null; then \
        groupdel "$(getent group ${USER_GID} | cut -d: -f1)" 2>/dev/null || true; \
    fi; \
    groupadd --gid ${USER_GID} ${USERNAME}; \
    useradd --uid ${USER_UID} --gid ${USER_GID} --create-home --shell /bin/bash ${USERNAME}; \
    echo "${USERNAME} ALL=(root) NOPASSWD:ALL" > /etc/sudoers.d/${USERNAME}; \
    chmod 0440 /etc/sudoers.d/${USERNAME}; \
    useradd --create-home --shell /bin/bash linuxbrew; \
    echo "linuxbrew ALL=(root) NOPASSWD:ALL" > /etc/sudoers.d/linuxbrew; \
    chmod 0440 /etc/sudoers.d/linuxbrew

# --- Homebrew-on-Linux + podman 6.0.1 (FULL client, pinned bottle) ------------
# Install Homebrew as the `linuxbrew` user via the official installer, then pin
# reproducibly: fetch + checkout the homebrew-core commit where the podman
# formula == 6.0.1, and with HOMEBREW_NO_INSTALL_FROM_API=1 (set in ENV) brew
# resolves against that checked-out tap and installs the HASH-VERIFIED bottle
# (no compile, ~1 min). We then ASSERT `podman --version` == 6.0.1 and confirm
# `podman machine --help` runs. World-read the prefix so `vscode` can use it.
#
# SOURCE-BUILD FALLBACK (documented, not implemented): if the bottle ever
# becomes unavailable, build podman from source at tag v6.0.1 (`make podman`
# in a clone of podman-container-tools/podman; needs CGO + ~9 -dev headers,
# ~2.5-6 min). See .devcontainer/README.md.
RUN set -eux; \
    sudo -u linuxbrew NONINTERACTIVE=1 CI=1 \
        /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"; \
    brew_env="HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_FROM_API=1 HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1"; \
    core_tap="$(sudo -u linuxbrew env $brew_env ${HOMEBREW_PREFIX}/bin/brew --repository)/Library/Taps/homebrew/homebrew-core"; \
    if [ ! -d "$core_tap" ]; then \
        sudo -u linuxbrew env $brew_env ${HOMEBREW_PREFIX}/bin/brew tap --force homebrew/core; \
    fi; \
    sudo -u linuxbrew git -C "$core_tap" fetch --depth 1 origin "${HOMEBREW_CORE_PIN}"; \
    sudo -u linuxbrew git -C "$core_tap" checkout "${HOMEBREW_CORE_PIN}"; \
    sudo -u linuxbrew env $brew_env ${HOMEBREW_PREFIX}/bin/brew install podman; \
    sudo -u linuxbrew env $brew_env ${HOMEBREW_PREFIX}/bin/brew cleanup -s || true; \
    chmod 0755 /home/linuxbrew; \
    chmod -R a+rX "${HOMEBREW_PREFIX}"; \
    podman --version | grep -q "${PODMAN_VERSION}"; \
    podman machine --help >/dev/null

# --- Go module/build caches + GOPATH bin, writable by the dev user ------------
RUN set -eux; \
    mkdir -p /go/pkg /go/bin; \
    chown -R ${USER_UID}:${USER_GID} /go

# --- Go tools: goimports + golangci-lint + govulncheck ------------------------
# Installed into the shared /go/bin (GOPATH bin) so they are on PATH for all
# users. Versions must match the Makefile pins exactly. GOTOOLCHAIN=local keeps
# the apt Go 1.26 in charge (no surprise toolchain download); these pinned tools
# build cleanly under Go 1.26.
RUN set -eux; \
    go install golang.org/x/tools/cmd/goimports@${GOIMPORTS_VERSION}; \
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}; \
    go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}; \
    test -x /go/bin/goimports; \
    test -x /go/bin/govulncheck; \
    golangci-lint version | grep -q "$(printf '%s' "${GOLANGCI_LINT_VERSION}" | sed 's/^v//')"; \
    chmod -R a+rX /go/bin; \
    chown -R ${USER_UID}:${USER_GID} /go

# --- Python tooling in a dedicated venv: mitmproxy, ruff, pre-commit ----------
# Ubuntu 26.04 ships Python 3.14 (>= mitmproxy's 3.12 floor). We build the venv
# from the SYSTEM interpreter (/usr/bin/python3.14) explicitly — brew is earlier
# on PATH and brings its own python@3.14 (living under the linuxbrew-owned prefix
# whose home dir is not world-executable), so a bare `python3` would seed the
# venv from a python the `vscode` user cannot execute. Placing the venv first on
# PATH makes `mitmdump`, `ruff`, `pre-commit` resolve to these pinned versions.
# mitmproxy exposes the `mitmdump` entrypoint needed for T5's golden fixture.
# The venv is world-readable so `vscode` inherits it.
ENV VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:/home/linuxbrew/.linuxbrew/bin:/home/linuxbrew/.linuxbrew/sbin:/usr/local/go/bin:/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin
RUN set -eux; \
    /usr/bin/python3 -m venv --copies /opt/venv; \
    /opt/venv/bin/pip install --no-cache-dir --upgrade pip; \
    /opt/venv/bin/pip install --no-cache-dir \
        "mitmproxy==${MITMPROXY_VERSION}" \
        "ruff==${RUFF_VERSION}" \
        "pre-commit==${PRECOMMIT_VERSION}"; \
    mitmdump --version | grep -q "${MITMPROXY_VERSION}"; \
    ruff --version | grep -q "${RUFF_VERSION}"; \
    pre-commit --version | grep -q "${PRECOMMIT_VERSION}"; \
    chmod -R a+rX /opt/venv

# --- gh CLI (2.96.0), pinned + SHA256-verified --------------------------------
RUN set -eux; \
    case "$(dpkg --print-architecture)" in \
        amd64) GH_ARCH=amd64 ;; \
        arm64) GH_ARCH=arm64 ;; \
        *) echo "unsupported arch for gh: $(dpkg --print-architecture)" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${GH_ARCH}.tar.gz" -o /tmp/gh.tgz; \
    curl -fsSL "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_checksums.txt" -o /tmp/gh_checksums.txt; \
    grep "gh_${GH_VERSION}_linux_${GH_ARCH}.tar.gz" /tmp/gh_checksums.txt | sed "s#gh_${GH_VERSION}_linux_${GH_ARCH}.tar.gz#/tmp/gh.tgz#" | sha256sum -c -; \
    tar -xzf /tmp/gh.tgz -C /tmp; \
    install -m 0755 "/tmp/gh_${GH_VERSION}_linux_${GH_ARCH}/bin/gh" /usr/local/bin/gh; \
    rm -rf /tmp/gh.tgz /tmp/gh_checksums.txt "/tmp/gh_${GH_VERSION}_linux_${GH_ARCH}"; \
    gh --version | grep -q "${GH_VERSION}"

# --- gitleaks (8.30.1), pinned + SHA256-verified ------------------------------
RUN set -eux; \
    case "$(dpkg --print-architecture)" in \
        amd64) GITLEAKS_ARCH=x64 ;; \
        arm64) GITLEAKS_ARCH=arm64 ;; \
        *) echo "unsupported arch for gitleaks: $(dpkg --print-architecture)" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_${GITLEAKS_ARCH}.tar.gz" -o /tmp/gitleaks.tgz; \
    curl -fsSL "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_checksums.txt" -o /tmp/gitleaks_checksums.txt; \
    grep "gitleaks_${GITLEAKS_VERSION}_linux_${GITLEAKS_ARCH}.tar.gz" /tmp/gitleaks_checksums.txt | sed "s#gitleaks_${GITLEAKS_VERSION}_linux_${GITLEAKS_ARCH}.tar.gz#/tmp/gitleaks.tgz#" | sha256sum -c -; \
    tar -xzf /tmp/gitleaks.tgz -C /tmp gitleaks; \
    install -m 0755 /tmp/gitleaks /usr/local/bin/gitleaks; \
    rm -f /tmp/gitleaks.tgz /tmp/gitleaks_checksums.txt /tmp/gitleaks; \
    gitleaks version | grep -q "${GITLEAKS_VERSION}"

# --- Build-time universal-contract provisioning (authenticated, secret mount) --
# Mirrors .github/workflows/contract-check.yml: authenticated GitHub Contents
# API fetch at the pinned PLAYBOOK_REF into the CONVENTIONAL HOME PATH for the
# non-root dev user. The token is mounted as a BuildKit secret (id=playbook_token)
# and is only readable for the duration of this RUN — it is NEVER persisted in
# any layer, ARG, or ENV. Fails closed with a clear message if the secret is
# absent or the fetched file is not the canonical universal contract.
#
#   BuildKit:     docker build --secret id=playbook_token,env=PLAYBOOK_TOKEN ...
#   devcontainer: passed via devcontainer.json build.options (see that file).
RUN --mount=type=secret,id=playbook_token,required=false \
    set -eu; \
    SECRET_FILE=/run/secrets/playbook_token; \
    if [ ! -s "$SECRET_FILE" ]; then \
        echo "" >&2; \
        echo "ERROR: build secret 'playbook_token' is missing or empty." >&2; \
        echo "The universal behavioral contract (private GSA-TTS/agentic-coding-playbook)" >&2; \
        echo "MUST be provisioned at build time. Provide a token with read access:" >&2; \
        echo "" >&2; \
        echo "  export PLAYBOOK_TOKEN=\$(gh auth token)   # or a PAT with repo:read" >&2; \
        echo "  docker build --secret id=playbook_token,env=PLAYBOOK_TOKEN ..." >&2; \
        echo "" >&2; \
        echo "See .devcontainer/README.md for the full instructions." >&2; \
        exit 1; \
    fi; \
    PLAYBOOK_TOKEN="$(cat "$SECRET_FILE")"; \
    mkdir -p /home/${USERNAME}/.agentic-coding-playbook; \
    curl -fsSL \
        -H "Authorization: Bearer ${PLAYBOOK_TOKEN}" \
        -H "Accept: application/vnd.github.raw+json" \
        -H "X-GitHub-Api-Version: 2022-11-28" \
        "https://api.github.com/repos/GSA-TTS/agentic-coding-playbook/contents/AGENTS.md?ref=${PLAYBOOK_REF}" \
        -o /home/${USERNAME}/.agentic-coding-playbook/AGENTS.md; \
    grep -q 'role: universal' /home/${USERNAME}/.agentic-coding-playbook/AGENTS.md \
        || { echo "ERROR: fetched file is not the universal contract (missing 'role: universal')." >&2; exit 1; }; \
    chown -R ${USER_UID}:${USER_GID} /home/${USERNAME}/.agentic-coding-playbook

USER ${USERNAME}
WORKDIR /workspaces/ppp

# Sanity: the probe passes offline via the home path once the contract is
# provisioned above. The real gate runs via `make check` / pre-commit at dev
# time (identical in-container and on host).

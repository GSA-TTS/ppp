# ppp opencode agent image (spec §5.7). Runs INSIDE the sandbox VM as a
# container (`podman run` in the guest), so the agent shares the VM's kernel but
# the VM has its own kernel separate from the host — two-layer isolation.
#
# Credentials are NEVER baked in and NEVER passed as env: the host-side mitmproxy
# addon injects provider auth headers on outbound requests (wayfinder #11), so
# the agent only ever holds placeholders. No *_PROXY env is set — egress is
# transparently tunneled via WireGuard, not an explicit proxy.
FROM ubuntu:24.04

# Node.js LTS (opencode is distributed via npm), git + openssh for repo work,
# ca-certificates so the mitmproxy CA (imported into the guest trust store) is
# honored by Node/curl. build-essential is intentionally omitted to keep the
# image lean; add per-kit if a project needs a toolchain.
ARG NODE_MAJOR=22
RUN set -eux; \
    export DEBIAN_FRONTEND=noninteractive; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates curl git openssh-client gnupg; \
    mkdir -p /etc/apt/keyrings; \
    curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
        | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg; \
    echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_${NODE_MAJOR}.x nodistro main" \
        > /etc/apt/sources.list.d/nodesource.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends nodejs; \
    npm install -g opencode-ai; \
    apt-get purge -y gnupg; \
    apt-get autoremove -y; \
    rm -rf /var/lib/apt/lists/*

# Sudoless non-root user; the agent runs unprivileged inside the container.
RUN useradd --create-home --shell /bin/bash ppp
USER ppp
WORKDIR /workspace

ENV OPENCODE_SANDBOX=1

# The agent is launched explicitly by ppp (headless `opencode run "<prompt>"` or
# interactive `opencode`), so no default CMD that would auto-start it.
CMD ["bash"]

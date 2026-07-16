#!/usr/bin/env bash
# postCreateCommand for the ppp dev container (ticket #15 / T2).
#
# Runs once after the container is created. Installs the pre-commit git hooks
# (so the fail-closed contract + gitleaks gate is identical in-container and on
# host) and prints how to verify the environment.
set -euo pipefail

echo ">> installing pre-commit git hooks"
pre-commit install

cat <<'EOF'

============================================================================
 ppp dev container ready.

 Verify the environment (all should succeed inside the container):

   make setup                 # install/pin Go tooling + contract probe
   make check                 # fmt + vet + lint + test + contract probe
   podman --version           # -> 6.0.1 (FULL client; microVMs are host-only)
   podman machine --help      # runs (introspection only)
   mitmdump --version         # -> 12.2.3
   goimports -h               # v0.48.0 (Makefile GOIMPORTS_VERSION)
   golangci-lint version      # -> v2.12.2 (Makefile GOLANGCI_LINT_VERSION)
   govulncheck -version       # -> v1.1.4 (SCA / #29)
   ruff --version             # -> 0.15.22
   gh --version               # -> 2.96.0
   pre-commit --version       # -> 4.6.0
   ./scripts/ensure-contract.sh   # contract probe -> exit 0 (home path)
   pre-commit run --all-files # contract probe + gitleaks

 SCOPE BOUNDARY: this container runs sandbox-safe core work only. It does
 NOT boot Podman Machine microVMs and does NOT run the seam-8 host e2e
 (#26/#27) — those are host-only. Only the podman *client* is present.
============================================================================
EOF

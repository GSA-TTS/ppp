# ppp (Podman Plus Proxy) — developer Makefile.
#
# One-command bootstrap (`make setup`) and one-command verify (`make check`)
# per AGENTS.md "Engineering Discipline". Both run the universal behavioral
# contract probe; `make check` fails closed if the contract is unavailable.

GO       ?= go
BIN_DIR  := bin
BINARY   := $(BIN_DIR)/ppp
CONTRACT := ./scripts/ensure-contract.sh

# Pinned tool versions live in ONE place: versions.env (AGENTS.md: no floating
# ranges). The Makefile, .devcontainer, and CI all read from it, so a bump is a
# one-line edit there. See versions.env for the full set + rationale.
include versions.env

# GOBIN falls back to $(go env GOPATH)/bin so `make setup` can find installed tools.
GOBIN := $(shell $(GO) env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif

.PHONY: all setup check version-sync build test test-addon test-e2e vet fmt-check lint contract clean

all: check

## setup: install/pin dev tooling and verify the behavioral contract.
setup:
	@echo ">> installing goimports ($(GOIMPORTS_VERSION))"
	$(GO) install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)
	@echo ">> ensuring golangci-lint ($(GOLANGCI_LINT_VERSION))"
	@if command -v golangci-lint >/dev/null 2>&1 && golangci-lint version 2>/dev/null | grep -q "$(patsubst v%,%,$(GOLANGCI_LINT_VERSION))"; then \
		echo "   golangci-lint $(GOLANGCI_LINT_VERSION) already installed"; \
	else \
		echo "   installing golangci-lint $(GOLANGCI_LINT_VERSION)"; \
		$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) || \
			echo "   WARNING: golangci-lint install failed; install $(GOLANGCI_LINT_VERSION) manually (https://golangci-lint.run)"; \
	fi
	@echo ">> checking for ruff (Python addon lint; optional at this stage)"
	@if command -v ruff >/dev/null 2>&1; then \
		echo "   ruff already installed: $$(ruff --version)"; \
	else \
		echo "   WARNING: ruff not installed (needed later for the mitmproxy addon; see https://docs.astral.sh/ruff)"; \
	fi
	@echo ">> verifying behavioral contract"
	$(CONTRACT)
	@echo ">> setup complete"

## check: format, vet, lint, test, and verify the behavioral contract.
check: version-sync fmt-check vet lint test contract
	@echo ">> check complete"

## version-sync: fail if go.mod's go directive drifts from versions.env GO_VERSION.
version-sync:
	@echo ">> version-sync (go.mod vs versions.env)"
	@modver=$$(awk '/^go /{print $$2}' go.mod); \
	want=$$(awk -F= '/^GO_VERSION=/{print $$2}' versions.env); \
	case "$$modver" in \
		$$want|$$want.*) : ;; \
		*) echo "   go.mod 'go $$modver' != versions.env GO_VERSION=$$want"; exit 1 ;; \
	esac

## fmt-check: fail if any Go source is not gofmt/goimports-clean.
fmt-check:
	@echo ">> gofmt check"
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "   the following files are not gofmt-formatted:"; \
		echo "$$unformatted"; \
		echo "   run 'gofmt -w .' (or 'goimports -w .') to fix"; \
		exit 1; \
	fi
	@if command -v goimports >/dev/null 2>&1 || [ -x "$(GOBIN)/goimports" ]; then \
		GOIMPORTS=$$(command -v goimports || echo "$(GOBIN)/goimports"); \
		echo ">> goimports check ($$GOIMPORTS)"; \
		misordered=$$($$GOIMPORTS -local github.com/GSA-TTS/ppp -l .); \
		if [ -n "$$misordered" ]; then \
			echo "   the following files have import issues:"; \
			echo "$$misordered"; \
			echo "   run 'goimports -local github.com/GSA-TTS/ppp -w .' to fix"; \
			exit 1; \
		fi; \
	else \
		echo ">> goimports not installed; skipping (run 'make setup')"; \
	fi

## vet: run go vet across all packages.
vet:
	@echo ">> go vet"
	$(GO) vet ./...

## lint: run golangci-lint if installed, otherwise warn and continue.
lint:
	@if command -v golangci-lint >/dev/null 2>&1 || [ -x "$(GOBIN)/golangci-lint" ]; then \
		LINT=$$(command -v golangci-lint || echo "$(GOBIN)/golangci-lint"); \
		echo ">> golangci-lint run ($$LINT)"; \
		$$LINT run; \
	else \
		echo ">> golangci-lint not installed; skipping (run 'make setup')"; \
	fi

## test: run the Go test suite (excludes the host-only e2e; see test-e2e).
test:
	@echo ">> go test"
	$(GO) test ./...

## test-e2e: the single host-only end-to-end test (T14). Requires a real host
## with podman + mitmdump 12.2.3 + network; creates and tears down a throwaway
## Podman Machine. Excluded from `make check`/CI/the dev container by the `e2e`
## build tag. Uses a dummy secret only. Point PPP_OPENCODE_IMAGE at a real agent
## image to exercise the agent too (defaults to a tiny public image).
test-e2e:
	@echo ">> go test -tags e2e ./test/e2e (host-only; ~minutes)"
	$(GO) test -tags e2e -timeout 20m -v ./test/e2e/

## test-addon: lint + test the embedded Python mitmproxy addon.
## Requires ruff + pytest with mitmproxy's runtime deps (ruamel.yaml) importable
## — all preinstalled in the dev container's /opt/venv. On the host, install
## them into the same environment first. conftest.py puts assets/ on sys.path.
## PY selects the addon interpreter: the dev-container venv if present, else the
## first python3 on PATH. Tools are run as `python -m` so they resolve against
## that interpreter's environment (where ruamel.yaml lives), not a stray PATH copy.
PY := $(shell [ -x /opt/venv/bin/python ] && echo /opt/venv/bin/python || command -v python3)
test-addon:
	@echo ">> ruff check (addon)"
	@if $(PY) -m ruff --version >/dev/null 2>&1; then \
		$(PY) -m ruff check assets tests/addon; \
	else \
		echo "   ruff not available for $(PY); skipping (use the dev container or 'pip install ruff==$(RUFF_VERSION)')"; \
	fi
	@echo ">> pytest (addon)"
	@if $(PY) -m pytest --version >/dev/null 2>&1; then \
		$(PY) -m pytest tests/addon; \
	else \
		echo "   pytest not available for $(PY); skipping (use the dev container or 'pip install pytest==$(PYTEST_VERSION)')"; \
	fi

## build: compile the ppp binary into $(BINARY).
build:
	@echo ">> building $(BINARY)"
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BINARY) ./cmd/ppp

## contract: verify the universal behavioral contract is present (fails closed).
contract:
	@echo ">> behavioral contract probe"
	$(CONTRACT)

## clean: remove build artifacts.
clean:
	rm -rf $(BIN_DIR)

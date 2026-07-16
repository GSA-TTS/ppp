# ppp (Podman Plus Proxy) — developer Makefile.
#
# One-command bootstrap (`make setup`) and one-command verify (`make check`)
# per AGENTS.md "Engineering Discipline". Both run the universal behavioral
# contract probe; `make check` fails closed if the contract is unavailable.

GO       ?= go
BIN_DIR  := bin
BINARY   := $(BIN_DIR)/ppp
CONTRACT := ./scripts/ensure-contract.sh

# Pinned dev-tool versions (AGENTS.md: no floating ranges). Keep these in sync
# with the versions installed in .devcontainer (ticket #15 / T2).
GOIMPORTS_VERSION    := v0.48.0
GOLANGCI_LINT_VERSION := v2.12.2

# GOBIN falls back to $(go env GOPATH)/bin so `make setup` can find installed tools.
GOBIN := $(shell $(GO) env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif

.PHONY: all setup check build test vet fmt-check lint contract clean

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
check: fmt-check vet lint test contract
	@echo ">> check complete"

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

## test: run the Go test suite.
test:
	@echo ">> go test"
	$(GO) test ./...

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

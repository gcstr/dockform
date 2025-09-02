## Makefile for dockform

SHELL := /bin/zsh

# Configurable variables
GO ?= go
PKGS := ./...
E2E_PKGS := ./test/e2e
MAIN := ./cmd/dockform
BIN  ?= dockform
COVER_OUT ?= cover.out
LINT ?= golangci-lint
GOLANGCI_LINT_PKG ?= github.com/golangci/golangci-lint/cmd/golangci-lint@latest

.DEFAULT_GOAL := help

.PHONY: help all build run install fmt vet lint deps tidy test coverage coverhtml ci clean e2e

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## / {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: fmt vet test build ## Format, vet, test, and build

build: ## Build the dockform binary
	$(GO) build -o $(BIN) $(MAIN)

run: ## Run the dockform CLI (pass ARGS="..." to forward args)
	$(GO) run $(MAIN) $(ARGS)

install: ## Install the dockform binary to GOPATH/bin
	$(GO) install $(MAIN)

fmt: ## Format code
	$(GO) fmt $(PKGS)

vet: ## Run go vet
	$(GO) vet $(PKGS)

lint: ## Run golangci-lint via go run
	$(GO) run $(GOLANGCI_LINT_PKG) run

deps: ## Download go module dependencies
	$(GO) mod download

tidy: ## Tidy go.mod/go.sum
	$(GO) mod tidy

test: vet ## Run tests with coverage
	$(GO) run gotest.tools/gotestsum@latest $(PKGS) -- -count=1 -coverprofile=$(COVER_OUT)

e2e: ## Run end-to-end tests
	$(GO) run gotest.tools/gotestsum@latest --format testname $(E2E_PKGS) -- -count=1 -v

coverage: ## Show coverage summary (requires cover.out)
	$(GO) run gotest.tools/gotestsum@latest -- -coverprofile=cover.out $(PKGS)

coverhtml: ## Generate HTML coverage report at cover.html
	$(GO) tool cover -html=$(COVER_OUT) -o cover.html

ci: ## Lint, vet, and test (mirror CI pipeline locally)
	$(MAKE) lint
	$(MAKE) vet
	$(MAKE) test
	$(MAKE) e2e

clean: ## Remove build artifacts
	rm -f $(BIN) $(COVER_OUT) cover.html


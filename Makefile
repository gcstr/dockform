## Makefile for dockform

SHELL := /bin/zsh

# Configurable variables
GO ?= go
PKGS := ./...
MAIN := ./cmd/dockform
BIN  ?= dockform
COVER_OUT ?= cover.out
LINT ?= golangci-lint

.DEFAULT_GOAL := help

.PHONY: help all build run install fmt vet lint deps tidy test coverage coverhtml ci clean e2e e2e-dood

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

lint: ## Run golangci-lint (auto-detect from PATH and Homebrew)
	@set -e; \
	if command -v $(LINT) >/dev/null 2>&1; then \
		LINTBIN="$(LINT)"; \
	elif [ -x /opt/homebrew/bin/golangci-lint ]; then \
		LINTBIN="/opt/homebrew/bin/golangci-lint"; \
	elif [ -x /usr/local/bin/golangci-lint ]; then \
		LINTBIN="/usr/local/bin/golangci-lint"; \
	elif command -v brew >/dev/null 2>&1 && [ -x "$$(brew --prefix 2>/dev/null)/bin/golangci-lint" ]; then \
		LINTBIN="$$(brew --prefix)/bin/golangci-lint"; \
	else \
		echo "golangci-lint not found. Install via Homebrew: brew install golangci-lint"; exit 1; \
	fi; \
	"$$LINTBIN" run

deps: ## Download go module dependencies
	$(GO) mod download

tidy: ## Tidy go.mod/go.sum
	$(GO) mod tidy

test: vet ## Run tests with coverage
	$(GO) test $(PKGS) -v -coverprofile=$(COVER_OUT)

e2e: ## Run end-to-end tests
	$(GO) test ./test/e2e -v -count=1

e2e-dood: ## Run e2e inside Docker (DooD)
	docker compose -f docker-compose.yml up --abort-on-container-exit
	docker compose -f docker-compose.yml down -v

coverage: ## Show coverage summary (requires cover.out)
	$(GO) tool cover -func=$(COVER_OUT)

coverhtml: ## Generate HTML coverage report at cover.html
	$(GO) tool cover -html=$(COVER_OUT) -o cover.html

ci: ## Lint, vet, and test (mirror CI pipeline locally)
	$(MAKE) lint
	$(MAKE) vet
	$(MAKE) test

clean: ## Remove build artifacts
	rm -f $(BIN) $(COVER_OUT) cover.html
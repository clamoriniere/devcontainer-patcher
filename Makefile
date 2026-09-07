# devcontainer-patcher Makefile

BINARY      := dcp
PKG         := github.com/clamoriniere/devcontainer-patcher
CMD_PKG     := $(PKG)/cmd
BIN_DIR     := bin
GOBIN       ?= $(shell go env GOPATH)/bin

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X $(CMD_PKG).version=$(VERSION) \
	-X $(CMD_PKG).commit=$(COMMIT) \
	-X $(CMD_PKG).date=$(DATE)

GO          ?= go
GOFLAGS     ?=

.DEFAULT_GOAL := build

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Sync go.mod / go.sum
	$(GO) mod tidy

.PHONY: deps
deps: ## Download module dependencies
	$(GO) mod download

.PHONY: build
build: ## Build the CLI into ./bin
	mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) .

.PHONY: install
install: ## Install the CLI into $(GOBIN)
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' .

.PHONY: test
test: ## Run unit tests
	$(GO) test $(GOFLAGS) ./...

.PHONY: test-race
test-race: ## Run tests with the race detector and coverage
	$(GO) test $(GOFLAGS) -race -coverprofile=coverage.out -covermode=atomic ./...

.PHONY: cover
cover: test-race ## Open the HTML coverage report
	$(GO) tool cover -html=coverage.out

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format the code
	$(GO) fmt ./...

.PHONY: lint
lint: ## Run golangci-lint if available
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed; running go vet instead"; \
		$(GO) vet ./...; \
	fi

.PHONY: check
check: fmt vet test ## Format, vet and test

.PHONY: run
run: ## Run the CLI (pass args with ARGS=...)
	$(GO) run . $(ARGS)

.PHONY: snapshot
snapshot: ## Dry-run the release pipeline with GoReleaser (SBOM needs syft; skipped here)
	goreleaser release --snapshot --clean --skip=sbom

.PHONY: release-check
release-check: ## Validate the GoReleaser config
	goreleaser check

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) dist coverage.out

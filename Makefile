# QuicBaaS Makefile
# High-performance distributed key-value database

# Use bash for shell commands (required for PIPESTATUS and other bash features)
SHELL := /bin/bash

.PHONY: help
.DEFAULT_GOAL := help

# Variables
APP_NAME := quicbaas
VERSION := $(shell cat VERSION 2>/dev/null | tr -d '\n' || git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GO_VERSION := 1.26.6

# Directories
BIN_DIR := bin
BUILD_DIR := build
DIST_DIR := dist
COVERAGE_DIR := coverage
CMD_DIR := cmd/$(APP_NAME)
DAEMON_CMD_DIR := cmd/$(APP_NAME)d
PKG_DIR := pkg
INTERNAL_DIR := internal
TEST_DIR := test
PROTO_DIR := api/proto

# Build flags with version injection
LDFLAGS := -X github.com/jeremyhahn/go-quicbaas/pkg/version.Version=$(VERSION) \
           -X github.com/jeremyhahn/go-quicbaas/pkg/version.BuildTime=$(BUILD_TIME) \
           -X github.com/jeremyhahn/go-quicbaas/pkg/version.GitCommit=$(GIT_COMMIT)

# Static build flags
STATIC_LDFLAGS := $(LDFLAGS) -s -w -extldflags '-static'

# Go commands (prefer PATH; allow override)
GOCMD ?= go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFMT ?= gofmt
GOLINT := $(shell go env GOPATH)/bin/golangci-lint
# Pinned per devops.md §9 and company task #122. $(GOBIN) is shared across
# checkouts on the build hosts, so an unpinned install in one tree silently
# changes every other tree's lint verdict.
GOLANGCI_VERSION := v2.12.2

# Benchmark configuration
BENCHTIME ?= 100ms

# ==============================================================================
# QuicBaaS Feature Build Configuration
# ==============================================================================
# Core features are always included:
#   KV Storage, Seal/Unseal, KMS, Secrets, RBAC, Backup, Metrics, gRPC
#
# Optional features default to enabled. Set to 0 to exclude from build.
#
# Example: make build WITH_VECTOR=0 WITH_RAG=0 WITH_MCP=0
# Example: make build-embedded (single-node, no cluster features)

# ------------------------------------------------------------------------------
# Distributed Features (default: enabled)
# ------------------------------------------------------------------------------
# Raft cluster mode (includes Discovery + Cluster DEK)
WITH_RAFT        ?= 1

# ------------------------------------------------------------------------------
# Data Models (default: enabled)
# ------------------------------------------------------------------------------
# Time series metrics (aggregations, retention, downsampling)
WITH_TIMESERIES  ?= 1
# Vector database (HNSW index)
WITH_VECTOR      ?= 1
# Retrieval Augmented Generation (requires VECTOR)
WITH_RAG         ?= 1

# ------------------------------------------------------------------------------
# API Servers (default: enabled, gRPC is core)
# ------------------------------------------------------------------------------
# REST/HTTP API
WITH_REST        ?= 1
# QUIC protocol
WITH_QUIC        ?= 1
# Model Context Protocol (LLM tools)
WITH_MCP         ?= 1

# ------------------------------------------------------------------------------
# WebSocket Messaging Protocols (default: enabled)
# ------------------------------------------------------------------------------
# These protocols run as subprotocols over the WebSocket transport layer.
# WebSocket and Echo subprotocol are always included (core transport + testing).
#
# MQTT 3.1.1/5.0 over WebSocket
WITH_MQTT        ?= 1
# AMQP 0-9-1 over WebSocket
WITH_AMQP        ?= 1
# STOMP 1.2 over WebSocket
WITH_STOMP       ?= 1

# ------------------------------------------------------------------------------
# Agent Orchestration (requires MCP)
# ------------------------------------------------------------------------------
# External Dependencies (default: disabled)
# ------------------------------------------------------------------------------
# L2 Redis cache (requires external Redis)
WITH_L2_REDIS    ?= 0

# ------------------------------------------------------------------------------
# KMS Backends (default: disabled, software backend always available)
# See docs/security/kms.md for details on each backend.
# ------------------------------------------------------------------------------
# AWS KMS (and S3 for object storage)
WITH_AWS         ?= 0
# GCP Cloud KMS (and GCS for object storage)
WITH_GCP         ?= 0
# Azure Key Vault (and Azure Blob for object storage)
WITH_AZURE       ?= 0
# HashiCorp Vault
WITH_VAULT       ?= 0
# PKCS#11 (HSM/smart cards) - requires CGO
WITH_PKCS11      ?= 0
# Quantum-safe cryptography (ML-DSA, ML-KEM)
WITH_QUANTUM     ?= 0

# ------------------------------------------------------------------------------
# Object Storage Backends (default: disabled for local builds, enabled for integration tests)
# Used by backup/restore system. See docs/backup.md for details.
# ------------------------------------------------------------------------------
# AWS S3
WITH_AWSS3       ?= 0
# GCP Cloud Storage
WITH_GCS         ?= 0
# Azure Blob Storage
WITH_AZUREBLOB   ?= 0
# MinIO (S3-compatible, commonly used for local testing)
WITH_MINIO       ?= 0

# ==============================================================================
# Dependency Validation
# ==============================================================================

# RAG requires VECTOR
ifeq ($(WITH_RAG),1)
  ifneq ($(WITH_VECTOR),1)
    $(error WITH_RAG=1 requires WITH_VECTOR=1)
  endif
endif

# ==============================================================================
# Build Tags - accumulate based on optional features
# ==============================================================================
# hashicorpmetrics: Use hashicorp/go-metrics instead of deprecated armon/go-metrics
#                   Required to avoid module path conflicts with memberlist
# local:            Local filesystem storage backend (always available, no external deps)
BUILD_TAGS := hashicorpmetrics local

# Distributed features
ifeq ($(WITH_RAFT),1)
  BUILD_TAGS += raft
endif

# Data model features
# quicbaas data-model features ride on qrdb engines; each feature tag is
# paired with qrdb's own engine tag (qrdb_ts / qrdb_vector) so the real
# engine is compiled in rather than qrdb's feature-disabled stub.
ifeq ($(WITH_TIMESERIES),1)
  BUILD_TAGS += timeseries qrdb_ts
endif
ifeq ($(WITH_VECTOR),1)
  BUILD_TAGS += vector qrdb_vector
endif
ifeq ($(WITH_RAG),1)
  BUILD_TAGS += rag qrdb_vector
endif

# API server features
ifeq ($(WITH_REST),1)
  BUILD_TAGS += rest
endif
ifeq ($(WITH_QUIC),1)
  BUILD_TAGS += quic
endif
ifeq ($(WITH_MCP),1)
  BUILD_TAGS += mcp
endif

# WebSocket Messaging Protocols (subprotocols over WebSocket transport)
ifeq ($(WITH_MQTT),1)
  BUILD_TAGS += quicbaas_mqtt
endif
ifeq ($(WITH_AMQP),1)
  BUILD_TAGS += quicbaas_amqp
endif
ifeq ($(WITH_STOMP),1)
  BUILD_TAGS += quicbaas_stomp
endif


# External dependencies
ifeq ($(WITH_L2_REDIS),1)
  BUILD_TAGS += l2_redis
endif

# KMS/XKMS backend tags (passed to go-xkms)
ifeq ($(WITH_AWS),1)
  BUILD_TAGS += awskms
endif
ifeq ($(WITH_GCP),1)
  BUILD_TAGS += gcpkms
endif
ifeq ($(WITH_AZURE),1)
  BUILD_TAGS += azurekv
endif
ifeq ($(WITH_VAULT),1)
  BUILD_TAGS += vault
endif
ifeq ($(WITH_PKCS11),1)
  BUILD_TAGS += pkcs11
endif
ifeq ($(WITH_QUANTUM),1)
  BUILD_TAGS += quantum
endif

# Object storage backend tags (for backup/restore)
ifeq ($(WITH_AWSS3),1)
  BUILD_TAGS += awss3
endif
ifeq ($(WITH_GCS),1)
  BUILD_TAGS += gcpstorage
endif
ifeq ($(WITH_AZUREBLOB),1)
  BUILD_TAGS += azureblob
endif
ifeq ($(WITH_MINIO),1)
  BUILD_TAGS += minio
endif

# ------------------------------------------------------------------------------
# Desktop GUI (default: disabled, requires Wails v2 + CGO)
# ------------------------------------------------------------------------------
# Wails v2 desktop application (Svelte 5 + Tailwind + daisyUI)
# Requires: wails CLI, node/npm, webkit2gtk-4.1 (Linux)
WITH_GUI         ?= 0

# GUI requires CGO and adds desktop + webkit2_41 build tags
ifeq ($(WITH_GUI),1)
  GUI_CGO_ENABLED := 1
endif

# ==============================================================================
# Codec Build Configuration (github.com/jeremyhahn/go-codec v1.3.0)
# ==============================================================================
# All codecs enabled by default. Set to 0 to exclude from build.
WITH_CODEC_JSON     ?= 1
WITH_CODEC_YAML     ?= 1
WITH_CODEC_TOML     ?= 1
WITH_CODEC_MSGPACK  ?= 1
WITH_CODEC_BSON     ?= 1
WITH_CODEC_CBOR     ?= 1
WITH_CODEC_PROTOBUF ?= 1

# Accumulate codec build tags
ifeq ($(WITH_CODEC_JSON),1)
  BUILD_TAGS += codec_json
endif
ifeq ($(WITH_CODEC_YAML),1)
  BUILD_TAGS += codec_yaml
endif
ifeq ($(WITH_CODEC_TOML),1)
  BUILD_TAGS += codec_toml
endif
ifeq ($(WITH_CODEC_MSGPACK),1)
  BUILD_TAGS += codec_msgpack
endif
ifeq ($(WITH_CODEC_BSON),1)
  BUILD_TAGS += codec_bson
endif
ifeq ($(WITH_CODEC_CBOR),1)
  BUILD_TAGS += codec_cbor
endif
ifeq ($(WITH_CODEC_PROTOBUF),1)
  BUILD_TAGS += codec_protobuf
endif

# Desktop GUI build tags
ifeq ($(WITH_GUI),1)
  BUILD_TAGS += desktop webkit2_41
endif

# dolthub/go-mysql-server's internal/regex ships exactly two files, gated
# `cgo && !gms_pure_go` and `gms_pure_go`. The daemon links 42 packages from
# that module and is always built CGO_ENABLED=0 (static binary), which selects
# neither file and fails the whole build:
#
#   internal/regex: build constraints exclude all Go files in .../internal/regex
#
# So gms_pure_go is not a preference here, it is the only configuration in
# which the daemon compiles at all. It is applied to every target rather than
# just the CGO-free ones so the daemon and the client agree on RLIKE/REGEXP
# semantics instead of quietly using different engines.
BUILD_TAGS += gms_pure_go

# Strip leading/trailing spaces for go build
BUILD_TAGS_ARG := $(if $(strip $(BUILD_TAGS)),-tags '$(strip $(BUILD_TAGS))',)
# FIPS builds need to combine fips tag with optional features
FIPS_BUILD_TAGS := $(strip $(BUILD_TAGS))
FIPS_BUILD_TAGS_ARG := -tags '$(strip $(FIPS_BUILD_TAGS))'
# Integration tests need Raft (cluster mode), vector, and cloud object storage backends
INTEGRATION_BUILD_TAGS := $(strip $(BUILD_TAGS)) raft vector qrdb_vector rag awss3 gcpstorage azureblob minio

# Docker - detect V2 vs V1
DOCKER := docker
DOCKER_COMPOSE := $(shell docker compose version >/dev/null 2>&1 && echo "docker compose" || echo "docker-compose")
DOCKER_IMAGE := $(APP_NAME)
DOCKER_TAG := $(VERSION)
DOCKER_REGISTRY := ghcr.io/jeremyhahn

# Proto tools
PROTOC := protoc
PROTOC_GEN_GO := $(shell which protoc-gen-go)
PROTOC_GEN_GO_GRPC := $(shell which protoc-gen-go-grpc)
BUF := buf

# PROTO_QRDB_DIR points at a go-qrdb checkout so the quicbaas protos can import
# pkg/api/grpc/proto/qrdbv1/authz.proto (the qrdb.v1.permission method option
# the unified server's reflection interceptor enforces). Defaults to the
# sibling checkout; override with PROTO_QRDB_DIR=/path/to/go-qrdb when the
# layout differs.
PROTO_QRDB_DIR ?= ../go-qrdb

# Colors for output (using tput for portability)
RED := $(shell tput setaf 1 2>/dev/null || echo "")
GREEN := $(shell tput setaf 2 2>/dev/null || echo "")
YELLOW := $(shell tput setaf 3 2>/dev/null || echo "")
BLUE := $(shell tput setaf 4 2>/dev/null || echo "")
NC := $(shell tput sgr0 2>/dev/null || echo "")

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\n$(BLUE)Usage:$(NC)\n  make $(GREEN)<target>$(NC)\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2 } /^##@/ { printf "\n$(BLUE)%s$(NC)\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: version
version: ## Show version information
	@echo "$(APP_NAME) version $(VERSION)"
	@echo "Build time: $(BUILD_TIME)"
	@echo "Git commit: $(GIT_COMMIT)"
	@echo "Go version: $(GO_VERSION)"

##@ Development

.PHONY: deps
deps: ## Download dependencies
	@echo "$(BLUE)Downloading dependencies...$(NC)"
	$(GOMOD) download
	$(GOMOD) tidy

.PHONY: update-deps
update-deps: ## Update dependencies
	@echo "$(BLUE)Updating dependencies...$(NC)"
	$(GOGET) -u ./...
	$(GOMOD) tidy

.PHONY: generate
generate: ## Run go generate
	@echo "$(BLUE)Running go generate...$(NC)"
	$(GOCMD) generate ./...

##@ Protocol Buffers

.PHONY: proto-gen
proto-gen: ## Generate protobuf code from .proto files
	@echo "$(BLUE)Generating protobuf code...$(NC)"
	@if [ -z "$(PROTOC_GEN_GO)" ]; then \
		echo "$(RED)protoc-gen-go not found. Run: make install-proto-tools$(NC)"; \
		exit 1; \
	fi
	@if [ -z "$(PROTOC_GEN_GO_GRPC)" ]; then \
		echo "$(RED)protoc-gen-go-grpc not found. Run: make install-proto-tools$(NC)"; \
		exit 1; \
	fi
	@if [ ! -f "$(PROTO_QRDB_DIR)/pkg/api/grpc/proto/qrdbv1/authz.proto" ]; then \
		echo "$(RED)qrdb authz.proto not found under PROTO_QRDB_DIR=$(PROTO_QRDB_DIR)$(NC)"; \
		exit 1; \
	fi
	@$(PROTOC) -I. -I$(PROTO_QRDB_DIR) \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		--go-grpc_opt=require_unimplemented_servers=false \
		$(PROTO_DIR)/*.proto
	@echo "$(GREEN)Protobuf code generated successfully$(NC)"

.PHONY: proto-lint
proto-lint: ## Lint proto files
	@echo "$(BLUE)Linting proto files...$(NC)"
	@if ! command -v buf > /dev/null; then \
		echo "$(YELLOW)buf not installed, using protoc lint...$(NC)"; \
		$(PROTOC) --descriptor_set_out=/dev/null $(PROTO_DIR)/*.proto; \
	else \
		$(BUF) lint; \
	fi
	@echo "$(GREEN)Proto files linted successfully$(NC)"

.PHONY: proto-breaking
proto-breaking: ## Check for breaking changes in proto files
	@echo "$(BLUE)Checking for breaking changes...$(NC)"
	@if ! command -v buf > /dev/null; then \
		echo "$(YELLOW)buf not installed, skipping breaking change detection$(NC)"; \
	else \
		$(BUF) breaking --against '.git#branch=main'; \
	fi

.PHONY: proto-format
proto-format: ## Format proto files
	@echo "$(BLUE)Formatting proto files...$(NC)"
	@if ! command -v buf > /dev/null; then \
		echo "$(YELLOW)buf not installed, skipping proto formatting$(NC)"; \
	else \
		$(BUF) format -w; \
	fi

.PHONY: proto-clean
proto-clean: ## Remove generated protobuf files
	@echo "$(BLUE)Cleaning generated proto files...$(NC)"
	@find $(PROTO_DIR) -name '*.pb.go' -delete
	@find $(PROTO_DIR) -name '*_grpc.pb.go' -delete
	@echo "$(GREEN)Generated proto files removed$(NC)"

.PHONY: install-proto-tools
install-proto-tools: ## Install protobuf code generation tools
	@echo "$(BLUE)Installing protobuf tools...$(NC)"
	@$(GOCMD) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@$(GOCMD) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "$(GREEN)Protobuf tools installed successfully$(NC)"
	@echo "$(YELLOW)Note: You also need protoc compiler installed on your system$(NC)"
	@echo "$(YELLOW)  - Ubuntu/Debian: apt-get install protobuf-compiler$(NC)"
	@echo "$(YELLOW)  - macOS: brew install protobuf$(NC)"
	@echo "$(YELLOW)  - Or download from: https://github.com/protocolbuffers/protobuf/releases$(NC)"

.PHONY: install-buf
install-buf: ## Install Buf CLI tool
	@echo "$(BLUE)Installing Buf CLI...$(NC)"
	@if command -v buf > /dev/null; then \
		echo "$(GREEN)Buf already installed$(NC)"; \
	else \
		curl -sSL "https://github.com/bufbuild/buf/releases/download/v1.28.1/buf-$$(uname -s)-$$(uname -m)" -o /tmp/buf; \
		chmod +x /tmp/buf; \
		sudo mv /tmp/buf /usr/local/bin/buf; \
		echo "$(GREEN)Buf installed successfully$(NC)"; \
	fi

# Backward compatibility alias
.PHONY: proto
proto: proto-gen ## Alias for proto-gen

.PHONY: fmt
fmt: ## Format code (excludes vendor)
	@echo "$(BLUE)Formatting code...$(NC)"
	@find . -name '*.go' -not -path '*/vendor/*' -not -path '*/.deps/*' -not -path '*/.tmp/*' -not -path '*/tmp/*' | xargs $(GOFMT) -s -w

.PHONY: fmt-check
fmt-check: ## Check code formatting (fails if not formatted, excludes vendor)
	@echo "$(BLUE)Checking code formatting...$(NC)"
	@UNFORMATTED=$$(find . -name '*.go' -not -path '*/vendor/*' -not -path '*/.deps/*' -not -path '*/.tmp/*' -not -path '*/tmp/*' | xargs $(GOFMT) -l); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "$(RED)Code not formatted. Run 'make fmt' to fix:$(NC)"; \
		echo "$$UNFORMATTED"; \
		exit 1; \
	fi
	@echo "$(GREEN)All code is properly formatted$(NC)"

.PHONY: check-insecure-tls
check-insecure-tls: ## Enforce InsecureSkipVerify allowlist - empty allowlist means any assignment fails
	@echo "$(BLUE)Checking InsecureSkipVerify allowlist...$(NC)"
	@# The allowlist is the `//nolint:gosec` annotation itself: a bare
	@# InsecureSkipVerify:true still fails, but a justified, reviewed one
	@# (e.g. sdk/go/transport/quic/transport_test.go's self-signed loopback
	@# cert) is allowed via the same trailing marker gosec itself recognizes.
	@# Runner-log-proven (task 2935): the exclude pattern previously only
	@# matched a fully commented-out line (`^...: *//`), not a trailing
	@# comment on real code, so this check failed on every run regardless of
	@# the existing nolint annotation's intent.
	@violations=$$(grep -rPn 'InsecureSkipVerify[[:space:]]*:[[:space:]]*true' --include="*.go" --exclude-dir='.deps' --exclude-dir='vendor' . \
		| grep -Pv '^[^:]+:[0-9]+:[[:space:]]*//|//nolint:gosec' \
		|| true); \
	if [ -n "$$violations" ]; then \
		echo "$(RED)FORBIDDEN InsecureSkipVerify in go-quicbaas (no allowlist entries):$(NC)"; \
		printf '%s\n' "$$violations"; \
		exit 1; \
	fi
	@echo "$(GREEN)InsecureSkipVerify allowlist check passed$(NC)"


.PHONY: vet
vet: ## Run go vet across every tag pass (untagged vet skips tag-gated packages)
	@echo "$(BLUE)Running go vet (maximal tags)...$(NC)"
	$(GOCMD) vet -tags '$(TEST_TAGS)' ./...
	@echo "$(BLUE)Running go vet (minimal tags)...$(NC)"
	$(GOCMD) vet -tags '$(MINIMAL_TEST_TAGS)' ./...
	@echo "$(BLUE)Running go vet (desktop GUI tags)...$(NC)"
	$(GOCMD) vet -tags '$(DESKTOP_TEST_TAGS)' ./...
	@echo "$(BLUE)Running go vet (integration tree)...$(NC)"
	$(GOCMD) vet -tags 'integration $(strip $(INTEGRATION_BUILD_TAGS)) $(TEST_EXTRA_TAGS)' ./test/...

##@ Build

.PHONY: build
build: build-daemon build-cli ## Build both daemon (quicbaasd) and client (quicbaas with embedded desktop GUI)

.PHONY: build-daemon
build-daemon: ## Build the daemon (quicbaasd)
	@echo "$(BLUE)Building $(APP_NAME)d daemon...$(NC)"
ifneq ($(strip $(BUILD_TAGS)),)
	@echo "$(YELLOW)Build tags: $(INTEGRATION_BUILD_TAGS)$(NC)"
endif
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(BUILD_TAGS_ARG) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d ./$(DAEMON_CMD_DIR)
	@echo "$(GREEN)Build successful: $(BIN_DIR)/$(APP_NAME)d$(NC)"

# Frontend build configuration
# BUILD_DIR for frontend, handling NFS EPERM issues on Node.js worker threads
FE_BUILD_DIR ?= /tmp/fe-build-$(shell whoami)

# Frontend build prerequisites (only when WITH_GUI=1)
.PHONY: _build-sdk-typescript _build-frontend

_build-sdk-typescript:
	@echo "$(BLUE)Building TypeScript SDK...$(NC)"
	@cd sdk/typescript && npm install --silent && node ./node_modules/typescript/bin/tsc
	@echo "$(GREEN)TypeScript SDK built$(NC)"

_build-frontend: _build-sdk-typescript
	@echo "$(BLUE)Building frontend (using temporary build directory: $(FE_BUILD_DIR))...$(NC)"
	@mkdir -p $(FE_BUILD_DIR)
	@echo "$(YELLOW)Copying sources to $(FE_BUILD_DIR)...$(NC)"
	@rsync -a --delete cmd/quicbaas/frontend/ $(FE_BUILD_DIR)/ --exclude node_modules --exclude package-lock.json --exclude dist || exit 1
	@echo "$(YELLOW)Running npm install...$(NC)"
	@cd $(FE_BUILD_DIR) && npm install 2>&1 | tail -5
	@echo "$(YELLOW)Overriding @quicbaas/sdk with absolute symlink (post npm-install to survive)...$(NC)"
	@mkdir -p $(FE_BUILD_DIR)/node_modules/@quicbaas
	@rm -rf $(FE_BUILD_DIR)/node_modules/@quicbaas/sdk
	@ln -sfn $(abspath sdk/typescript) $(FE_BUILD_DIR)/node_modules/@quicbaas/sdk
	@test -e $(FE_BUILD_DIR)/node_modules/@quicbaas/sdk/dist/index.js \
		|| (echo "$(RED)SDK symlink broken — dist/index.js not reachable via symlink$(NC)" && exit 1)
	@echo "$(YELLOW)Building...$(NC)"
	@rm -rf $(FE_BUILD_DIR)/dist
	@cd $(FE_BUILD_DIR) && set -o pipefail; npm run build 2>&1 | tail -10
	@test -f $(FE_BUILD_DIR)/dist/index.html \
		|| (echo "$(RED)Frontend build produced no dist/index.html — build failed silently$(NC)" && exit 1)
	@echo "$(BLUE)Copying dist back to embed location + frontend tree...$(NC)"
	@rm -rf pkg/api/rest/ui/dist cmd/quicbaas/frontend/dist
	@cp -r $(FE_BUILD_DIR)/dist pkg/api/rest/ui/dist
	@cp -r $(FE_BUILD_DIR)/dist cmd/quicbaas/frontend/dist
	@echo "$(GREEN)Frontend built$(NC)"

.PHONY: build-sdk-typescript
build-sdk-typescript: ## Build TypeScript SDK (direct compilation)
	@echo "$(BLUE)Building TypeScript SDK...$(NC)"
	@cd sdk/typescript && npm install --silent && node ./node_modules/typescript/bin/tsc
	@echo "$(GREEN)TypeScript SDK built$(NC)"

.PHONY: build-frontend
build-frontend: build-sdk-typescript ## Build frontend (handles NFS EPERM via temp dir)
	@echo "$(BLUE)Building frontend...$(NC)"
	@mkdir -p $(FE_BUILD_DIR)
	@echo "$(YELLOW)Syncing sources to $(FE_BUILD_DIR)...$(NC)"
	@rsync -a --delete cmd/quicbaas/frontend/ $(FE_BUILD_DIR)/ --exclude node_modules --exclude package-lock.json --exclude dist || exit 1
	@echo "$(YELLOW)Running npm install...$(NC)"
	@cd $(FE_BUILD_DIR) && npm install 2>&1 | tail -5
	@echo "$(YELLOW)Overriding @quicbaas/sdk + @quicbaas/svelte-security with absolute symlinks (npm install would make them relative)...$(NC)"
	@mkdir -p $(FE_BUILD_DIR)/node_modules/@quicbaas
	@rm -rf $(FE_BUILD_DIR)/node_modules/@quicbaas/sdk
	@ln -sfn $(abspath sdk/typescript) $(FE_BUILD_DIR)/node_modules/@quicbaas/sdk
	@test -e $(FE_BUILD_DIR)/node_modules/@quicbaas/sdk/dist/index.js \
		|| (echo "$(RED)SDK symlink broken — dist/index.js not reachable via symlink$(NC)" && exit 1)
	@rm -rf $(FE_BUILD_DIR)/node_modules/@quicbaas/svelte-security
	@ln -sfn $(abspath sdk/typescript/svelte-security) $(FE_BUILD_DIR)/node_modules/@quicbaas/svelte-security
	@test -e $(FE_BUILD_DIR)/node_modules/@quicbaas/svelte-security/src/index.ts \
		|| (echo "$(RED)svelte-security symlink broken — src/index.ts not reachable via symlink$(NC)" && exit 1)
	@echo "$(YELLOW)Purging vite cache + stale dist...$(NC)"
	@rm -rf $(FE_BUILD_DIR)/node_modules/.vite $(FE_BUILD_DIR)/dist
	@echo "$(YELLOW)Running vite build...$(NC)"
	@cd $(FE_BUILD_DIR) && bash -c "set -o pipefail; npm run build 2>&1 | tail -10" \
		|| (echo "$(RED)Frontend build failed — see errors above$(NC)" && exit 1)
	@test -f $(FE_BUILD_DIR)/dist/index.html \
		|| (echo "$(RED)Frontend build produced no dist/index.html — aborting$(NC)" && exit 1)
	@echo "$(BLUE)Copying dist back to embed location + frontend tree...$(NC)"
	@rm -rf pkg/api/rest/ui/dist cmd/quicbaas/frontend/dist
	@cp -r $(FE_BUILD_DIR)/dist pkg/api/rest/ui/dist
	@cp -r $(FE_BUILD_DIR)/dist cmd/quicbaas/frontend/dist
	@echo "$(GREEN)Frontend built$(NC)"

.PHONY: build-ui
build-ui: build-sdk-typescript build-frontend ## Build both SDK and frontend

# The client binary (quicbaas) is ALWAYS built with the desktop GUI
# embedded — matches go-qrdb and go-xkms conventions. Launch dispatch:
#   ./bin/quicbaas              → GUI when display is present, else CLI
#   ./bin/quicbaas <subcommand> → CLI
#   ./bin/quicbaas --help       → CLI (never opens a window)
# CGO=1 + desktop build tag link Wails+GTK/WebKit. Requires:
#   sudo apt-get install libgtk-3-dev libwebkit2gtk-4.1-dev
# on Ubuntu 24.04 / Debian 13. The desktop tags (desktop, production,
# webkit2_41) are the same Wails v2 set go-xkms's xkey target uses.
CLI_DESKTOP_TAGS := desktop production webkit2_41

.PHONY: build-cli
build-cli: build-ui ## Build the client (quicbaas) — single binary with embedded desktop GUI and CLI dispatch
	@echo "$(BLUE)Building $(APP_NAME) client (CLI + embedded desktop GUI)...$(NC)"
	@if ! pkg-config --exists webkit2gtk-4.1; then \
		echo "$(RED)Error: libwebkit2gtk-4.1-dev not found$(NC)"; \
		echo "$(YELLOW)Install with: sudo apt-get install libgtk-3-dev libwebkit2gtk-4.1-dev$(NC)"; \
		exit 1; \
	fi
	@echo "$(YELLOW)Build tags: $(BUILD_TAGS) $(CLI_DESKTOP_TAGS)$(NC)"
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=1 $(GOBUILD) -tags '$(BUILD_TAGS) $(CLI_DESKTOP_TAGS)' -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME) ./$(CMD_DIR)
	@echo "$(GREEN)Build successful: $(BIN_DIR)/$(APP_NAME)$(NC)"
	@echo "$(BLUE)Version info:$(NC)"
	@$(BIN_DIR)/$(APP_NAME) version --short

##@ Deploy

# Local dev-daemon redeploy configuration. Defaults match the self-hosting
# daemon; override on the command line, e.g.:
#   make redeploy DEPLOY_CONFIG=/etc/quicbaas.yaml DEPLOY_REST_PORT=8443
DEPLOY_CONFIG    ?= /tmp/qb-self.yaml
DEPLOY_DATA_DIR  ?= /tmp/qb-self-data
DEPLOY_LOG       ?= /tmp/qb-self.log
DEPLOY_REST_PORT ?= 18444

.PHONY: redeploy
redeploy: ## Rebuild client + daemon (embedded web UI) and restart the local dev daemon
	@# build-cli FIRST: it regenerates the frontend dist that build-daemon
	@# embeds (pkg/api/rest/ui/dist via go:embed). The reverse order ships a
	@# daemon whose web UI is one build stale.
	@$(MAKE) build-cli
	@$(MAKE) build-daemon
	@echo "$(BLUE)Stopping $(APP_NAME)d (config: $(DEPLOY_CONFIG))...$(NC)"
	@# Bracketed first character keeps the pattern from matching this recipe's
	@# own shell (whose argv contains the pattern), which pkill would otherwise
	@# terminate along with the daemon.
	@pkill -f "[q]uicbaasd start --config $(DEPLOY_CONFIG)" || true
	@for i in $$(seq 1 50); do \
		pgrep -f "[q]uicbaasd start --config $(DEPLOY_CONFIG)" >/dev/null || break; \
		sleep 0.2; \
	done
	@if pgrep -f "[q]uicbaasd start --config $(DEPLOY_CONFIG)" >/dev/null; then \
		echo "$(RED)Daemon did not stop within 10s$(NC)"; exit 1; \
	fi
	@echo "$(BLUE)Starting $(BIN_DIR)/$(APP_NAME)d...$(NC)"
	@nohup $(BIN_DIR)/$(APP_NAME)d start --config $(DEPLOY_CONFIG) --data-dir $(DEPLOY_DATA_DIR) >> $(DEPLOY_LOG) 2>&1 &
	@for i in $$(seq 1 100); do \
		curl -skf https://127.0.0.1:$(DEPLOY_REST_PORT)/api/v1/health >/dev/null 2>&1 && break; \
		sleep 0.3; \
	done
	@curl -skf https://127.0.0.1:$(DEPLOY_REST_PORT)/api/v1/health >/dev/null 2>&1 \
		|| (echo "$(RED)Daemon failed readiness after restart — check $(DEPLOY_LOG)$(NC)" && exit 1)
	@echo "$(GREEN)Redeployed: daemon restarted, web UI live at https://127.0.0.1:$(DEPLOY_REST_PORT)$(NC)"
	@echo "$(YELLOW)Desktop GUI ($(BIN_DIR)/$(APP_NAME)) was rebuilt — relaunch it to pick up the new UI$(NC)"

.PHONY: build-static
build-static: build-static-daemon build-static-cli ## Build static binaries for containers

.PHONY: build-static-daemon
build-static-daemon: ## Build static daemon binary
	@echo "$(BLUE)Building static $(APP_NAME)d daemon...$(NC)"
ifneq ($(strip $(BUILD_TAGS)),)
	@echo "$(YELLOW)Build tags: $(INTEGRATION_BUILD_TAGS)$(NC)"
endif
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(BUILD_TAGS_ARG) -ldflags "$(STATIC_LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-static ./$(DAEMON_CMD_DIR)
	@echo "$(GREEN)Static build successful: $(BIN_DIR)/$(APP_NAME)d-static$(NC)"

.PHONY: build-static-cli
build-static-cli: ## Build static CLI binary
	@echo "$(BLUE)Building static $(APP_NAME) CLI...$(NC)"
ifneq ($(strip $(BUILD_TAGS)),)
	@echo "$(YELLOW)Build tags: $(INTEGRATION_BUILD_TAGS)$(NC)"
endif
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(BUILD_TAGS_ARG) -ldflags "$(STATIC_LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-static ./$(CMD_DIR)
	@echo "$(GREEN)Static build successful: $(BIN_DIR)/$(APP_NAME)-static$(NC)"

.PHONY: build-fips
build-fips: build-fips-daemon build-fips-cli ## Build with FIPS 140-3 mode using GOFIPS140

.PHONY: build-fips-daemon
build-fips-daemon: ## Build FIPS daemon
	@echo "$(BLUE)Building $(APP_NAME)d with FIPS 140-3 mode...$(NC)"
	@echo "$(YELLOW)Using GOFIPS140=latest (Go 1.24+)$(NC)"
	@echo "$(YELLOW)Build tags: $(FIPS_BUILD_TAGS)$(NC)"
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOFIPS140=latest $(GOBUILD) $(FIPS_BUILD_TAGS_ARG) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-fips ./$(DAEMON_CMD_DIR)
	@echo "$(GREEN)FIPS build successful: $(BIN_DIR)/$(APP_NAME)d-fips$(NC)"

.PHONY: build-fips-cli
build-fips-cli: ## Build FIPS CLI
	@echo "$(BLUE)Building $(APP_NAME) CLI with FIPS 140-3 mode...$(NC)"
	@echo "$(YELLOW)Using GOFIPS140=latest (Go 1.24+)$(NC)"
	@echo "$(YELLOW)Build tags: $(FIPS_BUILD_TAGS)$(NC)"
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOFIPS140=latest $(GOBUILD) $(FIPS_BUILD_TAGS_ARG) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-fips ./$(CMD_DIR)
	@echo "$(GREEN)FIPS build successful: $(BIN_DIR)/$(APP_NAME)-fips$(NC)"

.PHONY: build-static-fips
build-static-fips: build-static-fips-daemon build-static-fips-cli ## Build static FIPS binaries for containers

.PHONY: build-static-fips-daemon
build-static-fips-daemon: ## Build static FIPS daemon
	@echo "$(BLUE)Building static $(APP_NAME)d with FIPS 140-3 mode...$(NC)"
	@echo "$(YELLOW)Using GOFIPS140=latest (Go 1.24+)$(NC)"
	@echo "$(YELLOW)Build tags: $(FIPS_BUILD_TAGS)$(NC)"
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOFIPS140=latest $(GOBUILD) $(FIPS_BUILD_TAGS_ARG) -ldflags "$(STATIC_LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-fips-static ./$(DAEMON_CMD_DIR)
	@echo "$(GREEN)Static FIPS build successful: $(BIN_DIR)/$(APP_NAME)d-fips-static$(NC)"

.PHONY: build-static-fips-cli
build-static-fips-cli: ## Build static FIPS CLI
	@echo "$(BLUE)Building static $(APP_NAME) CLI with FIPS 140-3 mode...$(NC)"
	@echo "$(YELLOW)Using GOFIPS140=latest (Go 1.24+)$(NC)"
	@echo "$(YELLOW)Build tags: $(FIPS_BUILD_TAGS)$(NC)"
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOFIPS140=latest $(GOBUILD) $(FIPS_BUILD_TAGS_ARG) -ldflags "$(STATIC_LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-fips-static ./$(CMD_DIR)
	@echo "$(GREEN)Static FIPS build successful: $(BIN_DIR)/$(APP_NAME)-fips-static$(NC)"

.PHONY: build-all
build-all: build build-static build-fips build-static-fips ## Build all variants (standard, static, FIPS, static-FIPS)

# ==============================================================================
# Feature-Based Build Variants
# ==============================================================================
# These targets provide pre-configured builds with specific feature sets.
# Use these for common deployment scenarios.

.PHONY: build-embedded
build-embedded: ## Build single-node embedded binary (no Raft, no extra APIs)
	@echo "$(BLUE)Building embedded binary (single-node, gRPC only)...$(NC)"
	$(MAKE) build \
		WITH_RAFT=0 \
		WITH_TIMESERIES=0 WITH_VECTOR=0 WITH_RAG=0 \
		WITH_REST=0 WITH_QUIC=0 WITH_MCP=0

.PHONY: build-minimal
build-minimal: ## Build minimal binary (core + gRPC only, no optional features)
	@echo "$(BLUE)Building minimal binary...$(NC)"
	$(MAKE) build \
		WITH_RAFT=0 \
		WITH_TIMESERIES=0 WITH_VECTOR=0 WITH_RAG=0 \
		WITH_REST=0 WITH_QUIC=0 WITH_MCP=0

.PHONY: build-standard
build-standard: ## Build standard binary (cluster, no AI features)
	@echo "$(BLUE)Building standard binary...$(NC)"
	$(MAKE) build \
		WITH_VECTOR=0 WITH_RAG=0 WITH_MCP=0

.PHONY: build-cluster
build-cluster: ## Build cluster-ready binary (all features, no cloud KMS)
	@echo "$(BLUE)Building cluster binary...$(NC)"
	$(MAKE) build

.PHONY: build-enterprise
build-enterprise: ## Build enterprise binary (all features + cloud KMS)
	@echo "$(BLUE)Building enterprise binary...$(NC)"
	$(MAKE) build \
		WITH_AWS=1 WITH_GCP=1 WITH_AZURE=1 WITH_VAULT=1

.PHONY: build-full
build-full: ## Build full-featured binary (everything including HSM, quantum)
	@echo "$(BLUE)Building full binary...$(NC)"
	$(MAKE) build \
		WITH_AWS=1 WITH_GCP=1 WITH_AZURE=1 WITH_VAULT=1 \
		WITH_PKCS11=1 WITH_QUANTUM=1 \
		WITH_L2_REDIS=1

.PHONY: build-linux-amd64
build-linux-amd64: ## Build for Linux AMD64
	@echo "$(BLUE)Building for Linux AMD64...$(NC)"
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-linux-amd64 ./$(DAEMON_CMD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-linux-amd64 ./$(CMD_DIR)
	@echo "$(GREEN)Linux AMD64 build successful$(NC)"

.PHONY: build-linux-arm64
build-linux-arm64: ## Build for Linux ARM64
	@echo "$(BLUE)Building for Linux ARM64...$(NC)"
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-linux-arm64 ./$(DAEMON_CMD_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-linux-arm64 ./$(CMD_DIR)
	@echo "$(GREEN)Linux ARM64 build successful$(NC)"

.PHONY: build-darwin-amd64
build-darwin-amd64: ## Build for macOS AMD64
	@echo "$(BLUE)Building for macOS AMD64...$(NC)"
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-darwin-amd64 ./$(DAEMON_CMD_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-darwin-amd64 ./$(CMD_DIR)
	@echo "$(GREEN)macOS AMD64 build successful$(NC)"

.PHONY: build-darwin-arm64
build-darwin-arm64: ## Build for macOS ARM64 (Apple Silicon)
	@echo "$(BLUE)Building for macOS ARM64...$(NC)"
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-darwin-arm64 ./$(DAEMON_CMD_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-darwin-arm64 ./$(CMD_DIR)
	@echo "$(GREEN)macOS ARM64 build successful$(NC)"

.PHONY: build-linux
build-linux: ## Build for Linux
	@echo "$(BLUE)Building for Linux...$(NC)"
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-linux-amd64 ./$(DAEMON_CMD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-linux-amd64 ./$(CMD_DIR)

.PHONY: build-darwin
build-darwin: ## Build for macOS
	@echo "$(BLUE)Building for macOS...$(NC)"
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-darwin-amd64 ./$(DAEMON_CMD_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-darwin-amd64 ./$(CMD_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-darwin-arm64 ./$(DAEMON_CMD_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-darwin-arm64 ./$(CMD_DIR)

.PHONY: build-windows
build-windows: ## Build for Windows
	@echo "$(BLUE)Building for Windows...$(NC)"
	@mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)d-windows-amd64.exe ./$(DAEMON_CMD_DIR)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-windows-amd64.exe ./$(CMD_DIR)

.PHONY: cross-compile
cross-compile: build-linux build-darwin build-windows ## Cross-compile for all platforms

.PHONY: install
install: build ## Install the binary
	@echo "$(BLUE)Installing $(APP_NAME)...$(NC)"
	$(GOCMD) install -ldflags "$(LDFLAGS)" ./$(CMD_DIR)

##@ Testing

# ==============================================================================
# Test tag matrix
# ==============================================================================
# Go compiles a file only when its //go:build constraint is satisfied, so a
# package sitting behind a tag that no test pass supplies is skipped in silence:
# `go test` prints "no test files" and the target still goes green. Every tag
# used anywhere under pkg/ or cmd/ must therefore appear in one of the passes
# below, or be listed in TAG_GATE_EXEMPT with the reason it cannot run.
# `make check-build-tags` enforces that and is a prerequisite of `make test`.
#
#   TEST_TAGS          maximal pass: every default-on feature compiled in.
#   MINIMAL_TEST_TAGS  minimal pass: every optional feature OFF, so the
#                      !rest / !quic / !mcp / !rag / !timeseries / !vector /
#                      !raft / !otel / !l2_redis / !desktop stub and no-op
#                      files -- and the tests asserting their behaviour --
#                      actually compile. The maximal pass can never reach
#                      these, because a tag and its negation are mutually
#                      exclusive within a single `go test` invocation.
#   DESKTOP_TEST_TAGS  Wails GUI bindings pass. Needs the webkit2gtk
#                      development libraries, so it gets its own target;
#                      `make test` must stay runnable with no GUI toolchain.
#
# TEST_EXTRA_TAGS gate tests only and must never change a shipped binary, so
# they live here and deliberately NOT in BUILD_TAGS:
#   canary    memory-scrubbing canary assertions in pkg/api/rest/secureio
#   otel      OpenTelemetry tracing helpers in pkg/api/grpc
#   l2_redis  Redis L2 cache; its unit tests drive an in-process mock client
#
# rest/quic/mcp/rag gate whole transport packages. Without them `go test`
# reports "no test files" for pkg/api/{rest,quic,mcp} and PASSES, so every
# authz-mapping, sealed-window and opcode-parity guard in those packages was
# silently absent from this gate — a control that appears present but never
# executes. They are listed here, not in BUILD_TAGS, because they must gate
# tests without altering a shipped binary.
TEST_EXTRA_TAGS := canary otel l2_redis rest quic mcp rag

# Note: Go 1.25+ requires space-separated tags, not comma-separated.
# The leading !integration is documentation: `integration` is simply absent
# from the list, which is what actually excludes the integration-tagged files.
TEST_TAGS := !integration $(strip $(BUILD_TAGS)) $(TEST_EXTRA_TAGS)
MINIMAL_TEST_TAGS := !integration hashicorpmetrics local
DESKTOP_TEST_TAGS := !integration $(strip $(BUILD_TAGS)) $(TEST_EXTRA_TAGS) desktop webkit2_41

# Tag lists for tools that reject the decorative !integration entry.
LINT_BUILD_TAGS := $(filter-out !integration,$(TEST_TAGS))

# Tags deliberately outside every test pass, each with the reason. Anything
# not covered above and not listed here fails `make check-build-tags`.
#   linux darwin windows unix   set by the toolchain from GOOS, never by us
#   ignore                      Go convention for a permanently excluded file
#   integration                 has its own gate: `make integration-test`
#   coverage                    coverage-mode toggle, driven by coverage targets
#   production                  shipped-build-only tag, and mutually exclusive
#                               with the !production test mocks in pkg/secrets
#   pkcs11 swtpm tpm_simulator  need real or simulated crypto hardware
#   awskms gcpkms azurekv vault
#   awss3 gcpstorage azureblob minio
#                               need live cloud credentials / endpoints
#   gpu                         needs an NVIDIA GPU
#   quicbaas_signal             pkg/signal DOES NOT COMPILE. Its kvAdapter is
#                               missing DeleteIfPresent, which go-signal's
#                               store.KVClient requires and which go-qrdb's
#                               sdk/go/transport.KVClient cannot supply
#                               atomically. That primitive is what makes
#                               one-time prekey consumption single-use, so this
#                               needs a qrdb SDK addition -- the package is
#                               currently unbuildable, not merely untested.
TAG_GATE_EXEMPT := linux darwin windows unix ignore integration coverage production \
	pkcs11 swtpm tpm_simulator \
	awskms gcpkms azurekv vault awss3 gcpstorage azureblob minio \
	gpu quicbaas_signal

# IAM_TEST_TAGS: retained alias for TEST_TAGS. IAM is now always compiled
# (the 'iam' build tag was removed in phase C1-G), so no extra tag is needed
# for packages under pkg/iam/... or pkg/server/. Kept as an alias so existing
# test/coverage/bench targets continue to reference a stable variable name.
IAM_TEST_TAGS := $(TEST_TAGS)

.PHONY: test
test: check-insecure-tls check-build-tags ## Run unit tests (excludes integration tests)
	@echo "$(BLUE)Running unit tests (excludes integration tests)...$(NC)"
ifneq ($(strip $(BUILD_TAGS)),)
	@echo "$(YELLOW)Build tags: $(TEST_TAGS)$(NC)"
endif
	@START_TIME=$$(date +%s); \
	$(GOTEST) -v -p 2 -tags '$(TEST_TAGS)' -timeout=5m ./pkg/... ./cmd/...; \
	TEST_RESULT=$$?; \
	END_TIME=$$(date +%s); \
	ELAPSED=$$((END_TIME - START_TIME)); \
	MINUTES=$$((ELAPSED / 60)); \
	SECONDS=$$((ELAPSED % 60)); \
	echo ""; \
	if [ $$MINUTES -gt 0 ]; then \
		printf "$(GREEN)Total time: %dm %ds$(NC)\n" $$MINUTES $$SECONDS; \
	else \
		printf "$(GREEN)Total time: %ds$(NC)\n" $$SECONDS; \
	fi; \
	exit $$TEST_RESULT

.PHONY: check-build-tags
check-build-tags: ## Fail if any //go:build tag is absent from every test pass
	@echo "$(BLUE)Checking build-tag coverage of the test gate...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@printf '%s\n' $(TEST_TAGS) $(MINIMAL_TEST_TAGS) $(DESKTOP_TEST_TAGS) $(TAG_GATE_EXEMPT) \
		| sed 's/^!//' | sort -u > $(BUILD_DIR)/tag-gate-covered.txt
	@grep -rh --include='*.go' '^//go:build' pkg cmd \
		| sed 's|^//go:build||' \
		| tr ' ()!' '\n\n\n\n' \
		| grep -E '^[A-Za-z_][A-Za-z0-9_.]*$$' \
		| grep -Ev '^go1\.' \
		| sort -u > $(BUILD_DIR)/tag-gate-found.txt
	@missing=$$(grep -vxF -f $(BUILD_DIR)/tag-gate-covered.txt $(BUILD_DIR)/tag-gate-found.txt || true); \
	if [ -n "$$missing" ]; then \
		echo "$(RED)These //go:build tags gate code that NO test pass compiles:$(NC)"; \
		printf '  %s\n' $$missing; \
		echo "$(YELLOW)Tests behind them are invisible to the gate: go test reports$(NC)"; \
		echo "$(YELLOW)'no test files' and the target passes green.$(NC)"; \
		echo "$(YELLOW)Fix by adding each tag to TEST_TAGS, MINIMAL_TEST_TAGS or$(NC)"; \
		echo "$(YELLOW)DESKTOP_TEST_TAGS, or to TAG_GATE_EXEMPT with the reason.$(NC)"; \
		exit 1; \
	fi
	@echo "$(GREEN)Build-tag gate coverage OK ($$(wc -l < $(BUILD_DIR)/tag-gate-found.txt) tags)$(NC)"

.PHONY: test-minimal
test-minimal: check-insecure-tls check-build-tags ## Run unit tests with every optional feature OFF (covers the !tag stub/no-op paths)
	@echo "$(BLUE)Running unit tests with all optional features disabled...$(NC)"
	@echo "$(YELLOW)Build tags: $(MINIMAL_TEST_TAGS)$(NC)"
	$(GOTEST) -p 2 -tags '$(MINIMAL_TEST_TAGS)' -timeout=10m ./pkg/... ./cmd/...

.PHONY: test-desktop
test-desktop: check-insecure-tls check-build-tags ## Run the Wails desktop binding tests (needs webkit2gtk dev libraries)
	@echo "$(BLUE)Running desktop GUI binding tests...$(NC)"
	@echo "$(YELLOW)Build tags: $(DESKTOP_TEST_TAGS)$(NC)"
	$(GOTEST) -p 2 -tags '$(DESKTOP_TEST_TAGS)' -timeout=10m ./cmd/...

.PHONY: test-all-tags
test-all-tags: test test-minimal test-desktop ## Run every test pass in the tag matrix
	@echo "$(GREEN)All tag passes completed successfully$(NC)"

.PHONY: test-fips
test-fips: ## Run tests in FIPS mode
	@echo "$(BLUE)Running tests in FIPS 140-3 mode...$(NC)"
	@echo "$(YELLOW)Using GOFIPS140=latest (Go 1.24+)$(NC)"
	GOFIPS140=latest $(GOTEST) -v ./pkg/fips/...

.PHONY: test-unit
test-unit: test ## Alias for test (unit tests only)
	@# This is now an alias for the test target which already excludes integration tests

.PHONY: test-verbose
test-verbose: ## Run tests with verbose output
	@echo "$(BLUE)Running tests (verbose)...$(NC)"
	$(GOTEST) -v -race ./...

# Dynamic package test targets - use: make test-<package>
# Examples: make test-xkms, make test-kms, make test-iam
# Supports nested packages: make test-kms/xkms, make test-security/certs
test-%: ## Run unit tests for a specific package (e.g., make test-xkms)
	@PKG="$*"; \
	PKG_PATH="./pkg/$${PKG}/..."; \
	if [ ! -d "pkg/$${PKG}" ]; then \
		PKG_PATH="./cmd/$${PKG}/..."; \
		if [ ! -d "cmd/$${PKG}" ]; then \
			echo "$(RED)Error: Package '$${PKG}' not found in pkg/ or cmd/$(NC)"; \
			exit 1; \
		fi; \
	fi; \
	case "$${PKG}" in iam|iam/*) TAGS='$(IAM_TEST_TAGS)';; *) TAGS='$(TEST_TAGS)';; esac; \
	echo "$(BLUE)Running tests for $${PKG_PATH}...$(NC)"; \
	$(GOTEST) -v -tags "$${TAGS}" -timeout=5m $${PKG_PATH}

# Redis L2 Cache integration tests
.PHONY: integration-test-cache-redis
integration-test-cache-redis: ## Run Redis L2 cache integration tests in Docker
	@echo "$(BLUE)Starting Redis container...$(NC)"
	@$(DOCKER_COMPOSE) up -d redis
	@echo "$(YELLOW)Waiting for Redis to be ready...$(NC)"
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		$(DOCKER) exec quicbaas-redis redis-cli ping >/dev/null 2>&1 && break || sleep 1; \
	done
	@echo "$(GREEN)Redis is ready$(NC)"
	@echo "$(BLUE)Running Redis L2 cache integration tests...$(NC)"
	@QUICBAAS_REDIS_ADDR=localhost:6379 $(GOTEST) -v -tags 'integration,l2_redis' -timeout=5m ./test/integration/cache/... || ($(DOCKER_COMPOSE) stop redis && exit 1)
	@echo "$(BLUE)Stopping Redis container...$(NC)"
	@$(DOCKER_COMPOSE) stop redis

# GPU Orchestration integration tests
.PHONY: integration-test-orchestration-gpu
integration-test-orchestration-gpu: ## Run GPU-accelerated orchestration integration tests (requires NVIDIA GPU + Ollama)
	@echo "$(BLUE)Running GPU orchestration integration tests...$(NC)"
	@if ! command -v nvidia-smi > /dev/null 2>&1 || ! nvidia-smi > /dev/null 2>&1; then \
		echo "$(RED)Error: No NVIDIA GPU detected. GPU orchestration tests require an NVIDIA GPU.$(NC)"; \
		exit 1; \
	fi
	@GPU_NAME=$$(nvidia-smi --query-gpu=name --format=csv,noheader | head -1); \
	echo "$(GREEN)GPU detected: $$GPU_NAME$(NC)"
	@if ! command -v ollama > /dev/null 2>&1; then \
		echo "$(RED)Error: ollama not found. Install from https://ollama.com$(NC)"; \
		exit 1; \
	fi
	@MODEL="qwen3:1.7b"; \
	if ! ollama list 2>/dev/null | grep -q "$$MODEL"; then \
		echo "$(YELLOW)Pulling model $$MODEL...$(NC)"; \
		ollama pull "$$MODEL"; \
	fi; \
	echo "$(GREEN)Model $$MODEL available$(NC)"
	@GOFLAGS=-mod=mod $(GOTEST) -v -tags 'integration,rag,gpu' -timeout 10m ./test/integration/orchestration_gpu/...

# ==============================================================================
# Dynamic Integration Test Targets
# ==============================================================================
# Usage: make integration-test-<package>
#
# Core tests:     make integration-test-scheduler, integration-test-kms, etc.
# Go SDK tests:   make integration-test-sdk-go-scheduler, integration-test-sdk-go-kv
# JS SDK tests:   make integration-test-sdk-js-scheduler
#
# This pattern auto-discovers tests in:
#   - test/integration/<pkg>/
#   - sdk/go/test/integration/<pkg>_test.go
#   - sdk/javascript/test/integration/<pkg>.integration.test.js
# ==============================================================================

# Core integration tests: test/integration/<pkg>/
integration-test-%: ## Run integration tests for a specific package in Docker (e.g., make integration-test-scheduler)
	@PKG="$*"; \
	PKG_UPPER=$$(echo "$${PKG}" | tr '[:lower:]' '[:upper:]'); \
	if [ -d "test/integration/$${PKG}" ]; then \
		echo "$(BLUE)Running Docker integration tests for package: $${PKG}$(NC)"; \
		QUICBAAS_TEST_FILTER="./test/integration/$${PKG}/..." $(MAKE) integration-test; \
	elif ls test/integration/$${PKG}*_test.go 1>/dev/null 2>&1; then \
		echo "$(BLUE)Running Docker integration tests for '$${PKG}' (pattern: Test$${PKG_UPPER})$(NC)"; \
		QUICBAAS_TEST_FILTER="-run Test$${PKG_UPPER} ./test/integration/..." $(MAKE) integration-test; \
	else \
		echo "$(RED)Error: No integration tests found for '$${PKG}'$(NC)"; \
		echo "$(YELLOW)Searched:$(NC)"; \
		echo "  - test/integration/$${PKG}/"; \
		echo "  - test/integration/$${PKG}*_test.go"; \
		echo ""; \
		echo "$(YELLOW)Available integration test packages:$(NC)"; \
		for dir in test/integration/*/; do echo "  - $$(basename $$dir)"; done | head -20; \
		exit 1; \
	fi

# Go SDK integration tests: sdk/go/test/integration/<pkg>_test.go
integration-test-sdk-go-%: ## Run Go SDK integration tests for a specific feature (e.g., make integration-test-sdk-go-scheduler)
	@PKG="$*"; \
	if [ -f "sdk/go/test/integration/$${PKG}_test.go" ]; then \
		echo "$(BLUE)Running Go SDK integration tests for: $${PKG}$(NC)"; \
		cd sdk/go && go test -v -tags integration -timeout 5m -run ".*" ./test/integration/... -test.run "Test.*$$(echo $${PKG} | sed 's/.*/\u&/').*"; \
	elif ls sdk/go/test/integration/*$${PKG}*_test.go 1>/dev/null 2>&1; then \
		echo "$(BLUE)Running Go SDK integration tests matching: $${PKG}$(NC)"; \
		cd sdk/go && go test -v -tags integration -timeout 5m ./test/integration/...; \
	else \
		echo "$(RED)Error: No Go SDK integration tests found for '$${PKG}'$(NC)"; \
		echo "$(YELLOW)Searched: sdk/go/test/integration/$${PKG}_test.go$(NC)"; \
		echo ""; \
		echo "$(YELLOW)Available Go SDK integration tests:$(NC)"; \
		ls sdk/go/test/integration/*_test.go 2>/dev/null | xargs -I{} basename {} _test.go | sed 's/_test.go//' | sort -u | head -10; \
		exit 1; \
	fi

# JavaScript SDK integration tests: sdk/javascript/test/integration/<pkg>.integration.test.js
integration-test-sdk-js-%: ## Run JavaScript SDK integration tests for a specific feature (e.g., make integration-test-sdk-js-scheduler)
	@PKG="$*"; \
	if [ -f "sdk/javascript/test/integration/$${PKG}.integration.test.js" ]; then \
		echo "$(BLUE)Running JavaScript SDK integration tests for: $${PKG}$(NC)"; \
		cd sdk/javascript && npm run test:integration -- --testPathPattern="$${PKG}"; \
	else \
		echo "$(RED)Error: No JavaScript SDK integration tests found for '$${PKG}'$(NC)"; \
		echo "$(YELLOW)Searched: sdk/javascript/test/integration/$${PKG}.integration.test.js$(NC)"; \
		echo ""; \
		echo "$(YELLOW)Available JavaScript SDK integration tests:$(NC)"; \
		ls sdk/javascript/test/integration/*.integration.test.js 2>/dev/null | xargs -I{} basename {} .integration.test.js | head -10; \
		exit 1; \
	fi

# All Go SDK integration tests
.PHONY: integration-test-sdk-go
integration-test-sdk-go: ## Run ALL Go SDK integration tests
	@echo "$(BLUE)Running all Go SDK integration tests...$(NC)"
	@cd sdk/go && go test -v -tags integration -timeout 10m ./test/integration/...
	@echo "$(GREEN)✓ Go SDK integration tests passed$(NC)"

# All JavaScript SDK integration tests
.PHONY: integration-test-sdk-js
integration-test-sdk-js: ## Run ALL JavaScript SDK integration tests
	@echo "$(BLUE)Running all JavaScript SDK integration tests...$(NC)"
	@cd sdk/javascript && npm run test:integration
	@echo "$(GREEN)✓ JavaScript SDK integration tests passed$(NC)"

# All SDK integration tests
.PHONY: integration-test-sdk
integration-test-sdk: integration-test-sdk-go integration-test-sdk-js ## Run ALL SDK integration tests (Go + JavaScript)

# ==============================================================================
# Phase 9: Session Events, GUI, and Signal Bridge Integration Tests
# ==============================================================================

.PHONY: integration-test-session-attach
integration-test-session-attach: build-daemon ## Run session attach integration tests (wire-level SSE smoke against live daemon)
	@echo "$(BLUE)Running session attach integration tests...$(NC)"
	@$(MAKE) stop-local >/dev/null 2>&1 || true
	@rm -rf /tmp/quicbaas-chrometest-data && mkdir -p /tmp/quicbaas-chrometest-data
	@rm -f /tmp/quicbaas-chrometest.sock
	@nohup ./bin/quicbaasd start --http-port 18080 --grpc-port 0 \
		--unix-socket /tmp/quicbaas-chrometest.sock \
		--data-dir /tmp/quicbaas-chrometest-data --log-level warn \
		>/tmp/quicbaasd-itest.log 2>&1 & \
	echo $$! > /tmp/quicbaasd-itest.pid
	@echo "$(YELLOW)Waiting for daemon readiness (up to 75s cold boot)...$(NC)"
	@for i in $$(seq 1 75); do \
		if curl -sk -o /dev/null -w "%{http_code}" https://localhost:18080/api/v1/health/ready 2>/dev/null | grep -q 200; then \
			echo "$(GREEN)daemon ready$(NC)"; break; \
		fi; sleep 1; \
	done
	@QUICBAAS_URL=https://localhost:18080 $(GOTEST) -v -tags integration -timeout=2m ./test/integration/session_attach/...; \
		TEST_RESULT=$$?; \
		kill -9 $$(cat /tmp/quicbaasd-itest.pid 2>/dev/null) 2>/dev/null || true; \
		$(MAKE) stop-local >/dev/null 2>&1 || true; \
		exit $$TEST_RESULT

.PHONY: integration-test-gui
integration-test-gui: test-e2e ## Run GUI Playwright tests — alias for test-e2e

.PHONY: integration-test-signalbridge
integration-test-signalbridge: ## Run Signal Bridge integration tests (docker-compose: go-signal + quicbaasd + signalbridge)
	@echo "$(BLUE)Running Signal Bridge integration tests...$(NC)"
	@if [ -d "test/integration/signalbridge" ]; then \
		if ! command -v docker-compose > /dev/null 2>&1 && ! docker compose version > /dev/null 2>&1; then \
			echo "$(RED)Error: docker-compose not found. Install Docker Compose.$(NC)"; \
			exit 1; \
		fi; \
		echo "$(YELLOW)Starting Signal Bridge stack...$(NC)"; \
		cd test/integration/signalbridge && docker-compose up -d; \
		echo "$(YELLOW)Waiting for services to be healthy (max 30s)...$(NC)"; \
		WAIT_COUNT=0; \
		while [ $$WAIT_COUNT -lt 30 ]; do \
			if docker-compose ps 2>/dev/null | grep -q "healthy"; then \
				break; \
			fi; \
			echo "  Waiting..."; \
			sleep 1; \
			WAIT_COUNT=$$((WAIT_COUNT + 1)); \
		done; \
		echo "$(YELLOW)Running smoke test...$(NC)"; \
		go test -v -tags integration -timeout=5m ./...; \
		TEST_RESULT=$$?; \
		echo "$(YELLOW)Cleaning up Signal Bridge stack...$(NC)"; \
		docker-compose down --volumes --remove-orphans; \
		exit $$TEST_RESULT; \
	else \
		echo "$(YELLOW)test/integration/signalbridge/ not found - skipping$(NC)"; \
	fi

# ==============================================================================
# Phase F: Standalone + Cluster + Seal/Hardware integration test targets
# ==============================================================================

.PHONY: integration-test-standalone
integration-test-standalone: ## Phase F: Run standalone (no-Raft, in-process embedded qrdb engine) integration tests (no docker required)
	@echo "$(BLUE)Running Phase F standalone integration tests (in-process, no docker)...$(NC)"
	@# No -mod=mod: this repo is used inside a go.work workspace, where any -mod
	@# other than readonly/vendor is rejected outright, so the flag made the
	@# target unrunnable rather than permissive.
	@$(GOTEST) -v -tags integration -timeout 120s ./test/integration/standalone/...
	@echo "$(GREEN)✓ Phase F standalone integration tests PASSED$(NC)"

.PHONY: integration-test-cluster
integration-test-cluster: ## Phase F: Run 3+1-node Raft cluster integration tests in Docker (requires docker-compose.cluster.yml)
	@echo "$(BLUE)Running Phase F cluster integration tests (3+1-node qrdb Raft cluster with at-rest encryption)...$(NC)"
	@if ! docker compose version > /dev/null 2>&1 && ! command -v docker-compose > /dev/null 2>&1; then \
		echo "$(RED)Error: Docker Compose is not installed$(NC)"; \
		exit 1; \
	fi
	@COMPOSE_FILE="test/integration/docker-compose.cluster.yml"; \
	CLUSTER_DIR="test/integration"; \
	echo "$(YELLOW)Cleaning up any previous cluster-test containers and bind-mount data dirs...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans 2>/dev/null || true; \
	ABS_CLUSTER_DIR="$$PWD/$$CLUSTER_DIR"; \
	if [ -d "$$ABS_CLUSTER_DIR/cluster-data" ]; then \
		docker run --rm -v "$$ABS_CLUSTER_DIR/cluster-data:/cleanup" alpine sh -c "rm -rf /cleanup/*" 2>/dev/null || true; \
	fi; \
	rm -rf $$CLUSTER_DIR/cluster-data $$CLUSTER_DIR/cluster-logs 2>/dev/null || true; \
	echo "$(YELLOW)Pre-creating host bind-mount data and log dirs (wide perms so container can write and host can read)...$(NC)"; \
	for N in node1 node2 node3 node4; do \
		mkdir -p $$CLUSTER_DIR/cluster-data/$$N $$CLUSTER_DIR/cluster-logs/$$N; \
		chmod 0777 $$CLUSTER_DIR/cluster-data/$$N $$CLUSTER_DIR/cluster-logs/$$N; \
	done; \
	echo "$(YELLOW)Building cluster-test image...$(NC)"; \
	docker compose -f $$COMPOSE_FILE build 2>&1 | while IFS= read -r line; do echo "  $$line"; done; \
	echo "$(YELLOW)Starting 3-node quicbaas cluster (nodes 1-3, bootstrap group)...$(NC)"; \
	docker compose -f $$COMPOSE_FILE up -d quicbaas-node1 quicbaas-node2 quicbaas-node3; \
	echo "$(YELLOW)Waiting for all 3 cluster nodes to become ready (up to 120s)...$(NC)"; \
	DEADLINE=$$(( $$(date +%s) + 120 )); \
	while [ $$(date +%s) -lt $$DEADLINE ]; do \
		N1=$$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 http://localhost:8081/api/v1/health/ready 2>/dev/null || echo 0); \
		N2=$$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 http://localhost:8082/api/v1/health/ready 2>/dev/null || echo 0); \
		N3=$$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 http://localhost:8083/api/v1/health/ready 2>/dev/null || echo 0); \
		if [ "$$N1" = "200" ] && [ "$$N2" = "200" ] && [ "$$N3" = "200" ]; then \
			echo "$(GREEN)All 3 nodes ready (node1=$$N1, node2=$$N2, node3=$$N3)$(NC)"; \
			break; \
		fi; \
		echo "  node1=$$N1 node2=$$N2 node3=$$N3 (waiting...)"; \
		sleep 3; \
	done; \
	echo "$(YELLOW)Granting host read access to node data dirs (test runner needs to walk SST files)...$(NC)"; \
	docker run --rm -v "$$PWD/$$CLUSTER_DIR/cluster-data:/data" alpine sh -c \
		"chmod -R a+rX /data 2>/dev/null; true" || true; \
	echo "$(YELLOW)Starting node4 (dynamic-membership joiner — will join via AddNode API)...$(NC)"; \
	docker compose -f $$COMPOSE_FILE up -d quicbaas-node4; \
	echo "$(YELLOW)Adding node4 to the Raft cluster via REST AddNode API (trying all nodes to find the leader)...$(NC)"; \
	ADD_DEADLINE=$$(( $$(date +%s) + 45 )); \
	ADD_OK=0; \
	while [ $$(date +%s) -lt $$ADD_DEADLINE ]; do \
		for PORT in 8081 8082 8083; do \
			CODE=$$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 \
				-X POST http://localhost:$$PORT/api/v1/cluster/nodes \
				-H 'Content-Type: application/json' \
				-d '{"shard_id":1,"node_id":4,"address":"quicbaas-node4:63004"}' \
				2>/dev/null || echo 0); \
			if [ "$$CODE" = "200" ]; then \
				echo "$(GREEN)AddNode succeeded via node at port $$PORT$(NC)"; \
				ADD_OK=1; \
				break 2; \
			fi; \
		done; \
		echo "  AddNode not yet accepted (retrying in 3s)..."; \
		sleep 3; \
	done; \
	if [ $$ADD_OK -eq 0 ]; then \
		echo "$(RED)AddNode failed within 45s — dynamic membership test will likely fail$(NC)"; \
	fi; \
	echo "$(YELLOW)Waiting for node4 to become ready (up to 90s)...$(NC)"; \
	N4_DEADLINE=$$(( $$(date +%s) + 90 )); \
	N4_READY=0; \
	while [ $$(date +%s) -lt $$N4_DEADLINE ]; do \
		N4=$$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 http://localhost:8084/api/v1/health/ready 2>/dev/null || echo 0); \
		if [ "$$N4" = "200" ]; then \
			echo "$(GREEN)node4 ready$(NC)"; \
			N4_READY=1; \
			break; \
		fi; \
		echo "  node4=$$N4 (waiting...)"; \
		sleep 3; \
	done; \
	if [ $$N4_READY -eq 0 ]; then \
		echo "$(RED)node4 did not become ready within 60s$(NC)"; \
		docker compose -f $$COMPOSE_FILE logs quicbaas-node4 --tail=50; \
	fi; \
	echo "$(YELLOW)Granting host read access to all node data dirs (including node4, for at-rest test)...$(NC)"; \
	docker run --rm -v "$$ABS_CLUSTER_DIR/cluster-data:/data" alpine sh -c \
		"chmod -R a+rX /data 2>/dev/null; true" || true; \
	echo "$(YELLOW)Running cluster integration tests (including AtRest + DynamicMembership)...$(NC)"; \
	QUICBAAS_NODE1_URL="http://localhost:8081" \
	QUICBAAS_NODE2_URL="http://localhost:8082" \
	QUICBAAS_NODE3_URL="http://localhost:8083" \
	QUICBAAS_NODE4_URL="http://localhost:8084" \
	QUICBAAS_NODE1_DATADIR="$$PWD/$$CLUSTER_DIR/cluster-data/node1" \
	QUICBAAS_NODE2_DATADIR="$$PWD/$$CLUSTER_DIR/cluster-data/node2" \
	QUICBAAS_NODE3_DATADIR="$$PWD/$$CLUSTER_DIR/cluster-data/node3" \
	QUICBAAS_NODE4_DATADIR="$$PWD/$$CLUSTER_DIR/cluster-data/node4" \
	$(GOTEST) -v -tags integration -timeout 300s ./test/integration/cluster/...; \
	RESULT=$$?; \
	echo "$(BLUE)Stopping cluster stack...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans; \
	if [ -d "$$ABS_CLUSTER_DIR/cluster-data" ]; then \
		docker run --rm -v "$$ABS_CLUSTER_DIR/cluster-data:/cleanup" alpine sh -c "rm -rf /cleanup/*" 2>/dev/null || true; \
	fi; \
	rm -rf $$CLUSTER_DIR/cluster-data $$CLUSTER_DIR/cluster-logs 2>/dev/null || true; \
	if [ $$RESULT -eq 0 ]; then \
		echo "$(GREEN)✓ Phase F cluster integration tests PASSED$(NC)"; \
	else \
		echo "$(RED)✗ Phase F cluster integration tests FAILED (exit $$RESULT)$(NC)"; \
	fi; \
	exit $$RESULT

.PHONY: integration-test-cluster-failover
integration-test-cluster-failover: ## Phase F: Run leader-failover integration test only (requires a running cluster stack)
	@echo "$(BLUE)Running Phase F leader-failover integration test...$(NC)"
	@if [ -z "$(QUICBAAS_NODE1_URL)" ] || [ -z "$(QUICBAAS_NODE2_URL)" ] || [ -z "$(QUICBAAS_NODE3_URL)" ]; then \
		echo "$(RED)Error: QUICBAAS_NODE1_URL, NODE2_URL, NODE3_URL must be set$(NC)"; \
		exit 1; \
	fi
	@$(GOTEST) -v -tags integration -timeout 180s -run TestQRDBCluster_LeaderFailoverRecovers \
		./test/integration/cluster/...

.PHONY: integration-test-cluster-membership
integration-test-cluster-membership: ## Phase F: Run dynamic-membership integration test only (requires QUICBAAS_NODE4_URL)
	@echo "$(BLUE)Running Phase F dynamic-membership integration test...$(NC)"
	@$(GOTEST) -v -tags integration -timeout 180s -run TestQRDBCluster_DynamicMembership \
		./test/integration/cluster/...

.PHONY: integration-test-seal
integration-test-seal: ## Run cloud-backed barrier seal integration tests (awskms+vault emulators) in Docker
	@echo "$(BLUE)Running cloud-seal integration tests (awskms + vault)...$(NC)"
	@if ! docker compose version > /dev/null 2>&1 && ! command -v docker-compose > /dev/null 2>&1; then \
		echo "$(RED)Error: Docker Compose is not installed$(NC)"; \
		exit 1; \
	fi
	@COMPOSE_FILE="test/integration/docker-compose.seal.yml"; \
	echo "$(YELLOW)Cleaning up any previous seal-test containers...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans 2>/dev/null || true; \
	echo "$(YELLOW)Building seal-test image (quicbaasd + quicbaas built with -tags 'awskms vault')...$(NC)"; \
	docker compose -f $$COMPOSE_FILE build seal-test 2>&1 | while IFS= read -r line; do echo "  $$line"; done; \
	echo "$(YELLOW)Starting emulators (floci KMS + vault dev server)...$(NC)"; \
	docker compose -f $$COMPOSE_FILE up -d floci vault; \
	echo "$(YELLOW)Running seal integration tests...$(NC)"; \
	docker compose -f $$COMPOSE_FILE run --rm -T seal-test; \
	RESULT=$$?; \
	echo "$(BLUE)Stopping seal-test stack...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans; \
	if [ $$RESULT -eq 0 ]; then \
		echo "$(GREEN)✓ Cloud-seal integration tests PASSED$(NC)"; \
	else \
		echo "$(RED)✗ Cloud-seal integration tests FAILED (exit $$RESULT)$(NC)"; \
	fi; \
	exit $$RESULT

.PHONY: integration-test-tenantbarrier
integration-test-tenantbarrier: ## Run per-tenant barrier integration tests against the seal-ENABLED single-node harness in Docker
	@echo "$(BLUE)Running per-tenant barrier integration tests (seal-enabled harness)...$(NC)"
	@if ! docker compose version > /dev/null 2>&1 && ! command -v docker-compose > /dev/null 2>&1; then \
		echo "$(RED)Error: Docker Compose is not installed$(NC)"; \
		exit 1; \
	fi
	@COMPOSE_FILE="test/integration/docker-compose.seal.yml"; \
	if [ ! -f test/integration/certs/ca.key ]; then \
		echo "$(YELLOW)Generating integration mTLS/PIV certificates...$(NC)"; \
		rm -rf test/integration/certs; \
		go run test/integration/gen-certs/main.go test/integration/certs; \
	else \
		echo "$(YELLOW)Reusing existing test/integration/certs (delete to regenerate)$(NC)"; \
	fi; \
	echo "$(YELLOW)Cleaning up any previous tenantbarrier containers...$(NC)"; \
	docker compose -p tenantbarrier -f $$COMPOSE_FILE down --volumes --remove-orphans 2>/dev/null || true; \
	echo "$(YELLOW)Building tenantbarrier images (seal-enabled quicbaasd node + test runner)...$(NC)"; \
	docker compose -p tenantbarrier -f $$COMPOSE_FILE build tenantbarrier-node tenantbarrier-test 2>&1 | while IFS= read -r line; do echo "  $$line"; done; \
	echo "$(YELLOW)Starting seal-enabled node (passphrase auto-unseal)...$(NC)"; \
	docker compose -p tenantbarrier -f $$COMPOSE_FILE up -d tenantbarrier-node; \
	echo "$(YELLOW)Running per-tenant barrier integration tests...$(NC)"; \
	docker compose -p tenantbarrier -f $$COMPOSE_FILE run --rm -T tenantbarrier-test; \
	RESULT=$$?; \
	echo "$(BLUE)Stopping tenantbarrier stack...$(NC)"; \
	docker compose -p tenantbarrier -f $$COMPOSE_FILE down --volumes --remove-orphans; \
	if [ $$RESULT -eq 0 ]; then \
		echo "$(GREEN)✓ Per-tenant barrier integration tests PASSED$(NC)"; \
	else \
		echo "$(RED)✗ Per-tenant barrier integration tests FAILED (exit $$RESULT)$(NC)"; \
	fi; \
	exit $$RESULT

.PHONY: integration-test-seal-hardware
integration-test-seal-hardware: ## Run hardware-backed barrier seal integration tests (swtpm + SoftHSM2) in Docker
	@echo "$(BLUE)Running hardware-seal integration tests (tpm2/swtpm + pkcs11/SoftHSM2)...$(NC)"
	@if ! docker compose version > /dev/null 2>&1 && ! command -v docker-compose > /dev/null 2>&1; then \
		echo "$(RED)Error: Docker Compose is not installed$(NC)"; \
		exit 1; \
	fi
	@COMPOSE_FILE="test/integration/docker-compose.seal-hardware.yml"; \
	CLOUD_COMPOSE="test/integration/docker-compose.seal.yml"; \
	echo "$(YELLOW)Checking for concurrent cloud-seal stack...$(NC)"; \
	while docker compose -f $$CLOUD_COMPOSE ps --services --filter status=running 2>/dev/null | grep -q .; do \
		echo "$(YELLOW)  cloud-seal stack is still running; waiting 60s before building hardware stack...$(NC)"; \
		sleep 60; \
	done; \
	echo "$(YELLOW)Cleaning up any previous hardware-seal-test containers...$(NC)"; \
	docker compose -p seal-hw -f $$COMPOSE_FILE down --volumes --remove-orphans 2>/dev/null || true; \
	echo "$(YELLOW)Building hardware-seal-test image (quicbaasd + quicbaas built with -tags 'tpm2 pkcs11', CGO_ENABLED=1)...$(NC)"; \
	docker compose -p seal-hw -f $$COMPOSE_FILE build seal-hw-test 2>&1 | while IFS= read -r line; do echo "  $$line"; done; \
	echo "$(YELLOW)Starting hardware emulators (swtpm1)...$(NC)"; \
	docker compose -p seal-hw -f $$COMPOSE_FILE up -d swtpm1; \
	echo "$(YELLOW)Running hardware-seal integration tests...$(NC)"; \
	docker compose -p seal-hw -f $$COMPOSE_FILE run --rm -T seal-hw-test; \
	RESULT=$$?; \
	echo "$(BLUE)Stopping hardware-seal-test stack...$(NC)"; \
	docker compose -p seal-hw -f $$COMPOSE_FILE down --volumes --remove-orphans; \
	if [ $$RESULT -eq 0 ]; then \
		echo "$(GREEN)✓ Hardware-seal integration tests PASSED$(NC)"; \
	else \
		echo "$(RED)✗ Hardware-seal integration tests FAILED (exit $$RESULT)$(NC)"; \
	fi; \
	exit $$RESULT

.PHONY: build-desktop
# The `build-desktop*` targets are compatibility aliases for `build-cli`.
# The client binary ALWAYS has the desktop GUI embedded — this matches
# the go-qrdb (`make build-client`) and go-xkms (`make build-xkey`)
# conventions. No WITH_GUI flag: the client is one artifact with
# runtime dispatch between GUI and CLI modes based on args + display.

.PHONY: build-desktop
build-desktop: build-cli ## Alias for build-cli (client is always built with embedded GUI)

.PHONY: build-desktop-linux
build-desktop-linux: build-cli ## Alias for build-cli. We're already on Linux.

.PHONY: build-desktop-mac
build-desktop-mac: ## Cross-compile client for macOS — NOT YET WIRED
	@echo "$(YELLOW)build-desktop-mac: cross-compilation from Linux is not wired yet.$(NC)"
	@echo "$(YELLOW)Requirements: osxcross toolchain + macOS WebKit SDK.$(NC)"
	@echo "$(YELLOW)For now: check out the repo on a macOS host and run 'make build-cli'.$(NC)"
	@exit 1

.PHONY: build-desktop-windows
build-desktop-windows: ## Cross-compile client for Windows — NOT YET WIRED
	@echo "$(YELLOW)build-desktop-windows: cross-compilation from Linux is not wired yet.$(NC)"
	@echo "$(YELLOW)Requirements: mingw-w64 + Windows WebView2 runtime.$(NC)"
	@echo "$(YELLOW)For now: check out the repo on a Windows host and run 'make build-cli'.$(NC)"
	@exit 1

# Dynamic coverage targets - use: make coverage-<package>
# Examples: make coverage-xkms, make coverage-kms, make coverage-iam
# Nested packages use dashes: make coverage-api-grpc (maps to pkg/api/grpc)
coverage-%: ## Generate coverage report for a specific package (e.g., make coverage-xkms)
	@PKG="$*"; \
	PKG_DIR=$$(echo "$${PKG}" | tr '-' '/'); \
	SAFE_NAME=$$(echo "$${PKG}" | tr '/' '-'); \
	PKG_PATH="./pkg/$${PKG_DIR}/..."; \
	if [ ! -d "pkg/$${PKG_DIR}" ]; then \
		PKG_PATH="./cmd/$${PKG_DIR}/..."; \
		if [ ! -d "cmd/$${PKG_DIR}" ]; then \
			echo "$(RED)Error: Package '$${PKG_DIR}' not found in pkg/ or cmd/$(NC)"; \
			exit 1; \
		fi; \
	fi; \
	case "$${PKG_DIR}" in iam|iam/*) TAGS='$(IAM_TEST_TAGS)';; *) TAGS='$(TEST_TAGS)';; esac; \
	echo "$(BLUE)Generating coverage for $${PKG_PATH}...$(NC)"; \
	mkdir -p $(COVERAGE_DIR); \
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -tags "$${TAGS}" -coverprofile=$(COVERAGE_DIR)/coverage-$${SAFE_NAME}.out $${PKG_PATH}; \
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-$${SAFE_NAME}.out -o $(COVERAGE_DIR)/coverage-$${SAFE_NAME}.html; \
	echo "$(GREEN)Coverage report: $(COVERAGE_DIR)/coverage-$${SAFE_NAME}.html$(NC)"; \
	TOTAL=$$($(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-$${SAFE_NAME}.out | grep total | awk '{print $$3}'); \
	echo "$(GREEN)Total coverage for $${PKG_DIR}: $$TOTAL$(NC)"

# Dynamic benchmark targets - use: make bench-<package>
# Examples: make bench-xkms, make bench-kms, make bench-storage
bench-%: ## Run benchmarks for a specific package (e.g., make bench-xkms)
	@PKG="$*"; \
	PKG_PATH="./pkg/$${PKG}/..."; \
	if [ ! -d "pkg/$${PKG}" ]; then \
		PKG_PATH="./cmd/$${PKG}/..."; \
		if [ ! -d "cmd/$${PKG}" ]; then \
			echo "$(RED)Error: Package '$${PKG}' not found in pkg/ or cmd/$(NC)"; \
			exit 1; \
		fi; \
	fi; \
	case "$${PKG}" in iam|iam/*) TAGS='$(IAM_TEST_TAGS)';; *) TAGS='$(TEST_TAGS)';; esac; \
	echo "$(BLUE)Running benchmarks for $${PKG_PATH}...$(NC)"; \
	$(GOTEST) -tags "$${TAGS}" -bench=. -benchmem -run=^$$ $${PKG_PATH}

.PHONY: list-packages
list-packages: ## List all packages with available test targets
	@echo "$(BLUE)Dynamic Package Test Targets$(NC)"
	@echo ""
	@echo "$(GREEN)Unit Tests (test-<pkg>):$(NC)"
	@echo "  Usage: make test-<package>"
	@echo "  Available packages in pkg/:"
	@for dir in pkg/*/; do \
		if [ -d "$$dir" ]; then \
			pkg=$$(basename "$$dir"); \
			echo "    make test-$$pkg"; \
		fi; \
	done | head -15
	@echo "    ... ($(shell ls -d pkg/*/ 2>/dev/null | wc -l) total packages)"
	@echo ""
	@echo "$(GREEN)Core Integration Tests (integration-test-<pkg>):$(NC)"
	@echo "  Usage: make integration-test-<package>"
	@echo "  Available packages in test/integration/:"
	@for dir in test/integration/*/; do \
		if [ -d "$$dir" ]; then \
			pkg=$$(basename "$$dir"); \
			echo "    make integration-test-$$pkg"; \
		fi; \
	done | head -15
	@echo "    ... ($(shell ls -d test/integration/*/ 2>/dev/null | wc -l) total packages)"
	@echo ""
	@echo "$(GREEN)Go SDK Integration Tests (integration-test-sdk-go-<pkg>):$(NC)"
	@echo "  Usage: make integration-test-sdk-go-<package>"
	@echo "  Available tests in sdk/go/test/integration/:"
	@for file in sdk/go/test/integration/*_test.go; do \
		if [ -f "$$file" ]; then \
			pkg=$$(basename "$$file" _test.go); \
			echo "    make integration-test-sdk-go-$$pkg"; \
		fi; \
	done 2>/dev/null | head -10
	@echo ""
	@echo "$(GREEN)JavaScript SDK Integration Tests (integration-test-sdk-js-<pkg>):$(NC)"
	@echo "  Usage: make integration-test-sdk-js-<package>"
	@echo "  Available tests in sdk/javascript/test/integration/:"
	@for file in sdk/javascript/test/integration/*.integration.test.js; do \
		if [ -f "$$file" ]; then \
			pkg=$$(basename "$$file" .integration.test.js); \
			echo "    make integration-test-sdk-js-$$pkg"; \
		fi; \
	done 2>/dev/null | head -10
	@echo ""
	@echo "$(GREEN)Run all SDK tests:$(NC)"
	@echo "    make integration-test-sdk        # All SDK tests (Go + JS)"
	@echo "    make integration-test-sdk-go     # All Go SDK tests"
	@echo "    make integration-test-sdk-js     # All JavaScript SDK tests"
	@echo ""
	@echo "$(GREEN)Coverage (coverage-<pkg>):$(NC)"
	@echo "  Usage: make coverage-<package>"
	@echo ""
	@echo "$(GREEN)Benchmarks (bench-<pkg>):$(NC)"
	@echo "  Usage: make bench-<package>"

# Legacy target - deprecated, use list-packages instead
.PHONY: list-integration-packages
list-integration-packages: ## (Deprecated) Use list-packages instead
	@for dir in test/integration/*/; do \
		if [ -d "$$dir" ] && [ "$$(basename $$dir)" != "helpers" ]; then \
			pkg=$$(basename "$$dir"); \
			echo "    make integration-test-$$pkg"; \
		fi; \
	done
	@echo ""
	@echo "$(GREEN)Coverage (coverage-<pkg>):$(NC)"
	@echo "  Usage: make coverage-<package>"
	@echo "  Example: make coverage-dao, make coverage-kms"
	@echo ""
	@echo "$(GREEN)Benchmarks (bench-<pkg>):$(NC)"
	@echo "  Usage: make bench-<package>"
	@echo "  Example: make bench-dao, make bench-storage"

.PHONY: test-integration-tagged
test-integration-tagged: ## Run integration tests with build tag (in-process)
	@echo "$(BLUE)Running tagged integration tests...$(NC)"
	@START_TIME=$$(date +%s); \
	$(GOTEST) -v -tags integration -timeout=10m ./pkg/... ./cmd/...; \
	TEST_RESULT=$$?; \
	END_TIME=$$(date +%s); \
	ELAPSED=$$((END_TIME - START_TIME)); \
	MINUTES=$$((ELAPSED / 60)); \
	SECONDS=$$((ELAPSED % 60)); \
	echo ""; \
	if [ $$MINUTES -gt 0 ]; then \
		printf "$(GREEN)Total time: %dm %ds$(NC)\n" $$MINUTES $$SECONDS; \
	else \
		printf "$(GREEN)Total time: %ds$(NC)\n" $$SECONDS; \
	fi; \
	exit $$TEST_RESULT

# Path the compose files' `gomodauth` secret reads. Outside the repo on
# purpose: the images end with COPY . ., so a credentials file inside the build
# context would be baked into a layer (.dockerignore lists gomodauth* as a
# second line of defence).
GOMODAUTH_ENV ?= /tmp/quicbaas-gomodauth.env
export GOMODAUTH_ENV

.PHONY: stage-gomodauth
stage-gomodauth: ## Stage Gitea module-fetch credentials for the image builds
	@umask 077; : > "$(GOMODAUTH_ENV)"
	@if [ -n "$${GIT_CONFIG_KEY_0:-}" ]; then \
		umask 077; \
		{ \
			printf 'export GOPRIVATE=%s\n' "$${GOPRIVATE:-}"; \
			printf 'export GIT_CONFIG_COUNT=%s\n' "$${GIT_CONFIG_COUNT:-}"; \
			printf 'export GIT_CONFIG_KEY_0=%s\n' "$${GIT_CONFIG_KEY_0}"; \
			printf 'export GIT_CONFIG_VALUE_0=%s\n' "$${GIT_CONFIG_VALUE_0:-}"; \
		} > "$(GOMODAUTH_ENV)"; \
		echo "$(GREEN)Staged Gitea module credentials for the image build$(NC)"; \
	else \
		echo "$(YELLOW)No GIT_CONFIG_KEY_0 in the environment; image builds resolve modules from the public proxy$(NC)"; \
		echo "$(YELLOW)  (dependency-resolution.md section 6 -- the Gitea runners inject it)$(NC)"; \
	fi

# Teardown is armed by an EXIT trap before the first container exists, because
# the workflow no longer prunes the runner behind this job (#3): what this stack
# leaves behind now stays behind. The recipe can leave early from a build
# failure, an unhealthy cluster, a `set -e` trip or a cancelled job, and every
# one of those paths used to skip the `down` near the end. The trap covers all
# of them; TORN_DOWN keeps it from repeating a teardown an explicit path already
# did. It is best-effort on purpose — a teardown error must not overwrite the
# test result the caller is waiting for, and the explicit `down`s still report
# their own failures loudly.
.PHONY: integration-test
integration-test: ## Run integration tests in Docker (Ollama tests EXCLUDED by default; OLLAMA_CPU=1 or GPU_HOST=<host> to include)
	@echo "$(BLUE)Running Docker integration tests...$(NC)"
	@if ! command -v docker > /dev/null 2>&1; then \
		echo "$(RED)Error: Docker is not installed$(NC)"; \
		echo "$(YELLOW)Install Docker: https://docs.docker.com/get-docker/$(NC)"; \
		exit 1; \
	fi
	@if ! docker compose version > /dev/null 2>&1 && ! command -v docker-compose > /dev/null 2>&1; then \
		echo "$(RED)Error: Docker Compose is not installed$(NC)"; \
		echo "$(YELLOW)Install Docker Compose:$(NC)"; \
		echo "$(YELLOW)  - Docker Compose V2 (recommended): https://docs.docker.com/compose/install/$(NC)"; \
		echo "$(YELLOW)  - Or run: sudo apt-get install docker-compose-plugin$(NC)"; \
		exit 1; \
	fi
	@$(MAKE) --no-print-directory stage-gomodauth
	@START_TIME=$$(date +%s); \
	COMPOSE_FILE="test/integration/docker-compose.integration.yml"; \
	GPU_HOST="$(strip $(GPU_HOST))"; \
	OLLAMA_CPU="$(strip $(OLLAMA_CPU))"; \
	OLLAMA_ENABLED=0; \
	GPU_AVAILABLE=0; \
	START_LOCAL_OLLAMA=0; \
	export OLLAMA_URL="http://ollama:11434"; \
	export QUICBAAS_OLLAMA_BASE_URL="http://ollama:11434"; \
	export QUICBAAS_SERVER_EMBEDDED_AGENT_OLLAMA_URL="http://ollama:11434"; \
	export QUICBAAS_OLLAMA_ENABLED="false"; \
	if [ -n "$$GPU_HOST" ]; then \
		case "$$GPU_HOST" in *:*) : ;; *) GPU_HOST="$$GPU_HOST:11434" ;; esac; \
		echo "$(GREEN)  ✓ Using remote GPU Ollama host: $$GPU_HOST$(NC)"; \
		OLLAMA_ENABLED=1; \
		GPU_AVAILABLE=1; \
		export OLLAMA_URL="http://$$GPU_HOST"; \
		export QUICBAAS_OLLAMA_BASE_URL="http://$$GPU_HOST"; \
		export QUICBAAS_SERVER_EMBEDDED_AGENT_OLLAMA_URL="http://$$GPU_HOST"; \
		export QUICBAAS_OLLAMA_ENABLED="true"; \
	elif command -v nvidia-smi > /dev/null 2>&1 && nvidia-smi > /dev/null 2>&1; then \
		GPU_NAME=$$(nvidia-smi --query-gpu=name --format=csv,noheader | head -1); \
		GPU_MEM=$$(nvidia-smi --query-gpu=memory.total --format=csv,noheader | head -1); \
		GPU_IDX=$$(nvidia-smi --query-gpu=index --format=csv,noheader | head -1); \
		echo "$(GREEN)  ✓ NVIDIA GPU detected: $$GPU_NAME ($$GPU_MEM) [device $$GPU_IDX]$(NC)"; \
		export NVIDIA_DEVICE_ID=$${NVIDIA_DEVICE_ID:-$$GPU_IDX}; \
		COMPOSE_FILE="$$COMPOSE_FILE -f test/integration/docker-compose.gpu.yml"; \
		OLLAMA_ENABLED=1; \
		GPU_AVAILABLE=1; \
		START_LOCAL_OLLAMA=1; \
		export QUICBAAS_OLLAMA_ENABLED="true"; \
	elif [ "$$OLLAMA_CPU" = "1" ]; then \
		echo "$(YELLOW)  ⚠ OLLAMA_CPU=1 — running Ollama tests against local CPU container (serialized)$(NC)"; \
		OLLAMA_ENABLED=1; \
		GPU_AVAILABLE=0; \
		START_LOCAL_OLLAMA=1; \
		export QUICBAAS_OLLAMA_ENABLED="true"; \
	else \
		echo "$(YELLOW)  ⚠ No GPU_HOST, no local NVIDIA GPU, OLLAMA_CPU unset — EXCLUDING Ollama-dependent tests$(NC)"; \
		echo "$(YELLOW)    (run with OLLAMA_CPU=1 or GPU_HOST=<host> to include them)$(NC)"; \
	fi; \
	TEST_GPU=$$GPU_AVAILABLE; \
	OLLAMA_UP_SERVICES=""; \
	if [ $$START_LOCAL_OLLAMA -eq 1 ]; then OLLAMA_UP_SERVICES="ollama ollama-setup"; fi; \
	echo "$(BLUE)  Ollama mode: enabled=$$OLLAMA_ENABLED gpu=$$TEST_GPU local-container=$$START_LOCAL_OLLAMA url=$$OLLAMA_URL$(NC)"; \
	TORN_DOWN=0; \
	trap 'if [ $$TORN_DOWN -eq 0 ]; then echo "$(YELLOW)Tearing down the integration stack...$(NC)"; docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans >/dev/null 2>&1 || true; fi' EXIT; \
	cp go.mod /tmp/quicbaas-gomod-backup; \
	cp sdk/go/go.mod /tmp/quicbaas-sdk-gomod-backup; \
	echo "$(YELLOW)CLI binary and mTLS certificates are produced inside the stack$(NC)"; \
	echo "$(YELLOW)  CLI: baked into the test-runner image from tracked source$(NC)"; \
	echo "$(YELLOW)  certs: generated per run by the certs-init service into the$(NC)"; \
	echo "$(YELLOW)         quicbaas-certs named volume, which the nodes mount$(NC)"; \
	echo ""; \
	echo "$(YELLOW)Cleaning up any previous test containers and volumes...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans 2>/dev/null || true; \
	echo ""; \
	echo "$(YELLOW)Removing old integration test images to force complete rebuild...$(NC)"; \
	docker rmi -f integration-integration-test integration-test-node1 integration-test-node2 integration-test-node3 integration-test-node4 integration-test-node5 2>/dev/null || true; \
	echo ""; \
	echo "$(YELLOW)Building Docker images (forcing clean rebuild to ensure latest code)...$(NC)"; \
	echo "$(YELLOW)Build tags: $(INTEGRATION_BUILD_TAGS)$(NC)"; \
	set -o pipefail; \
	if ! docker compose -f $$COMPOSE_FILE build --no-cache --pull --build-arg BUILD_TAGS="$(strip $(INTEGRATION_BUILD_TAGS))" 2>&1 | while IFS= read -r line; do \
		echo "  $$line"; \
	done; then \
		echo "$(RED)Image build FAILED — refusing to run integration tests against stale images.$(NC)"; \
		exit 1; \
	fi; \
	echo ""; \
	echo "$(YELLOW)Starting integration test cluster...$(NC)"; \
	docker compose -f $$COMPOSE_FILE up -d test-node1 test-node2 test-node3 floci minio azurite fake-gcs-server $$OLLAMA_UP_SERVICES; \
	echo ""; \
	echo "$(YELLOW)Waiting for cluster nodes to be healthy (checking every 5s, max 120s)...$(NC)"; \
	WAIT_COUNT=0; \
	MAX_WAIT=24; \
	while [ $$WAIT_COUNT -lt $$MAX_WAIT ]; do \
		HEALTHY=0; \
		for node in test-node1 test-node2 test-node3; do \
			STATUS=$$(docker compose -f $$COMPOSE_FILE ps --format json $$node 2>/dev/null | grep -o '"Health":"[^"]*"' | cut -d'"' -f4 || echo "unknown"); \
			if [ "$$STATUS" = "healthy" ]; then \
				HEALTHY=$$((HEALTHY + 1)); \
			fi; \
		done; \
		ELAPSED=$$((WAIT_COUNT * 5)); \
		if [ $$HEALTHY -eq 3 ]; then \
			echo "$(GREEN)  ✓ All 3 nodes healthy after $${ELAPSED}s$(NC)"; \
			break; \
		else \
			echo "  [$${ELAPSED}s] $$HEALTHY/3 nodes healthy..."; \
		fi; \
		sleep 5; \
		WAIT_COUNT=$$((WAIT_COUNT + 1)); \
	done; \
	if [ $$WAIT_COUNT -ge $$MAX_WAIT ]; then \
		echo "$(RED)  ✗ Timeout waiting for cluster to become healthy$(NC)"; \
		echo "$(YELLOW)Node status:$(NC)"; \
		docker compose -f $$COMPOSE_FILE ps; \
		echo "$(YELLOW)Recent logs:$(NC)"; \
		docker compose -f $$COMPOSE_FILE logs --tail=20; \
		docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans; \
		TORN_DOWN=1; \
		exit 1; \
	fi; \
	echo ""; \
	echo "$(YELLOW)Verifying gRPC ports (50051) are ready...$(NC)"; \
	GRPC_WAIT=0; \
	GRPC_MAX_WAIT=30; \
	while [ $$GRPC_WAIT -lt $$GRPC_MAX_WAIT ]; do \
		GRPC_READY=0; \
		for node in test-node1 test-node2 test-node3; do \
			if docker compose -f $$COMPOSE_FILE exec -T $$node sh -c "nc -z localhost 50051" 2>/dev/null; then \
				GRPC_READY=$$((GRPC_READY + 1)); \
			fi; \
		done; \
		if [ $$GRPC_READY -eq 3 ]; then \
			echo "$(GREEN)  ✓ All gRPC ports ready after $${GRPC_WAIT}s$(NC)"; \
			break; \
		else \
			echo "  [$$GRPC_WAIT s] $$GRPC_READY/3 gRPC ports ready..."; \
		fi; \
		sleep 2; \
		GRPC_WAIT=$$((GRPC_WAIT + 2)); \
	done; \
	if [ $$GRPC_WAIT -ge $$GRPC_MAX_WAIT ]; then \
		echo "$(YELLOW)  ⚠ gRPC port check timed out, tests may fail for gRPC transport$(NC)"; \
	fi; \
	echo ""; \
	echo "$(YELLOW)Verifying MCP ports (8888) are ready...$(NC)"; \
	MCP_WAIT=0; \
	MCP_MAX_WAIT=15; \
	while [ $$MCP_WAIT -lt $$MCP_MAX_WAIT ]; do \
		MCP_READY=0; \
		for node in test-node1 test-node2 test-node3; do \
			if docker compose -f $$COMPOSE_FILE exec -T $$node sh -c "nc -z localhost 8888" 2>/dev/null; then \
				MCP_READY=$$((MCP_READY + 1)); \
			fi; \
		done; \
		if [ $$MCP_READY -eq 3 ]; then \
			echo "$(GREEN)  ✓ All MCP ports ready after $${MCP_WAIT}s$(NC)"; \
			break; \
		else \
			echo "  [$$MCP_WAIT s] $$MCP_READY/3 MCP ports ready..."; \
		fi; \
		sleep 2; \
		MCP_WAIT=$$((MCP_WAIT + 2)); \
	done; \
	if [ $$MCP_WAIT -ge $$MCP_MAX_WAIT ]; then \
		echo "$(YELLOW)  ⚠ MCP port check timed out, tests may fail for MCP transport$(NC)"; \
	fi; \
	echo ""; \
	echo "$(YELLOW)Waiting for storage backends (Floci, MinIO, Azurite) to be ready...$(NC)"; \
	STORAGE_WAIT=0; \
	STORAGE_MAX_WAIT=30; \
	while [ $$STORAGE_WAIT -lt $$STORAGE_MAX_WAIT ]; do \
		STORAGE_READY=0; \
		if docker compose -f $$COMPOSE_FILE exec -T floci sh -c "curl -sf http://localhost:4566/_floci/health | grep -q '\"s3\":\"running\"'" >/dev/null 2>&1; then \
			STORAGE_READY=$$((STORAGE_READY + 1)); \
		fi; \
		if docker compose -f $$COMPOSE_FILE exec -T minio sh -c "curl -sf http://localhost:9000/minio/health/live" >/dev/null 2>&1; then \
			STORAGE_READY=$$((STORAGE_READY + 1)); \
		fi; \
		if docker compose -f $$COMPOSE_FILE exec -T azurite sh -c "node -e \"require('http').get('http://localhost:10000',r=>{process.exit(r.statusCode===400?0:1)})\"" >/dev/null 2>&1; then \
			STORAGE_READY=$$((STORAGE_READY + 1)); \
		fi; \
		if [ $$STORAGE_READY -ge 3 ]; then \
			echo "$(GREEN)  ✓ All storage backends ready after $${STORAGE_WAIT}s$(NC)"; \
			break; \
		else \
			echo "  [$$STORAGE_WAIT s] $$STORAGE_READY/3 storage backends ready..."; \
		fi; \
		sleep 2; \
		STORAGE_WAIT=$$((STORAGE_WAIT + 2)); \
	done; \
	if [ $$STORAGE_WAIT -ge $$STORAGE_MAX_WAIT ]; then \
		echo "$(YELLOW)  ⚠ Some storage backends not ready, backup tests may fail$(NC)"; \
	fi; \
	echo ""; \
	if [ $$START_LOCAL_OLLAMA -eq 1 ]; then \
		echo "$(YELLOW)Waiting for local Ollama container to be ready...$(NC)"; \
		OLLAMA_WAIT=0; \
		OLLAMA_MAX_WAIT=120; \
		while [ $$OLLAMA_WAIT -lt $$OLLAMA_MAX_WAIT ]; do \
			if docker compose -f $$COMPOSE_FILE exec -T ollama ollama list >/dev/null 2>&1; then \
				echo "$(GREEN)  ✓ Ollama ready after $${OLLAMA_WAIT}s$(NC)"; \
				MODEL_READY=0; \
				if docker compose -f $$COMPOSE_FILE exec -T ollama ollama list 2>/dev/null | grep -q 'all-minilm'; then \
					echo "$(GREEN)  ✓ all-minilm model loaded$(NC)"; \
					MODEL_READY=1; \
				fi; \
				if [ $$MODEL_READY -eq 0 ]; then \
					echo "$(YELLOW)  ⚠ Model not yet loaded, pulling all-minilm...$(NC)"; \
					docker compose -f $$COMPOSE_FILE exec -T ollama ollama pull all-minilm 2>&1 | tail -1; \
				fi; \
				if docker compose -f $$COMPOSE_FILE exec -T ollama ollama list 2>/dev/null | grep -q 'tinyllama'; then \
					echo "$(GREEN)  ✓ tinyllama model loaded$(NC)"; \
				else \
					echo "$(YELLOW)  ⚠ tinyllama not loaded, pulling...$(NC)"; \
					docker compose -f $$COMPOSE_FILE exec -T ollama ollama pull tinyllama 2>&1 | tail -1; \
				fi; \
				break; \
			fi; \
			echo "  [$$OLLAMA_WAIT s] Ollama starting..."; \
			sleep 5; \
			OLLAMA_WAIT=$$((OLLAMA_WAIT + 5)); \
		done; \
		if [ $$OLLAMA_WAIT -ge $$OLLAMA_MAX_WAIT ]; then \
			echo "$(YELLOW)  ⚠ Ollama not ready, embedding/RAG tests may fail$(NC)"; \
		fi; \
	elif [ $$OLLAMA_ENABLED -eq 1 ]; then \
		echo "$(YELLOW)Probing remote Ollama host $$OLLAMA_URL ...$(NC)"; \
		if curl -sf "$$OLLAMA_URL/api/tags" >/dev/null 2>&1; then \
			echo "$(GREEN)  ✓ Remote Ollama reachable at $$OLLAMA_URL$(NC)"; \
		else \
			echo "$(YELLOW)  ⚠ Remote Ollama at $$OLLAMA_URL not reachable yet — tests will retry$(NC)"; \
		fi; \
	else \
		echo "$(BLUE)Ollama disabled — skipping Ollama readiness wait$(NC)"; \
	fi; \
	echo ""; \
	echo "$(YELLOW)Allowing brief stabilization period (5s)...$(NC)"; \
	sleep 5; \
	echo "$(GREEN)Cluster is ready for $(QUICBAAS_TEST_FILTER:-all) tests$(NC)"; \
	echo ""; \
	echo "$(BLUE)=========================================================================================================$(NC)"; \
	echo "$(BLUE)RUNNING INTEGRATION TESTS$(NC)"; \
	echo "$(BLUE)=========================================================================================================$(NC)"; \
	echo ""; \
	docker compose -f $$COMPOSE_FILE run --rm -T \
		-e QUICBAAS_TEST_FILTER="$(QUICBAAS_TEST_FILTER)" \
		-e QUICBAAS_TEST_OLLAMA="$$OLLAMA_ENABLED" \
		-e QUICBAAS_TEST_GPU="$$TEST_GPU" \
		-e OLLAMA_URL="$$OLLAMA_URL" \
		integration-test 2>&1 | while IFS= read -r line; do \
		echo "$$line"; \
	done; \
	DOCKER_RESULT=$${PIPESTATUS[0]}; \
	echo ""; \
	echo "$(BLUE)Stopping integration test cluster...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans; \
	TORN_DOWN=1; \
	echo "$(YELLOW)Restoring go.mod files (if modified by container)...$(NC)"; \
	if [ -f /tmp/quicbaas-gomod-backup ]; then cp /tmp/quicbaas-gomod-backup go.mod; fi; \
	if [ -f /tmp/quicbaas-sdk-gomod-backup ]; then cp /tmp/quicbaas-sdk-gomod-backup sdk/go/go.mod; fi; \
	rm -f /tmp/quicbaas-gomod-backup /tmp/quicbaas-sdk-gomod-backup; \
	if [ -d test/integration/certs ] && [ ! -w test/integration/certs ]; then \
		sudo rm -rf test/integration/certs; \
	else \
		rm -rf test/integration/certs; \
	fi; \
	END_TIME=$$(date +%s); \
	DOCKER_ELAPSED=$$((END_TIME - START_TIME)); \
	MINUTES=$$((DOCKER_ELAPSED / 60)); \
	SECONDS=$$((DOCKER_ELAPSED % 60)); \
	echo ""; \
	if [ $$DOCKER_RESULT -eq 0 ]; then \
		echo "$(GREEN)=========================================================================================================$(NC)"; \
		echo "$(GREEN)✓ Integration tests PASSED$(NC)"; \
	else \
		echo "$(RED)=========================================================================================================$(NC)"; \
		echo "$(RED)✗ Integration tests FAILED (exit code: $$DOCKER_RESULT)$(NC)"; \
	fi; \
	if [ $$MINUTES -gt 0 ]; then \
		echo "$(BLUE)Total time: $${MINUTES}m $${SECONDS}s$(NC)"; \
	else \
		echo "$(BLUE)Total time: $${SECONDS}s$(NC)"; \
	fi; \
	echo "$(BLUE)=========================================================================================================$(NC)"; \
	exit $$DOCKER_RESULT

.PHONY: integration-test-ollama-cpu
integration-test-ollama-cpu: ## Run the full integration suite INCLUDING Ollama tests against a local CPU container (serialized)
	@echo "$(BLUE)Running integration tests with local CPU Ollama (serialized)...$(NC)"
	@$(MAKE) integration-test OLLAMA_CPU=1

.PHONY: integration-test-ollama-gpu
integration-test-ollama-gpu: ## Run the full integration suite INCLUDING Ollama + GPU tests against a remote GPU host (requires GPU_HOST=<host>)
	@if [ -z "$(strip $(GPU_HOST))" ]; then \
		echo "$(RED)Error: integration-test-ollama-gpu requires GPU_HOST=<host> (e.g. make integration-test-ollama-gpu GPU_HOST=gpu01.lan)$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Running integration tests with remote GPU Ollama host: $(GPU_HOST)$(NC)"
	@$(MAKE) integration-test GPU_HOST="$(GPU_HOST)"

.PHONY: integration-test-minikube
integration-test-minikube: ## Run Minikube integration tests (auto-starts Minikube if needed)
	@echo "$(BLUE)=========================================================================================================$(NC)"
	@echo "$(BLUE)MINIKUBE INTEGRATION TESTS$(NC)"
	@echo "$(BLUE)=========================================================================================================$(NC)"
	@START_TIME=$$(date +%s); \
	if ! command -v minikube > /dev/null 2>&1; then \
		echo "$(YELLOW)Minikube not installed - skipping Minikube integration tests$(NC)"; \
		echo "$(YELLOW)Install Minikube to run these tests: https://minikube.sigs.k8s.io/docs/start/$(NC)"; \
		exit 0; \
	fi; \
	\
	MINIKUBE_WAS_RUNNING=false; \
	if minikube status >/dev/null 2>&1; then \
		echo "$(GREEN)✓ Minikube cluster already running$(NC)"; \
		MINIKUBE_WAS_RUNNING=true; \
	else \
		echo "$(YELLOW)Starting Minikube cluster (this may take 1-2 minutes)...$(NC)"; \
		minikube start --cpus=2 --memory=2048mb || (echo "$(RED)Failed to start Minikube$(NC)" && exit 1); \
		echo "$(GREEN)✓ Minikube cluster started$(NC)"; \
		MINIKUBE_WAS_RUNNING=false; \
	fi; \
	\
	NAMESPACE_EXISTED=false; \
	if kubectl get namespace quicbaas-test >/dev/null 2>&1; then \
		NAMESPACE_STATUS=$$(kubectl get namespace quicbaas-test -o jsonpath='{.status.phase}' 2>/dev/null); \
		if [ "$$NAMESPACE_STATUS" = "Terminating" ]; then \
			echo "$(YELLOW)Namespace is terminating, waiting for it to be fully deleted...$(NC)"; \
			WAIT_COUNT=0; \
			while kubectl get namespace quicbaas-test >/dev/null 2>&1 && [ $$WAIT_COUNT -lt 60 ]; do \
				sleep 2; \
				WAIT_COUNT=$$((WAIT_COUNT + 1)); \
				if [ $$((WAIT_COUNT % 5)) -eq 0 ]; then \
					echo "$(YELLOW)  Still waiting for namespace deletion ($$((WAIT_COUNT * 2))s)...$(NC)"; \
				fi; \
			done; \
			if kubectl get namespace quicbaas-test >/dev/null 2>&1; then \
				echo "$(RED)Namespace stuck in Terminating state. Attempting force cleanup...$(NC)"; \
				kubectl get namespace quicbaas-test -o json | jq '.spec.finalizers = []' | kubectl replace --raw /api/v1/namespaces/quicbaas-test/finalize -f - 2>/dev/null || true; \
				sleep 5; \
			fi; \
			echo "$(YELLOW)Creating test namespace...$(NC)"; \
			kubectl create namespace quicbaas-test; \
			echo "$(GREEN)✓ Test namespace created$(NC)"; \
			NAMESPACE_EXISTED=false; \
		else \
			echo "$(GREEN)✓ Test namespace already exists$(NC)"; \
			NAMESPACE_EXISTED=true; \
		fi; \
	else \
		echo "$(YELLOW)Creating test namespace...$(NC)"; \
		kubectl create namespace quicbaas-test; \
		echo "$(GREEN)✓ Test namespace created$(NC)"; \
	fi; \
	\
	STATEFULSET_EXISTED=false; \
	if kubectl get statefulset quicbaas -n quicbaas-test >/dev/null 2>&1; then \
		echo "$(GREEN)✓ StatefulSet already deployed$(NC)"; \
		STATEFULSET_EXISTED=true; \
	else \
		echo "$(YELLOW)Deploying test StatefulSet...$(NC)"; \
		kubectl apply -f pkg/discovery/testdata/quicbaas-statefulset.yaml; \
		echo "$(YELLOW)Waiting for pods to be created...$(NC)"; \
		for i in 1 2 3 4 5; do \
			if kubectl get pods -n quicbaas-test -l app=quicbaas 2>/dev/null | grep -q quicbaas; then \
				break; \
			fi; \
			echo "$(YELLOW)  Waiting for pods to appear (attempt $$i/5)...$(NC)"; \
			sleep 5; \
		done; \
		echo "$(YELLOW)Waiting for pods to be ready (timeout: 120s)...$(NC)"; \
		kubectl wait --for=condition=ready pod -l app=quicbaas -n quicbaas-test --timeout=120s || \
			(echo "$(RED)Pods failed to become ready. Status:$(NC)" && kubectl get pods -n quicbaas-test && exit 1); \
		echo "$(GREEN)✓ StatefulSet deployed and pods ready$(NC)"; \
	fi; \
	\
	echo ""; \
	echo "$(GREEN)Checking for test container image in Minikube...$(NC)"; \
	if minikube image ls | grep -q 'quicbaas-test:latest'; then \
		echo "$(GREEN)✓ Test image already loaded$(NC)"; \
	else \
		echo "$(YELLOW)Building and loading test container image...$(NC)"; \
		docker build -t quicbaas-test:latest -f pkg/discovery/testdata/Dockerfile.test . > /tmp/minikube-build.log 2>&1; \
		BUILD_RESULT=$$?; \
		if [ $$BUILD_RESULT -ne 0 ]; then \
			echo "$(RED)Failed to build test image. See /tmp/minikube-build.log$(NC)"; \
			echo "$(YELLOW)Attempting to skip go mod download and build with local modules...$(NC)"; \
		fi; \
		echo "$(YELLOW)Loading image into Minikube...$(NC)"; \
		minikube image load quicbaas-test:latest > /tmp/minikube-load.log 2>&1 || \
			(echo "$(RED)Failed to load image into Minikube. See /tmp/minikube-load.log$(NC)" && exit 1); \
		echo "$(GREEN)✓ Test image loaded into Minikube$(NC)"; \
	fi; \
	\
	echo "$(GREEN)Deploying test Job...$(NC)"; \
	kubectl delete job quicbaas-integration-test -n quicbaas-test --ignore-not-found=true; \
	kubectl apply -f pkg/discovery/testdata/test-job.yaml; \
	echo "$(YELLOW)Waiting for test Job to start (timeout: 60s)...$(NC)"; \
	kubectl wait --for=condition=ready pod -l app=quicbaas-test -n quicbaas-test --timeout=60s || \
		(echo "$(RED)Test pod failed to start$(NC)" && kubectl get pods -n quicbaas-test -l app=quicbaas-test && exit 1); \
	echo "$(GREEN)✓ Test Job started$(NC)"; \
	\
	echo ""; \
	echo "$(GREEN)Running Minikube integration tests in pod...$(NC)"; \
	echo "$(YELLOW)Streaming test output...$(NC)"; \
	echo ""; \
	TEST_RESULT=0; \
	POD_NAME=$$(kubectl get pods -n quicbaas-test -l app=quicbaas-test -o jsonpath='{.items[0].metadata.name}'); \
	kubectl logs -f $$POD_NAME -n quicbaas-test || true; \
	kubectl wait --for=condition=complete job/quicbaas-integration-test -n quicbaas-test --timeout=5m || TEST_RESULT=$$?; \
	if [ $$TEST_RESULT -ne 0 ]; then \
		echo ""; \
		echo "$(RED)Tests failed or timed out. Checking job status...$(NC)"; \
		kubectl describe job quicbaas-integration-test -n quicbaas-test; \
		kubectl get pods -n quicbaas-test -l app=quicbaas-test; \
	fi; \
	\
	echo ""; \
	echo "$(BLUE)Cleaning up test resources...$(NC)"; \
	if [ "$$NAMESPACE_EXISTED" = "false" ]; then \
		echo "$(YELLOW)Deleting test Job first to speed up termination...$(NC)"; \
		kubectl delete job quicbaas-integration-test -n quicbaas-test --ignore-not-found=true --wait=true --timeout=30s 2>/dev/null || true; \
		echo "$(YELLOW)Deleting test namespace (created by this run)...$(NC)"; \
		kubectl delete namespace quicbaas-test --wait=true --timeout=60s 2>&1 | grep -v "deleting" || true; \
		CLEANUP_WAIT=0; \
		while kubectl get namespace quicbaas-test >/dev/null 2>&1 && [ $$CLEANUP_WAIT -lt 30 ]; do \
			sleep 2; \
			CLEANUP_WAIT=$$((CLEANUP_WAIT + 1)); \
			if [ $$((CLEANUP_WAIT % 5)) -eq 0 ]; then \
				echo "$(YELLOW)  Waiting for namespace deletion ($$((CLEANUP_WAIT * 2))s)...$(NC)"; \
			fi; \
		done; \
		if kubectl get namespace quicbaas-test >/dev/null 2>&1; then \
			echo "$(YELLOW)Namespace deletion taking longer than expected, continuing...$(NC)"; \
			echo "$(YELLOW)Note: Run 'make integration-test-minikube-cleanup' if needed$(NC)"; \
		else \
			echo "$(GREEN)✓ Test namespace deleted$(NC)"; \
		fi; \
	else \
		echo "$(YELLOW)Preserving test namespace (existed before this run)$(NC)"; \
		echo "$(YELLOW)Cleaning up test Job only...$(NC)"; \
		kubectl delete job quicbaas-integration-test -n quicbaas-test --ignore-not-found=true --wait=true --timeout=30s 2>/dev/null || true; \
	fi; \
	\
	if [ "$$MINIKUBE_WAS_RUNNING" = "false" ]; then \
		echo "$(YELLOW)Stopping Minikube cluster (started by this run)...$(NC)"; \
		minikube stop; \
		echo "$(GREEN)✓ Minikube cluster stopped$(NC)"; \
	else \
		echo "$(YELLOW)Preserving Minikube cluster (was running before this run)$(NC)"; \
	fi; \
	\
	END_TIME=$$(date +%s); \
	MINIKUBE_ELAPSED=$$((END_TIME - START_TIME)); \
	echo "$(BLUE)=========================================================================================================$(NC)"; \
	if [ $$TEST_RESULT -eq 0 ]; then \
		echo "$(GREEN)✓ Minikube integration tests PASSED$(NC)"; \
	else \
		echo "$(RED)✗ Minikube integration tests FAILED$(NC)"; \
	fi; \
	echo "$(BLUE)=========================================================================================================$(NC)"; \
	exit $$TEST_RESULT

.PHONY: integration-test-minikube-cleanup
integration-test-minikube-cleanup: ## Clean up Minikube test resources (namespace only)
	@echo "$(BLUE)Cleaning up Minikube test resources...$(NC)"
	@if command -v kubectl > /dev/null 2>&1; then \
		echo "$(YELLOW)Deleting test Job first...$(NC)"; \
		kubectl delete job quicbaas-integration-test -n quicbaas-test --ignore-not-found=true --wait=true --timeout=30s 2>/dev/null || true; \
		echo "$(YELLOW)Deleting test namespace...$(NC)"; \
		kubectl delete namespace quicbaas-test --ignore-not-found=true --wait=true --timeout=60s; \
		CLEANUP_WAIT=0; \
		while kubectl get namespace quicbaas-test >/dev/null 2>&1 && [ $$CLEANUP_WAIT -lt 30 ]; do \
			sleep 2; \
			CLEANUP_WAIT=$$((CLEANUP_WAIT + 1)); \
		done; \
		if kubectl get namespace quicbaas-test >/dev/null 2>&1; then \
			echo "$(YELLOW)Namespace stuck in Terminating state. Attempting force cleanup...$(NC)"; \
			kubectl get namespace quicbaas-test -o json | jq '.spec.finalizers = []' | kubectl replace --raw /api/v1/namespaces/quicbaas-test/finalize -f - 2>/dev/null || true; \
		fi; \
		echo "$(GREEN)✓ Test namespace deleted (if it existed)$(NC)"; \
	else \
		echo "$(YELLOW)kubectl not found - skipping cleanup$(NC)"; \
	fi

.PHONY: integration-test-minikube-full-cleanup
integration-test-minikube-full-cleanup: integration-test-minikube-cleanup ## Full cleanup (namespace + stop Minikube)
	@echo "$(BLUE)Stopping Minikube cluster...$(NC)"
	@if command -v minikube > /dev/null 2>&1; then \
		minikube stop 2>/dev/null || echo "$(YELLOW)Minikube already stopped$(NC)"; \
		echo "$(GREEN)✓ Minikube cluster stopped$(NC)"; \
	else \
		echo "$(YELLOW)Minikube not found - skipping$(NC)"; \
	fi

.PHONY: perf-test
perf-test: ## Run all performance tests
	@echo "$(BLUE)Running QuicBaaS performance tests...$(NC)"
	$(GOTEST) -bench=. -benchtime=30s -benchmem ./test/performance/quicbaas/
	@echo "$(GREEN)Performance tests completed$(NC)"

.PHONY: perf-test-quick
perf-test-quick: ## Run quick performance tests (10s)
	@echo "$(BLUE)Running quick performance tests...$(NC)"
	$(GOTEST) -bench=. -benchtime=10s ./test/performance/quicbaas/
	@echo "$(GREEN)Quick performance tests completed$(NC)"

.PHONY: perf-test-all
perf-test-all: ## Run all performance tests including comparisons
	@echo "$(BLUE)Running all performance tests...$(NC)"
	$(GOTEST) -bench=. -benchtime=30s -benchmem ./test/performance/...
	@echo "$(GREEN)All performance tests completed$(NC)"

.PHONY: test-integration
test-integration: integration-test ## Alias for integration-test (deprecated, use integration-test)

.PHONY: test-docker
test-docker: ## Run tests in Docker environment
	@echo "$(BLUE)Running tests in Docker environment...$(NC)"
	@$(DOCKER_COMPOSE) -f test/integration/docker-compose.test.yml build
	@$(DOCKER) run --rm $(DOCKER_IMAGE):dev go test -v -race -short ./...

# Removed duplicate/circular integration-test alias to avoid recursion

##@ Local Development Loop

.PHONY: run-local
run-local: build-daemon ## Start local daemon (quicbaasd) for development
	@echo "$(BLUE)Starting QuicBaaS daemon...$(NC)"
	@rm -rf /tmp/quicbaas-chrometest-data
	@mkdir -p /tmp/quicbaas-chrometest-data
	@rm -f /tmp/quicbaas-chrometest.sock
	@echo "$(YELLOW)Data dir: /tmp/quicbaas-chrometest-data$(NC)"
	@echo "$(YELLOW)Unix socket: /tmp/quicbaas-chrometest.sock$(NC)"
	@echo "$(YELLOW)HTTP port: 18080$(NC)"
	@echo "$(YELLOW)Web UI: http://localhost:18080/ui/$(NC)"
	@./bin/quicbaasd start --http-port 18080 --grpc-port 0 --unix-socket /tmp/quicbaas-chrometest.sock --data-dir /tmp/quicbaas-chrometest-data --log-level info

.PHONY: stop-local
stop-local: ## Stop local daemon and clean up zombie processes
	@echo "$(BLUE)Stopping QuicBaaS daemon...$(NC)"
	@pkill -f 'bin/quicbaasd start' || true
	@echo "$(YELLOW)Waiting 2s for graceful shutdown...$(NC)"
	@sleep 2
	@echo "$(YELLOW)Killing any zombie QUIC listener on UDP :10443...$(NC)"
	@lsof -i :10443 2>/dev/null | awk 'NR==2{print $$2}' | xargs -r kill -9 2>/dev/null || true
	@echo "$(GREEN)Daemon stopped$(NC)"

.PHONY: test-sdk-ts
test-sdk-ts: ## Run TypeScript SDK unit tests
	@echo "$(BLUE)Running TypeScript SDK tests...$(NC)"
	@cd sdk/typescript && npx vitest run
	@echo "$(GREEN)TypeScript SDK tests passed$(NC)"

.PHONY: test-frontend
test-frontend: ## Run frontend unit tests
	@echo "$(BLUE)Running frontend tests...$(NC)"
	@cd cmd/quicbaas/frontend && npm test
	@echo "$(GREEN)Frontend tests passed$(NC)"

.PHONY: test-e2e
test-e2e: build-ui build-daemon ## Run Playwright E2E tests — no ACP backend required (0 skipped, 0 failed)
	@echo "$(BLUE)Running Playwright E2E tests (chromium-no-backend project)...$(NC)"
	@$(MAKE) stop-local >/dev/null 2>&1 || true
	@rm -rf /tmp/quicbaas-chrometest-data && mkdir -p /tmp/quicbaas-chrometest-data
	@rm -f /tmp/quicbaas-chrometest.sock
	@nohup ./bin/quicbaasd start --http-port 18080 --grpc-port 0 \
		--unix-socket /tmp/quicbaas-chrometest.sock \
		--data-dir /tmp/quicbaas-chrometest-data --log-level warn \
		--embedded-agent --embedded-agent-backend ollama \
		>/tmp/quicbaasd-e2e.log 2>&1 & \
	echo $$! > /tmp/quicbaasd-e2e.pid
	@echo "$(YELLOW)Waiting for daemon readiness (up to 75s cold boot)...$(NC)"
	@for i in $$(seq 1 75); do \
		if curl -sk -o /dev/null -w "%{http_code}" https://localhost:18080/api/v1/health/ready 2>/dev/null | grep -q 200; then \
			echo "$(GREEN)daemon ready$(NC)"; break; \
		fi; sleep 1; \
	done
	@cd cmd/quicbaas/frontend && \
		NODE_EXTRA_CA_CERTS=/tmp/quicbaas-chrometest-data/certs/ca-chain.crt \
		QUICBAAS_UI_URL=https://localhost:18080/ui/ \
		QUICBAAS_DAEMON_LOG=/tmp/quicbaasd-e2e.log \
		npx playwright test --project=chromium-no-backend; \
		TEST_RESULT=$$?; \
		kill -9 $$(cat /tmp/quicbaasd-e2e.pid 2>/dev/null) 2>/dev/null || true; \
		$(MAKE) stop-local >/dev/null 2>&1 || true; \
		exit $$TEST_RESULT

.PHONY: test-e2e-acp
test-e2e-acp: build-ui build-daemon ## Run Playwright session lifecycle tests — requires claude/codex/opencode on PATH
	@echo "$(BLUE)Running Playwright ACP session lifecycle tests (chromium-acp project)...$(NC)"
	@for backend in claude codex opencode; do \
		if command -v $$backend >/dev/null 2>&1; then \
			ACP_BACKEND=$$backend; break; \
		fi; \
	done; \
	if [ -z "$$ACP_BACKEND" ]; then \
		echo "$(RED)Error: no ACP backend found on PATH. Install claude, codex, or opencode first.$(NC)"; \
		exit 1; \
	fi; \
	echo "$(GREEN)Found ACP backend: $$ACP_BACKEND$(NC)"
	@$(MAKE) stop-local >/dev/null 2>&1 || true
	@rm -rf /tmp/quicbaas-chrometest-data && mkdir -p /tmp/quicbaas-chrometest-data
	@rm -f /tmp/quicbaas-chrometest.sock
	@nohup ./bin/quicbaasd start --http-port 18080 --grpc-port 0 \
		--unix-socket /tmp/quicbaas-chrometest.sock \
		--data-dir /tmp/quicbaas-chrometest-data --log-level info \
		--embedded-agent --embedded-agent-backend claude \
		>/tmp/quicbaasd-e2e-acp.log 2>&1 & \
	echo $$! > /tmp/quicbaasd-e2e-acp.pid
	@echo "$(YELLOW)Waiting for daemon readiness (up to 75s cold boot)...$(NC)"
	@for i in $$(seq 1 75); do \
		if curl -sk -o /dev/null -w "%{http_code}" https://localhost:18080/api/v1/health/ready 2>/dev/null | grep -q 200; then \
			echo "$(GREEN)daemon ready$(NC)"; break; \
		fi; sleep 1; \
	done
	@cd cmd/quicbaas/frontend && \
		NODE_EXTRA_CA_CERTS=/tmp/quicbaas-chrometest-data/certs/ca-chain.crt \
		QUICBAAS_UI_URL=https://localhost:18080/ui/ \
		QUICBAAS_DAEMON_LOG=/tmp/quicbaasd-e2e-acp.log \
		QUICBAAS_E2E_BACKEND=acp \
		npx playwright test --project=chromium-acp; \
		TEST_RESULT=$$?; \
		kill -9 $$(cat /tmp/quicbaasd-e2e-acp.pid 2>/dev/null) 2>/dev/null || true; \
		$(MAKE) stop-local >/dev/null 2>&1 || true; \
		exit $$TEST_RESULT

.PHONY: test-e2e-docker
test-e2e-docker: ## Run the Playwright e2e suite fully in Docker (host needs only Docker)
	@echo "$(BLUE)Running dockerized Playwright e2e (chromium-no-backend)...$(NC)"
	@COMPOSE_FILE="test/e2e/docker-compose.e2e.yml"; \
	set -o pipefail; \
	if ! docker compose -f $$COMPOSE_FILE build e2e 2>&1 | while IFS= read -r line; do echo "  $$line"; done; then \
		echo "$(RED)Image build FAILED — refusing to run e2e against a stale cached image.$(NC)"; \
		exit 1; \
	fi; \
	docker compose -f $$COMPOSE_FILE run --rm e2e; \
	RESULT=$$?; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans >/dev/null 2>&1 || true; \
	exit $$RESULT

.PHONY: test-e2e-acp-docker
test-e2e-acp-docker: ## Run the Playwright ACP live suite in Docker (opencode + ollama on a GPU host). Requires GPU_HOST=<host>.
	@if [ -z "$(strip $(GPU_HOST))" ]; then \
		echo "$(RED)Error: GPU_HOST is required (e.g. GPU_HOST=198.51.100.91). The ACP suite runs opencode against a remote ollama.$(NC)"; \
		exit 1; \
	fi; \
	GPU_HOST="$(strip $(GPU_HOST))"; \
	case "$$GPU_HOST" in *:*) : ;; *) GPU_HOST="$$GPU_HOST:11434" ;; esac; \
	echo "$(BLUE)Running dockerized Playwright ACP e2e (opencode + ollama @ $$GPU_HOST)...$(NC)"; \
	export QUICBAAS_E2E_SANDBOX="$(strip $(QUICBAAS_E2E_SANDBOX))"; \
	export QUICBAAS_E2E_SANDBOX="$${QUICBAAS_E2E_SANDBOX:-0}"; \
	export QUICBAAS_E2E_SANDBOX_IMAGE="quicbaas-e2e-sandbox-agent:latest"; \
	if [ "$$QUICBAAS_E2E_SANDBOX" = "1" ]; then \
		echo "$(BLUE)Container-sandbox mode: building sandbox agent image $$QUICBAAS_E2E_SANDBOX_IMAGE (opencode + ollama @ $$GPU_HOST)...$(NC)"; \
		docker build -f test/e2e/Dockerfile.sandbox-agent \
			--build-arg OLLAMA_URL="http://$$GPU_HOST" \
			--build-arg OPENCODE_MODEL="qwen3:1.7b" \
			-t "$$QUICBAAS_E2E_SANDBOX_IMAGE" test/e2e \
			2>&1 | while IFS= read -r line; do echo "  $$line"; done; \
	fi; \
	COMPOSE_FILE="test/e2e/docker-compose.e2e.yml"; \
	E2E_PROJECT=chromium-acp E2E_BACKEND=opencode E2E_OLLAMA_URL="http://$$GPU_HOST" \
	docker compose -f $$COMPOSE_FILE -f test/e2e/docker-compose.e2e-acp.yml build e2e 2>&1 | while IFS= read -r line; do echo "  $$line"; done; \
	E2E_PROJECT=chromium-acp E2E_BACKEND=opencode E2E_OLLAMA_URL="http://$$GPU_HOST" \
	docker compose -f $$COMPOSE_FILE -f test/e2e/docker-compose.e2e-acp.yml run --rm e2e; \
	RESULT=$$?; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans >/dev/null 2>&1 || true; \
	exit $$RESULT

.PHONY: e2e-test
e2e-test: ## Run end-to-end tests
	@echo "$(BLUE)Running E2E tests...$(NC)"
	$(GOTEST) -v -race -tags e2e ./$(TEST_DIR)/e2e/...

.PHONY: test-all
test-all: test integration-test e2e-test ## Run all tests (except FIPS - use 'make test-fips' separately)
	@echo "$(GREEN)✓ All tests passed!$(NC)"

.PHONY: benchmark
benchmark: ## Run benchmarks (tunable benchtime via BENCHTIME, default 100ms)
	@echo "$(BLUE)Running benchmarks...$(NC)"
	# Serialize package execution to avoid port conflicts; shorten benchtime to avoid timeouts
	$(GOTEST) -p=1 -count=1 -tags '$(TEST_TAGS)' -benchtime=$(BENCHTIME) -bench=. -benchmem -run=^$$ ./pkg/... ./cmd/...

.PHONY: benchmark-quick
benchmark-quick: ## Run quick benchmarks (1 iteration per bench)
	@echo "$(BLUE)Running quick benchmarks...$(NC)"
	$(GOTEST) -p=1 -count=1 -tags '$(TEST_TAGS)' -benchtime=1x -bench=. -benchmem -run=^$$ ./pkg/... ./cmd/...

.PHONY: benchmark-full
benchmark-full: ## Run full-length benchmarks (default Go benchtime)
	@echo "$(BLUE)Running full-length benchmarks...$(NC)"
	$(GOTEST) -p=1 -count=1 -tags '$(TEST_TAGS)' -bench=. -benchmem -run=^$$ ./pkg/... ./cmd/...

.PHONY: benchmark-fips
benchmark-fips: ## Run benchmarks in FIPS mode
	@echo "$(BLUE)Running benchmarks in FIPS 140-3 mode...$(NC)"
	@echo "$(YELLOW)Using GOFIPS140=latest (Go 1.24+)$(NC)"
	GOFIPS140=latest $(GOTEST) -bench=. -benchmem -tags fips ./pkg/fips/...

##@ Code Quality

.PHONY: lint
lint: ## Run linters
	@echo "$(BLUE)Running linters...$(NC)"
	@test -x $(GOLINT) || (echo "$(RED)golangci-lint not installed. Run: make install-tools$(NC)" && exit 1)
	# 20m, not the template's 5m. This module is ~526k lines across ~1300 files
	# and every linter runs over the full tag set; on a contended runner the
	# pass took 16 minutes on 2026-08-14 and was killed by the deadline after
	# it had already printed "0 issues" -- a green analysis reported as a red
	# job. The budget exists to catch a hang, so it needs to sit above the
	# honest worst case rather than near the median.
	$(GOLINT) run --timeout=20m --build-tags '$(LINT_BUILD_TAGS)' ./...

.PHONY: coverage
coverage: ## Generate coverage report (unit only; excludes generated code)
	@printf "$(BLUE)Generating coverage report...$(NC)\n"
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -p 2 -tags '$(TEST_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-raw.out $$(go list -tags '$(TEST_TAGS)' ./pkg/... ./cmd/... | grep -v '/proto$$')
	@printf "$(BLUE)Filtering out non-production code (generated, mocks, stubs, test helpers)...$(NC)\n"
	@head -1 $(COVERAGE_DIR)/coverage-raw.out > $(COVERAGE_DIR)/coverage.out
	@grep -v '\.pb\.go:' $(COVERAGE_DIR)/coverage-raw.out | \
		grep -v '_grpc\.pb\.go:' | \
		grep -v '/proto/' | \
		grep -v '/mocks/' | \
		grep -v '/integration/' | \
		grep -v 'mocks\.go:' | \
		grep -v '_stub\.go:' | \
		grep -v 'stub\.go:' | \
		grep -v 'test_helpers\.go:' | \
		grep -v '_test_helpers\.go:' | \
		grep -v '/testing\.go:' | \
		grep -v 'fixtures\.go:' | \
		tail -n +2 >> $(COVERAGE_DIR)/coverage.out || true
	@$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "$(GREEN)Coverage report: $(COVERAGE_DIR)/coverage.html$(NC)"
	@echo ""
	@echo "$(GREEN)========================================$(NC)"
	@TOTAL=$$($(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage.out | grep total | awk '{print $$3}'); \
	echo "$(GREEN)  TOTAL COVERAGE: $$TOTAL$(NC)"
	@echo "$(GREEN)========================================$(NC)"

# ==============================================================================
# Coverage Gate (hard threshold — mirrors go-qrdb's COVER_MIN / check-coverage)
# ==============================================================================
# COVER_MIN is the hard floor for total unit-test coverage. `check-coverage`
# fails the build (exit 1) when aggregate coverage across ./pkg/... and
# ./cmd/... drops below it. The `coverage` target it depends on already excludes
# generated *.pb.go, mocks, stubs, and test helpers from the profile.
#
# Threshold choice: the project goal is 90% (see CLAUDE.md). Measured live
# total on 2026-08-03 was 91.5%, so the floor sits at the goal itself with
# ~1.5pt headroom for normal churn.
COVER_MIN ?= 90

.PHONY: check-coverage
check-coverage: coverage ## Fail the build when total coverage is below COVER_MIN
	@TOTAL=$$($(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	printf "$(BLUE)Coverage gate: %s%% (minimum %s%%)$(NC)\n" "$$TOTAL" "$(COVER_MIN)"; \
	if awk -v t="$$TOTAL" -v m="$(COVER_MIN)" 'BEGIN { exit !(t+0 < m+0) }'; then \
		printf "$(RED)FAIL: total coverage %s%% is below minimum %s%%$(NC)\n" "$$TOTAL" "$(COVER_MIN)"; \
		exit 1; \
	fi; \
	printf "$(GREEN)Coverage gate passed (%s%% >= %s%%)$(NC)\n" "$$TOTAL" "$(COVER_MIN)"

.PHONY: coverage-fips
coverage-fips: ## Generate coverage report for FIPS mode
	@echo "$(BLUE)Generating FIPS coverage report...$(NC)"
	@echo "$(YELLOW)Using GOFIPS140=latest (Go 1.24+)$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' GOFIPS140=latest $(GOTEST) -coverprofile=$(COVERAGE_DIR)/coverage-fips.out -tags fips ./pkg/fips/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-fips.out -o $(COVERAGE_DIR)/coverage-fips.html
	@echo "$(GREEN)FIPS coverage report: $(COVERAGE_DIR)/coverage-fips.html$(NC)"
	@echo "$(BLUE)Total FIPS coverage:$(NC)"
	@$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-fips.out | grep total | awk '{print "  " $$3}'

.PHONY: coverage-report
coverage-report: coverage ## Show coverage in browser
	@which xdg-open > /dev/null 2>&1 && xdg-open $(COVERAGE_DIR)/coverage.html || \
	which open > /dev/null 2>&1 && open $(COVERAGE_DIR)/coverage.html || \
	echo "$(YELLOW)Coverage report generated at $(COVERAGE_DIR)/coverage.html$(NC)"

# Rules gosec may skip repo-wide. devops.md §8: every entry carries a written
# reason, checked against the code. .github/workflows/ci.yml runs this same
# target, so there is no second copy of this list to keep in sync.
#
# Anything NOT listed here is resolved at the site instead, with a `#nosec Gxxx
# -- reason` annotation naming why that specific line is safe. Per-site beats
# repo-wide: a blanket exclude also hides the next occurrence, which nobody
# reviewed. G402 (TLS config) and G407 (hardcoded IV/nonce) were on this list
# and matched zero sites; they were removed rather than left as standing
# permission for a finding that has never been examined.
#
#   G115 integer overflow on conversion. 472 sites, and the rule cannot see
#        the bounds check: the conversions it flags are guarded on the same
#        value (frame/entry sizes against their Max constants, ports against
#        65535) immediately above the cast.
#   G117 exported struct field matching a secret-ish name. Fires on the
#        credential and token types the IAM and OAuth2 surfaces exist to
#        model -- naming a field `Token` is the point, not a leak.
#   G204 subprocess launched with a variable. Every site is an operator-
#        supplied command in the agent deploy and orchestrator paths, quoted
#        through shellQuote and never interpolated into `sh -c`.
#   G304 file inclusion via variable. Config, certificate and data-directory
#        paths come from the server's own config, not from request input.
#   G404 math/rand. Retry/backoff jitter only (pkg/rag/pipeline/retry.go,
#        examples/timeseries). Jitter must be cheap and merely unpredictable
#        enough to desynchronize retries; it is not a security primitive.
#   G703 path traversal via taint analysis. Two sites, neither on a request
#        path, both re-validated 2026-08-26 against gosec v2.29.0 AND v2.28.0
#        -- identical findings under both, so this does not turn on the
#        scanner version.
#        scripts/protoannotate/main.go globs a HARDCODED constant (protoDir =
#        "api/proto"); it is a build-time source rewriter that takes no
#        external input and ships in no binary.
#        cmd/quicbaas/canonical_auth_login.go stats a client-cert path read
#        from one of four named env vars, expanding a leading "~" and nothing
#        else. The environment belongs to the user already running the CLI --
#        anyone who can set it can already execute as that user -- and os.Stat
#        only probes existence, never opening the file.
#        Deliberately NOT "the same paths as G304": neither site is server
#        config. Do not extend this entry to a site reachable from a request.
#   G704 SSRF via taint analysis. The flagged requests are the federation and
#        ACME clients calling endpoints from stored provider configuration.
GOSEC_EXCLUDE := G115,G117,G204,G304,G404,G703,G704

.PHONY: gosec
gosec: ## Run gosec security scanner
	@echo "$(BLUE)Running security analysis with gosec...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@GOSEC_BIN=$$(command -v gosec 2>/dev/null || echo "$$HOME/go/bin/gosec"); \
	if [ ! -x "$$GOSEC_BIN" ]; then \
		echo "$(RED)gosec not found$(NC)"; \
		echo "$(YELLOW)Install with: make install-gosec$(NC)"; \
		exit 1; \
	fi; \
	$$GOSEC_BIN -concurrency 4 -exclude=$(GOSEC_EXCLUDE) -severity medium -confidence medium \
		-exclude-dir=test -exclude-dir=testdata -exclude-dir=vendor -exclude-dir=sdk -exclude-dir=.tmp -exclude-dir=.deps \
		-exclude-generated \
		-tags "$$(echo '$(LINT_BUILD_TAGS)' | tr ' ' ',')" \
		-fmt=sarif -out=$(BUILD_DIR)/gosec.sarif -stdout -verbose=text ./...; rc=$$?; \
	if [ $$rc -eq 0 ]; then \
		echo "$(GREEN)Security scan complete - no issues found$(NC)"; \
	elif [ $$rc -gt 128 ]; then \
		echo "$(RED)gosec was KILLED by signal $$(($$rc - 128)) (exit $$rc) -- it did NOT report a finding.$(NC)"; \
		echo "$(RED)A shell renders a signal death as 128+N, and the old '|| exit 1' here collapsed$(NC)"; \
		echo "$(RED)that into the same red as real findings -- someone then hunts a vulnerability$(NC)"; \
		echo "$(RED)that was never reported. This is memory pressure in the job container.$(NC)"; \
		echo "$(RED)Re-run when the host is quieter; confirm locally before treating it as a finding.$(NC)"; \
		exit $$rc; \
	else \
		echo "$(RED)Security issues found (SARIF: $(BUILD_DIR)/gosec.sarif)$(NC)"; \
		exit $$rc; \
	fi

.PHONY: vuln
vuln: ## Run govulncheck to scan for known vulnerabilities
	@echo "$(BLUE)Running vulnerability scan with govulncheck...$(NC)"
	@GOVULNCHECK_BIN=$$(command -v govulncheck 2>/dev/null || echo "$$HOME/go/bin/govulncheck"); \
	if [ -x "$$GOVULNCHECK_BIN" ]; then \
		$$GOVULNCHECK_BIN ./...; rc=$$?; \
		if [ $$rc -eq 0 ]; then \
			echo "$(GREEN)Vulnerability scan complete$(NC)"; \
		elif [ $$rc -gt 128 ]; then \
			echo "$(RED)govulncheck was KILLED by signal $$(($$rc - 128)) (exit $$rc) -- it did NOT report a vulnerability.$(NC)"; \
			echo "$(RED)On a 16 GB job container this is the memory cap: govulncheck holds the whole$(NC)"; \
			echo "$(RED)package graph and its SSA live, which GOMEMLIMIT cannot collect. Re-run when$(NC)"; \
			echo "$(RED)the host is quieter, or raise the cap -- do not read this as a finding.$(NC)"; \
			exit $$rc; \
		else \
			echo "$(RED)govulncheck found reachable vulnerabilities$(NC)"; \
			exit $$rc; \
		fi; \
	else \
		echo "$(YELLOW)govulncheck not found$(NC)"; \
		echo "$(YELLOW)Install with: make install-govulncheck$(NC)"; \
		exit 1; \
	fi

.PHONY: trivy
# test/ and examples/ are excluded: neither ships, and their fixtures and
# throwaway dependencies would otherwise dominate the report. bin/, build/ and
# data/ are gitignored local artifacts that are not in a CI checkout at all --
# data/ in particular holds a locally generated server key that the secret
# scanner would flag on every run. The certs skip-file keeps the secret scanner
# off the committed test TLS material.
trivy: ## Run Trivy vulnerability scanner on filesystem
	@echo "$(BLUE)Running Trivy vulnerability scan...$(NC)"
	@if ! command -v trivy >/dev/null 2>&1; then \
		echo "$(RED)trivy not found$(NC)"; \
		echo "$(YELLOW)Install with: make install-trivy$(NC)"; \
		exit 1; \
	fi; \
	trivy fs --scanners vuln,secret --severity CRITICAL,HIGH --exit-code 1 \
		--skip-dirs test --skip-dirs examples --skip-dirs bin --skip-dirs build \
		--skip-dirs data \
		--skip-files 'pkg/server/certs/*.key' . && \
	echo "$(GREEN)Trivy scan complete - no critical/high vulnerabilities found$(NC)"

# The images this Makefile builds that also ship: the daemon and its FIPS
# variant. The CLI image (Dockerfile.cli) has no Makefile build target; it and
# these two are gated in .github/workflows/release.yml before push.
TRIVY_IMAGES := $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_IMAGE):fips

.PHONY: trivy-image
# Mirrors the gate in the ci.yml docker job so the same failure reproduces
# locally (devops.md §4). Not a prerequisite of `security`: it builds images,
# which needs a Docker daemon and many minutes.
trivy-image: docker-build docker-build-fips ## Build the shipped images and scan each with Trivy
	@echo "$(BLUE)Running Trivy vulnerability scan on Docker images...$(NC)"
	@if command -v trivy >/dev/null 2>&1; then \
		rc=0; \
		for img in $(TRIVY_IMAGES); do \
			echo "$(BLUE)  Scanning $$img...$(NC)"; \
			trivy image --severity CRITICAL,HIGH --exit-code 1 --no-progress "$$img" || rc=1; \
		done; \
		if [ $$rc -ne 0 ]; then \
			echo "$(RED)Trivy found CRITICAL/HIGH findings$(NC)"; \
			exit 1; \
		fi; \
		echo "$(GREEN)Trivy image scan complete$(NC)"; \
	else \
		echo "$(YELLOW)trivy not found - skipping image scan$(NC)"; \
		echo "$(YELLOW)Install with: make install-trivy$(NC)"; \
	fi

.PHONY: modverify
modverify: ## Verify module integrity against go.sum
	@echo "$(BLUE)Verifying module integrity...$(NC)"
	@$(GOCMD) mod verify

.PHONY: race
race: ## Run tests with race detector
	@echo "$(BLUE)Running race detector tests...$(NC)"
ifneq ($(strip $(BUILD_TAGS)),)
	@echo "$(YELLOW)Build tags: $(INTEGRATION_BUILD_TAGS)$(NC)"
endif
	CGO_ENABLED=1 $(GOCMD) test -race -p 2 $(BUILD_TAGS_ARG) -timeout=10m ./pkg/... ./cmd/... || \
		(echo "$(RED)Race detector found issues$(NC)" && exit 1)
	@echo "$(GREEN)Race detector tests passed$(NC)"

.PHONY: security
# The full devops.md §8 suite. Container images are scanned separately by
# `make trivy-image`, which builds them.
security: gosec vuln modverify trivy ## Full §8 suite (gosec, govulncheck, go mod verify, trivy fs)
	@echo "$(GREEN)All security scans passed$(NC)"
	@echo "$(YELLOW)Note: container images are scanned separately - run 'make trivy-image'$(NC)"

.PHONY: security-scan
security-scan: security ## Alias for `security` (kept for existing muscle memory)

##@ Docker

.PHONY: docker-build
docker-build: ## Build Docker image
	@echo "$(BLUE)Building Docker image...$(NC)"
ifneq ($(strip $(BUILD_TAGS)),)
	@echo "$(YELLOW)Build tags: $(INTEGRATION_BUILD_TAGS)$(NC)"
endif
	$(DOCKER) buildx build --build-arg BUILD_TAGS="$(strip $(INTEGRATION_BUILD_TAGS))" -t $(DOCKER_IMAGE):$(DOCKER_TAG) -t $(DOCKER_IMAGE):latest .
	@echo "$(GREEN)Docker image built: $(DOCKER_IMAGE):$(DOCKER_TAG)$(NC)"

.PHONY: docker-build-dev
docker-build-dev: ## Build development Docker image
	@echo "$(BLUE)Building development Docker image...$(NC)"
ifneq ($(strip $(BUILD_TAGS)),)
	@echo "$(YELLOW)Build tags: $(INTEGRATION_BUILD_TAGS)$(NC)"
endif
	$(DOCKER) buildx build --build-arg BUILD_TAGS="$(strip $(INTEGRATION_BUILD_TAGS))" -f test/integration/Dockerfile.dev -t $(DOCKER_IMAGE):dev .

.PHONY: docker-build-fips
docker-build-fips: ## Build FIPS 140-3 Docker image
	@echo "$(BLUE)Building FIPS 140-3 Docker image...$(NC)"
	@echo "$(YELLOW)Using GOFIPS140=latest (Go 1.24+)$(NC)"
ifneq ($(strip $(BUILD_TAGS)),)
	@echo "$(YELLOW)Build tags: $(INTEGRATION_BUILD_TAGS)$(NC)"
endif
	$(DOCKER) buildx build --build-arg GOFIPS140=latest --build-arg BUILD_TAGS="$(strip $(INTEGRATION_BUILD_TAGS))" -f Dockerfile.fips -t $(DOCKER_IMAGE):fips .
	@echo "$(GREEN)FIPS Docker image built: $(DOCKER_IMAGE):fips$(NC)"

.PHONY: docker-build-devcontainer
docker-build-devcontainer: ## Build devcontainer image
	@echo "$(BLUE)Building devcontainer image...$(NC)"
	$(DOCKER) buildx build -f .devcontainer/Dockerfile -t $(DOCKER_IMAGE)-devcontainer:latest .
	@echo "$(GREEN)Devcontainer image built: $(DOCKER_IMAGE)-devcontainer:latest$(NC)"

.PHONY: docker-build-integration
docker-build-integration: ## Build integration test images
	@echo "$(BLUE)Building integration test images...$(NC)"
ifneq ($(strip $(BUILD_TAGS)),)
	@echo "$(YELLOW)Build tags: $(INTEGRATION_BUILD_TAGS)$(NC)"
endif
	$(DOCKER) buildx build --build-arg BUILD_TAGS="$(strip $(INTEGRATION_BUILD_TAGS))" -f test/integration/Dockerfile -t $(DOCKER_IMAGE)-integration-test:latest .
	$(DOCKER) buildx build --build-arg BUILD_TAGS="$(strip $(INTEGRATION_BUILD_TAGS))" -f test/integration/Dockerfile.node -t $(DOCKER_IMAGE)-integration-node:latest .
	@echo "$(GREEN)Integration test images built$(NC)"

.PHONY: docker-run
docker-run: ## Run Docker container
	@echo "$(BLUE)Running Docker container...$(NC)"
	$(DOCKER) run -p 8080:8080 -p 9090:9090 --name $(APP_NAME) $(DOCKER_IMAGE):latest

.PHONY: docker-run-fips
docker-run-fips: ## Run FIPS Docker container
	@echo "$(BLUE)Running FIPS Docker container...$(NC)"
	$(DOCKER) run -p 8080:8080 -p 9090:9090 --name $(APP_NAME)-fips $(DOCKER_IMAGE):fips

.PHONY: docker-stop
docker-stop: ## Stop Docker container
	@echo "$(BLUE)Stopping Docker container...$(NC)"
	$(DOCKER) stop $(APP_NAME) || true
	$(DOCKER) rm $(APP_NAME) || true

.PHONY: docker-push
docker-push: ## Push Docker image to registry
	@echo "$(BLUE)Pushing Docker image to registry...$(NC)"
	$(DOCKER) tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	$(DOCKER) tag $(DOCKER_IMAGE):latest $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest
	$(DOCKER) push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	$(DOCKER) push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest

.PHONY: docker-compose-up
docker-compose-up: ## Start docker-compose environment
	@echo "$(BLUE)Starting docker-compose environment...$(NC)"
	$(DOCKER_COMPOSE) up -d

.PHONY: docker-compose-down
docker-compose-down: ## Stop docker-compose environment
	@echo "$(BLUE)Stopping docker-compose environment...$(NC)"
	$(DOCKER_COMPOSE) down --volumes --remove-orphans

.PHONY: docker-compose-logs
docker-compose-logs: ## Show docker-compose logs
	$(DOCKER_COMPOSE) logs -f

##@ Cluster Management

.PHONY: cluster-up
cluster-up: ## Start test cluster
	@echo "$(BLUE)Starting QuicBaaS test cluster...$(NC)"
	@$(DOCKER_COMPOSE) -f test/integration/docker-compose.test.yml up -d
	@echo "$(GREEN)Test cluster started$(NC)"
	@echo "$(YELLOW)Waiting for cluster to be ready...$(NC)"
	@sleep 5
	@$(MAKE) cluster-status

.PHONY: cluster-down
cluster-down: ## Stop test cluster
	@echo "$(BLUE)Stopping QuicBaaS test cluster...$(NC)"
	@$(DOCKER_COMPOSE) -f test/integration/docker-compose.test.yml down --volumes --remove-orphans
	@echo "$(GREEN)Test cluster stopped and cleaned up$(NC)"

.PHONY: cluster-down-integration
cluster-down-integration: ## Stop integration test cluster
	@echo "$(BLUE)Stopping integration test cluster...$(NC)"
	@$(DOCKER_COMPOSE) -f test/integration/docker-compose.integration.yml down --volumes --remove-orphans
	@echo "$(GREEN)Integration test cluster stopped and cleaned up$(NC)"

.PHONY: cluster-restart
cluster-restart: cluster-down cluster-up ## Restart test cluster

.PHONY: cluster-logs
cluster-logs: ## View cluster logs
	@$(DOCKER_COMPOSE) -f test/integration/docker-compose.test.yml logs -f

.PHONY: cluster-logs-node1
cluster-logs-node1: ## View node1 logs
	@$(DOCKER_COMPOSE) -f test/integration/docker-compose.test.yml logs -f node1

.PHONY: cluster-logs-node2
cluster-logs-node2: ## View node2 logs
	@$(DOCKER_COMPOSE) -f test/integration/docker-compose.test.yml logs -f node2

.PHONY: cluster-logs-node3
cluster-logs-node3: ## View node3 logs
	@$(DOCKER_COMPOSE) -f test/integration/docker-compose.test.yml logs -f node3

.PHONY: cluster-status
cluster-status: ## Check cluster status
	@echo "$(BLUE)Checking cluster status...$(NC)"
	@echo ""
	@echo "Node 1:"
	@curl -sf http://localhost:8081/v1/health/live && echo "  Status: Healthy" || echo "  Status: Unhealthy"
	@echo ""
	@echo "Node 2:"
	@curl -sf http://localhost:8082/v1/health/live && echo "  Status: Healthy" || echo "  Status: Unhealthy"
	@echo ""
	@echo "Node 3:"
	@curl -sf http://localhost:8083/v1/health/live && echo "  Status: Healthy" || echo "  Status: Unhealthy"
	@echo ""

.PHONY: cluster-health
cluster-health: ## Run cluster health check
	@echo "$(BLUE)Running cluster health check...$(NC)"
	@for port in 8081 8082 8083; do \
		echo "Checking node on port $$port..."; \
		curl -sf http://localhost:$$port/api/v1/health || echo "  Node on port $$port is unhealthy"; \
	done

.PHONY: cluster-clean
cluster-clean: cluster-down ## Clean cluster volumes and data
	@echo "$(BLUE)Cleaning cluster volumes...$(NC)"
	@$(DOCKER) volume rm -f $$($(DOCKER) volume ls -q | grep quicbaas) 2>/dev/null || true
	@rm -rf data/ logs/ 2>/dev/null || true
	@echo "$(GREEN)Cluster data cleaned$(NC)"

##@ Deployment

.PHONY: deploy-dev
deploy-dev: ## Deploy to development environment
	@echo "$(BLUE)Deploying to development...$(NC)"
	kubectl apply -f deployments/kubernetes/dev/

.PHONY: deploy-staging
deploy-staging: ## Deploy to staging environment
	@echo "$(BLUE)Deploying to staging...$(NC)"
	kubectl apply -f deployments/kubernetes/staging/

.PHONY: deploy-prod
deploy-prod: ## Deploy to production environment
	@echo "$(BLUE)Deploying to production...$(NC)"
	kubectl apply -f deployments/kubernetes/prod/

##@ Tools

.PHONY: install-gosec
install-gosec: ## Install gosec security scanner
	@echo "$(BLUE)Installing gosec...$(NC)"
	@if ! command -v gosec >/dev/null 2>&1; then \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
		echo "$(GREEN)gosec installed$(NC)"; \
	else \
		echo "$(GREEN)gosec already installed$(NC)"; \
	fi

.PHONY: install-govulncheck
install-govulncheck: ## Install govulncheck vulnerability scanner
	@echo "$(BLUE)Installing govulncheck...$(NC)"
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
		echo "$(GREEN)govulncheck installed$(NC)"; \
	else \
		echo "$(GREEN)govulncheck already installed$(NC)"; \
	fi

.PHONY: install-trivy
install-trivy: ## Install Trivy vulnerability scanner
	@echo "$(BLUE)Installing trivy...$(NC)"
	@if ! command -v trivy >/dev/null 2>&1; then \
		curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b $$(go env GOPATH)/bin; \
		echo "$(GREEN)trivy installed$(NC)"; \
	else \
		echo "$(GREEN)trivy already installed$(NC)"; \
	fi

.PHONY: install-golangci-lint
install-golangci-lint: ## Install golangci-lint (pinned -- see GOLANGCI_VERSION)
	@echo "$(BLUE)Installing golangci-lint $(GOLANGCI_VERSION)...$(NC)"
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	@echo "$(GREEN)golangci-lint $(GOLANGCI_VERSION) installed$(NC)"

.PHONY: install-tools
install-tools: install-golangci-lint install-gosec install-govulncheck install-trivy ## Install development tools
	@echo "$(BLUE)Installing development tools...$(NC)"
	@$(MAKE) install-proto-tools
	@echo "$(GREEN)Development tools installed$(NC)"

##@ FIPS Utilities

.PHONY: fips-status
fips-status: ## Check FIPS mode status
	@echo "$(BLUE)Checking FIPS mode status...$(NC)"
	@echo "Go version: $(GO_VERSION)"
	@echo ""
	@echo "Go FIPS 140-3 support (GOFIPS140):"
	@if [ -z "$${GOFIPS140}" ]; then \
		echo "  GOFIPS140 not set (standard build)"; \
	else \
		echo "  GOFIPS140=$$GOFIPS140"; \
	fi
	@echo ""
	@if [ -f "$(BIN_DIR)/$(APP_NAME)-fips" ]; then \
		echo "FIPS binary exists: $(BIN_DIR)/$(APP_NAME)-fips"; \
		echo "Checking for FIPS symbols..."; \
		strings $(BIN_DIR)/$(APP_NAME)-fips | grep -i fips | head -5 || echo "  No FIPS symbols found"; \
	else \
		echo "FIPS binary not found. Build with: make build-fips"; \
	fi
	@echo ""
	@echo "Note: Go 1.24+ uses crypto/fips140.Enabled() instead of BoringCrypto"

.PHONY: fips-verify
fips-verify: ## Verify FIPS build
	@echo "$(BLUE)Verifying FIPS build...$(NC)"
	@if [ -f "$(BIN_DIR)/$(APP_NAME)-fips" ]; then \
		echo "Checking for FIPS symbols..."; \
		strings $(BIN_DIR)/$(APP_NAME)-fips | grep -i fips | head -10 || echo "$(YELLOW)Warning: No FIPS symbols found$(NC)"; \
		echo ""; \
		echo "File info:"; \
		file $(BIN_DIR)/$(APP_NAME)-fips; \
		echo ""; \
		echo "$(GREEN)FIPS binary verified$(NC)"; \
	else \
		echo "$(RED)FIPS binary not found. Build with: make build-fips$(NC)"; \
	fi

##@ Cleanup

.PHONY: clean
clean: ## Clean build artifacts and test data
	@echo "$(BLUE)Cleaning build artifacts and test data...$(NC)"
	@# Clean build directories and binaries
	rm -rf $(BIN_DIR) $(BUILD_DIR) $(DIST_DIR) $(COVERAGE_DIR)
	rm -f quicbaas quicbaasd coverage.out coverage.html *.log
	@# Clean compiled test binaries (root and all subdirectories)
	find . -name "*.test" -type f -delete 2>/dev/null || true
	@# Clean temp files from /tmp
	rm -f /tmp/quicbaas-*.log /tmp/quicbaas-*.pid /tmp/QuicBaaS_*.log 2>/dev/null || true
	@# Clean test data and logs
	rm -rf data/ logs/ 2>/dev/null || true
	@# Clean testdata directories (except processor testdata which has real test fixtures)
	find . -type d -name "testdata" -not -path "*/pkg/rag/processor/testdata" -exec rm -rf {} + 2>/dev/null || true
	@# Clean benchmark artifacts
	rm -rf test/benchmark/results test/benchmark/profiles/*.prof 2>/dev/null || true
	@# Clean empty directories left by tests
	find pkg -type d -empty -delete 2>/dev/null || true
	find sdk -type d -empty -delete 2>/dev/null || true
	find test -type d -empty -delete 2>/dev/null || true
	@# Clean strange test artifact directories
	rm -rf "cmd/quicbaas/path#fragment" "cmd/quicbaas/path?param=value" "cmd/quicbaas/path:with:colons" 2>/dev/null || true
	@# Clean Go caches
	$(GOCMD) clean -cache -testcache
	@echo "$(GREEN)Cleanup complete$(NC)"

# Docker cleanup is scoped to the images this repo builds, by tag. It must never
# be a host-global `docker system prune -af --volumes`: the CI runners are
# shared, and that deletes every unused image and named volume on the host,
# including the ones other repositories' jobs depend on (incident 2026-08-17,
# #3 — it repeatedly destroyed the codex-review job image and failed 16 review
# runs portfolio-wide).
#
# This is every image identity the repository creates under a name it writes
# literally: the `-t` of its own `docker build`s, the destination of its own
# `docker tag`s, and the explicit `image:` of a Compose service that builds.
# scripts/docker-image-inventory.sh re-derives all of that from those commands
# and files; `make verify-clean-docker` fails when this list has drifted from
# it in either direction, so a new image-producing target cannot quietly escape
# cleanup and a tag no producer here names cannot be reached.
#
# Ownership comes from producers only. That a target runs `docker rmi X` is not
# evidence that X is ours — it is the claim being checked — so removals are not
# an inventory source.
#
# Compose services with no explicit `image:` are therefore deliberately absent,
# and that is why `integration-integration-test` and `integration-test-node1..5`
# are not listed here. Compose names those `<project>-<service>`, and the
# project name is the directory Compose ran in or a target's `-p` override — for
# the root docker-compose.yml it is the checkout directory's own name, and two
# compose files in one directory share a name. Those names identify whichever
# checkout on the host used them last, not this repository. Each such stack is
# instead torn down by the target that runs it, where the name is unambiguous
# because that target just used it: `integration-test` brings its stack down
# with `--volumes --remove-orphans` and removes those six images by name before
# it rebuilds them, and an EXIT trap brings the stack down again on every way
# out of that recipe.
DOCKER_LOCAL_IMAGES := \
	$(DOCKER_IMAGE):$(DOCKER_TAG) \
	$(DOCKER_IMAGE):latest \
	$(DOCKER_IMAGE):dev \
	$(DOCKER_IMAGE):fips \
	$(DOCKER_IMAGE)-test:latest \
	$(DOCKER_IMAGE)-devcontainer:latest \
	$(DOCKER_IMAGE)-integration-test:latest \
	$(DOCKER_IMAGE)-integration-node:latest \
	$(DOCKER_IMAGE)-claude-code-agent:latest \
	$(DOCKER_IMAGE)-e2e-sandbox-agent:latest \
	$(DOCKER_IMAGE)/swtpm:latest \
	signalbridge:latest \
	$(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG) \
	$(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest \
	go-quicbaas-quicbaas-node1:latest \
	go-quicbaas-quicbaas-node2:latest \
	go-quicbaas-quicbaas-node3:latest

.PHONY: clean-docker
clean-docker: ## Remove the Docker images this project builds (never prunes the host)
	@echo "$(BLUE)Removing QuicBaaS Docker images...$(NC)"
	@if ! command -v $(DOCKER) > /dev/null 2>&1; then \
		echo "$(RED)Error: Docker is not installed$(NC)"; \
		exit 1; \
	fi
	@# One daemon call decides what exists, so its failure (daemon down, access
	@# denied) propagates instead of reading as "no such image". Removal failures
	@# are accumulated: a `for` loop otherwise reports only its last command's
	@# status, and an early failure followed by an absent tag would exit 0.
	@#
	@# Removal is never forced. These tags are fixed, so on a shared runner a
	@# concurrent checkout of this repository can be using one; `rmi -f` would
	@# untag it out from under that run. Unforced `rmi` refuses instead, and the
	@# refusal is reported rather than swallowed.
	@present=$$($(DOCKER) images --format '{{.Repository}}:{{.Tag}}') || exit $$?; \
	status=0; \
	for image in $(DOCKER_LOCAL_IMAGES); do \
		if printf '%s\n' "$$present" | grep -qxF "$$image"; then \
			$(DOCKER) rmi "$$image" || status=1; \
		fi; \
	done; \
	if [ $$status -ne 0 ]; then \
		echo "$(RED)Some QuicBaaS images could not be removed (in use?)$(NC)"; \
		exit $$status; \
	fi; \
	echo "$(GREEN)QuicBaaS Docker images removed$(NC)"

.PHONY: clean-all
clean-all: clean clean-docker proto-clean cluster-clean ## Clean everything

##@ Documentation

.PHONY: docs
docs: ## Generate documentation
	@echo "$(BLUE)Generating documentation...$(NC)"
	@which godoc > /dev/null || go install golang.org/x/tools/cmd/godoc@latest
	@echo "$(GREEN)Run 'godoc -http=:6060' to view documentation$(NC)"

.PHONY: docs-serve
docs-serve: ## Serve documentation
	@which godoc > /dev/null || go install golang.org/x/tools/cmd/godoc@latest
	@echo "$(GREEN)Serving documentation at http://localhost:6060$(NC)"
	godoc -http=:6060

##@ Development Workflow

.PHONY: dev
dev: dev-rebuild dev-start ## Rebuild and start development environment

.PHONY: dev-rebuild
dev-rebuild: ## Rebuild development Docker image
	@echo "$(BLUE)Rebuilding development image...$(NC)"
	$(DOCKER) buildx build --load -f test/integration/Dockerfile.dev -t quicbaas:dev .
	@$(DOCKER) tag quicbaas:dev go-quicbaas-quicbaas-node1:latest
	@$(DOCKER) tag quicbaas:dev go-quicbaas-quicbaas-node2:latest
	@$(DOCKER) tag quicbaas:dev go-quicbaas-quicbaas-node3:latest
	@echo "$(GREEN)Development image rebuilt$(NC)"

.PHONY: dev-start
dev-start: ## Start development environment
	@echo "$(BLUE)Starting development environment...$(NC)"
	$(DOCKER_COMPOSE) -f docker-compose.yml up -d --remove-orphans
	@echo "$(GREEN)Development environment started$(NC)"
	@echo "$(YELLOW)Waiting for services to be healthy...$(NC)"
	@sleep 10
	@$(DOCKER_COMPOSE) ps

.PHONY: dev-logs
dev-logs: ## Show development logs
	$(DOCKER_COMPOSE) logs -f

.PHONY: dev-restart
dev-restart: ## Restart development environment
	@echo "$(BLUE)Restarting development environment...$(NC)"
	$(DOCKER_COMPOSE) restart

.PHONY: dev-down
dev-down: ## Stop development environment
	@echo "$(BLUE)Stopping development environment...$(NC)"
	$(DOCKER_COMPOSE) down --volumes --remove-orphans

.PHONY: dev-gui
dev-gui: _build-sdk-typescript ## Launch Wails desktop GUI in development mode (hot reload)
	@echo "$(BLUE)Starting QuicBaaS GUI in development mode...$(NC)"
	cd cmd/quicbaas && wails dev -tags 'desktop webkit2_41 $(strip $(BUILD_TAGS))'

.PHONY: watch
watch: ## Watch for changes and rebuild
	@echo "$(BLUE)Watching for changes...$(NC)"
	@which air > /dev/null || go install github.com/cosmtrek/air@latest
	air

##@ CI/CD

.PHONY: ci
ci: deps fmt-check vet lint security build test test-minimal race check-coverage ## Run CI pipeline locally (matches GitHub CI)
	@echo "$(GREEN)CI pipeline completed successfully$(NC)"

.PHONY: ci-full
ci-full: deps fmt-check vet lint security build test test-minimal test-desktop race coverage integration-test ## Run full CI pipeline with integration tests
	@echo "$(GREEN)Full CI pipeline completed successfully$(NC)"

.PHONY: pre-commit
pre-commit: fmt vet lint test ## Run pre-commit checks
	@echo "$(GREEN)Pre-commit checks passed$(NC)"

.PHONY: check
check: fmt vet lint gosec vuln ## Run all code quality checks (fmt, vet, lint, gosec, vuln)
	@echo "$(GREEN)All quality checks passed$(NC)"

##@ Release

.PHONY: release
release: cross-compile docker-build trivy-image-release ## Create a new release with container security scan
	@echo "$(BLUE)Creating release $(VERSION)...$(NC)"
	@mkdir -p $(DIST_DIR)
	@cd $(BIN_DIR) && tar -czf ../$(DIST_DIR)/$(APP_NAME)-$(VERSION)-linux-amd64.tar.gz $(APP_NAME)-linux-amd64
	@cd $(BIN_DIR) && tar -czf ../$(DIST_DIR)/$(APP_NAME)-$(VERSION)-darwin-amd64.tar.gz $(APP_NAME)-darwin-amd64
	@cd $(BIN_DIR) && tar -czf ../$(DIST_DIR)/$(APP_NAME)-$(VERSION)-darwin-arm64.tar.gz $(APP_NAME)-darwin-arm64
	@cd $(BIN_DIR) && zip ../$(DIST_DIR)/$(APP_NAME)-$(VERSION)-windows-amd64.zip $(APP_NAME)-windows-amd64.exe
	@echo "$(GREEN)Release artifacts created in $(DIST_DIR)$(NC)"

.PHONY: trivy-image-release
trivy-image-release: ## Run Trivy on Docker image for release (fails on CRITICAL/HIGH)
	@echo "$(BLUE)Running Trivy vulnerability scan on release image...$(NC)"
	@TRIVY_BIN=$$(command -v trivy 2>/dev/null); \
	if [ -n "$$TRIVY_BIN" ]; then \
		$$TRIVY_BIN image --severity CRITICAL,HIGH --exit-code 1 $(DOCKER_IMAGE):$(DOCKER_TAG) && \
		echo "$(GREEN)Trivy image scan passed - no critical/high vulnerabilities$(NC)"; \
	else \
		echo "$(YELLOW)trivy not found$(NC)"; \
		echo "$(YELLOW)Install with: make install-trivy$(NC)"; \
		exit 1; \
	fi

##@ Database Operations

# Note: db-backup and db-restore targets removed - implement as needed using QuicBaaS APIs

##@ Monitoring

.PHONY: metrics
metrics: ## Show metrics endpoint
	@echo "$(BLUE)Metrics available at http://localhost:9090/metrics$(NC)"
	curl -s http://localhost:9090/metrics

.PHONY: health
health: ## Check health status
	@echo "$(BLUE)Checking health status...$(NC)"
	@curl -s http://localhost:8080/health/live && echo "" || echo "$(RED)Service not healthy$(NC)"
	@curl -s http://localhost:8080/health/ready && echo "" || echo "$(RED)Service not ready$(NC)"

.PHONY: all
all: clean deps lint test build ## Run all major targets
	@echo "$(GREEN)All targets completed successfully$(NC)"
.PHONY: integration-bench
integration-bench: ## Run integration benchmarks
	@echo "$(BLUE)Running integration benchmarks...$(NC)"
	@echo "$(YELLOW)Starting integration test cluster...$(NC)"
	@$(DOCKER_COMPOSE) -f test/integration/docker-compose.integration.yml up -d
	@echo "$(YELLOW)Waiting for services to be ready...$(NC)"
	@sleep 10
	@echo "$(BLUE)Running benchmarks...$(NC)"
	$(GOTEST) -bench=. -benchmem ./test/integration/benchmarks/... || ($(DOCKER_COMPOSE) -f test/integration/docker-compose.integration.yml down && exit 1)
	@echo "$(BLUE)Stopping integration test cluster...$(NC)"
	@$(DOCKER_COMPOSE) -f test/integration/docker-compose.integration.yml down --volumes
	@echo "$(GREEN)Integration benchmarks completed$(NC)"

# ==============================================================================
# NOTE: Legacy hardcoded integration test targets removed.
# Use the dynamic pattern instead:
#   make integration-test-<package>     # e.g., make integration-test-scheduler
#   make integration-test-sdk-go-<pkg>  # e.g., make integration-test-sdk-go-scheduler
#   make integration-test-sdk-js-<pkg>  # e.g., make integration-test-sdk-js-scheduler
# Run 'make list-packages' to see all available targets.
# ==============================================================================

# IAM package tests
# IAM is always compiled (no build tag), so these targets use the standard
# test tag set.
.PHONY: test-iam
test-iam: ## Run IAM unit tests
	go test -v -race -count=1 -tags '$(IAM_TEST_TAGS)' ./pkg/iam/...

.PHONY: test-iam-coverage
test-iam-coverage:
	@mkdir -p $(COVERAGE_DIR)
	$(GOTEST) -v -race -tags '$(IAM_TEST_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-iam.out -covermode=atomic ./pkg/iam/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-iam.out -o $(COVERAGE_DIR)/coverage-iam.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-iam.out | grep total

.PHONY: bench-iam
bench-iam:
	go test -tags '$(IAM_TEST_TAGS)' -bench=. -benchmem -benchtime=5s ./pkg/iam/...

# =============================================================================
# Ollama Package Tests
# =============================================================================

.PHONY: test-ollama
test-ollama: ## Run Ollama unit tests (pkg/ollama + REST handlers)
	@echo "$(BLUE)Running Ollama unit tests...$(NC)"
	go test -v -race -count=1 ./pkg/ollama/...
	go test -v -race -count=1 -run "TestHandleOllama" ./pkg/api/rest/...
	go test -v -race -count=1 -run "TestOllama" ./pkg/api/mcp/...
	@echo "$(GREEN)✓ Ollama unit tests passed$(NC)"

.PHONY: test-ollama-coverage
test-ollama-coverage: ## Run Ollama tests with coverage report
	@echo "$(BLUE)Running Ollama tests with coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	$(GOTEST) -v -race -tags '$(TEST_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-ollama.out -covermode=atomic ./pkg/ollama/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-ollama.out -o $(COVERAGE_DIR)/coverage-ollama.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-ollama.out | grep total

# =============================================================================
# RAG Package Tests
# =============================================================================

.PHONY: test-rag
test-rag: ## Run RAG unit tests (pkg/rag + REST handlers)
	@echo "$(BLUE)Running RAG unit tests...$(NC)"
	go test -v -race -count=1 ./pkg/rag/...
	go test -v -race -count=1 -run "TestHandleRAG" ./pkg/api/rest/...
	go test -v -race -count=1 -run "TestRAG" ./pkg/api/mcp/...
	@echo "$(GREEN)✓ RAG unit tests passed$(NC)"

.PHONY: test-rag-coverage
test-rag-coverage: ## Run RAG tests with coverage report
	@echo "$(BLUE)Running RAG tests with coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	$(GOTEST) -v -race -tags '$(TEST_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-rag.out -covermode=atomic ./pkg/rag/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-rag.out -o $(COVERAGE_DIR)/coverage-rag.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-rag.out | grep total

# =============================================================================
# Agent Orchestration Tests
# =============================================================================

.PHONY: test-acp
test-acp: ## Run ACP/agent backend unit tests
	@echo "$(BLUE)Running ACP tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -tags 'rag $(BUILD_TAGS)' ./pkg/acp/...
	@echo "$(GREEN)ACP unit tests passed$(NC)"

.PHONY: test-agent
test-agent: ## Run agent daemon unit tests (pkg/agent/*)
	@echo "$(BLUE)Running agent daemon tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -count=1 -tags 'rag mcp $(BUILD_TAGS)' ./pkg/agent/...
	@echo "$(GREEN)Agent daemon tests passed$(NC)"

.PHONY: coverage-acp
coverage-acp: ## Generate coverage report for ACP package
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -tags 'rag $(BUILD_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-acp.out -covermode=atomic ./pkg/acp/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-acp.out -o $(COVERAGE_DIR)/coverage-acp.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-acp.out | grep total

.PHONY: bench-agent
bench-agent: ## Run agent daemon benchmarks
	@echo "$(BLUE)Running agent daemon benchmarks...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -bench=. -benchmem -benchtime=$(BENCHTIME) -run=^$$ -tags 'rag mcp $(BUILD_TAGS)' ./pkg/agent/...
	@echo "$(GREEN)Agent daemon benchmarks completed$(NC)"

.PHONY: coverage-agent
coverage-agent: ## Generate coverage report for agent daemon package
	@echo "$(BLUE)Generating agent daemon coverage report...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -count=1 -tags 'rag mcp $(BUILD_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-agent.out -covermode=atomic ./pkg/agent/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-agent.out -o $(COVERAGE_DIR)/coverage-agent.html
	@echo "$(YELLOW)Coverage report: file://$(COVERAGE_DIR)/coverage-agent.html$(NC)"
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-agent.out | grep total

# Special integration tests with custom build flags (not covered by dynamic pattern)
.PHONY: integration-test-rag
integration-test-rag: ## Run RAG integration tests (Ollama via local CPU container; pass GPU_HOST=<host> to use a remote GPU)
	@echo "$(BLUE)Running RAG integration tests...$(NC)"
	@QUICBAAS_TEST_FILTER="./test/integration/rag/..." OLLAMA_CPU=$${OLLAMA_CPU:-1} WITH_RAFT=1 WITH_REST=1 WITH_RAG=1 WITH_VECTOR=1 $(MAKE) integration-test

.PHONY: integration-test-timeseries
integration-test-timeseries: ## Run Time Series integration tests (requires timeseries build tag)
	@echo "$(BLUE)Running Time Series integration tests...$(NC)"
	@QUICBAAS_TEST_FILTER="./test/integration/timeseries/..." WITH_RAFT=1 WITH_REST=1 WITH_TIMESERIES=1 $(MAKE) integration-test

.PHONY: integration-test-backup
integration-test-backup: ## Run Backup multi-backend integration tests
	@echo "$(BLUE)Running Backup multi-backend integration tests...$(NC)"
	@QUICBAAS_TEST_FILTER="./test/integration/backup/..." WITH_RAFT=1 WITH_REST=1 $(MAKE) integration-test

.PHONY: integration-test-iam
integration-test-iam: ## Run IAM integration tests
	@echo "$(BLUE)Running IAM integration tests...$(NC)"
	@QUICBAAS_TEST_FILTER="./test/integration/iam/..." WITH_RAFT=1 WITH_REST=1 WITH_QUIC=1 WITH_MCP=1 $(MAKE) integration-test

.PHONY: integration-test-durability
integration-test-durability: ## Run restart-durability integration tests (entities must survive a daemon restart)
	@echo "$(BLUE)Running restart-durability integration tests...$(NC)"
	@QUICBAAS_TEST_FILTER="./test/integration/durability/..." WITH_RAFT=1 $(MAKE) integration-test

.PHONY: integration-test-login
integration-test-login: ## Run unified login ceremony integration tests (mTLS + PIV across REST/gRPC/QUIC/Unix)
	@echo "$(BLUE)Running unified login ceremony integration tests...$(NC)"
	@QUICBAAS_TEST_FILTER="./test/integration/login/..." WITH_RAFT=1 WITH_REST=1 WITH_QUIC=1 WITH_MCP=1 $(MAKE) integration-test

.PHONY: integration-test-factors
integration-test-factors: ## Run cross-vendor hardware factor integration tests (spec §M13)
	@echo "$(BLUE)Running cross-vendor factor integration tests...$(NC)"
	@go test -v -tags integration -timeout 60s ./test/integration/factors/...

.PHONY: integration-test-init
integration-test-init: build-daemon ## Run canonical init/start/transport matrix (15 software-only cells + negative)
	@echo "$(BLUE)Running canonical init matrix integration tests...$(NC)"
	@INTEGRATION_TEST=1 QUICBAASD_BINARY="$(abspath $(BIN_DIR)/$(APP_NAME)d)" \
		go test -v -tags integration -timeout 5m -count 1 ./test/integration/init/...

.PHONY: integration-test-agent
integration-test-agent: ## Run agent orchestration integration tests (requires Docker + Ollama)
	@echo "$(BLUE)Starting Ollama container for agent tests...$(NC)"
	@if command -v nvidia-smi > /dev/null 2>&1 && nvidia-smi > /dev/null 2>&1; then \
		GPU_NAME=$$(nvidia-smi --query-gpu=name --format=csv,noheader | head -1); \
		GPU_MEM=$$(nvidia-smi --query-gpu=memory.total --format=csv,noheader | head -1); \
		GPU_IDX=$$(nvidia-smi --query-gpu=index --format=csv,noheader | head -1); \
		echo "$(GREEN)  ✓ NVIDIA GPU detected: $$GPU_NAME ($$GPU_MEM) [device $$GPU_IDX]$(NC)"; \
		NVIDIA_DEVICE_ID=$${NVIDIA_DEVICE_ID:-$$GPU_IDX} \
		docker compose -f test/integration/agent/docker-compose.yml -f test/integration/agent/docker-compose.gpu.yml up -d --wait; \
	else \
		echo "$(YELLOW)  ⚠ No NVIDIA GPU detected - Ollama will use CPU inference$(NC)"; \
		docker compose -f test/integration/agent/docker-compose.yml up -d --wait; \
	fi
	@echo "$(YELLOW)Pulling tinyllama model (this may take a few minutes on first run)...$(NC)"
	@docker exec quicbaas-agent-ollama ollama pull tinyllama
	@echo "$(GREEN)Ollama is ready with tinyllama model$(NC)"
	@echo "$(BLUE)Running agent orchestration integration tests...$(NC)"
	@OLLAMA_URL=http://localhost:11434 OLLAMA_MODEL=tinyllama \
		GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' \
		$(GOTEST) -v -race -tags 'integration rag' -timeout 300s ./test/integration/agent/... || \
		(echo "$(RED)Agent integration tests failed$(NC)" && docker compose -f test/integration/agent/docker-compose.yml down --remove-orphans && exit 1)
	@echo "$(BLUE)Stopping Ollama container...$(NC)"
	@docker compose -f test/integration/agent/docker-compose.yml down --remove-orphans
	@echo "$(GREEN)Agent integration tests passed$(NC)"

.PHONY: integration-test-agent-runtime
integration-test-agent-runtime: ## Run agent daemon integration tests in Docker (central + Ollama qwen3:1.7b)
	@echo "$(BLUE)Running agent runtime integration tests...$(NC)"
	@COMPOSE_FILE="test/integration/agent_runtime/docker-compose.yml"; \
	if command -v nvidia-smi > /dev/null 2>&1 && nvidia-smi > /dev/null 2>&1; then \
		GPU_NAME=$$(nvidia-smi --query-gpu=name --format=csv,noheader | head -1); \
		GPU_MEM=$$(nvidia-smi --query-gpu=memory.total --format=csv,noheader | head -1); \
		echo "$(GREEN)  ✓ NVIDIA GPU detected: $$GPU_NAME ($$GPU_MEM)$(NC)"; \
	else \
		echo "$(YELLOW)  ⚠ No NVIDIA GPU detected - Ollama will use CPU inference$(NC)"; \
	fi; \
	echo "$(YELLOW)Starting agent runtime test environment...$(NC)"; \
	docker compose -f $$COMPOSE_FILE up -d --wait central ollama 2>&1 | tail -5; \
	echo "$(YELLOW)Pulling qwen3:1.7b model (fastest for CI)...$(NC)"; \
	docker compose -f $$COMPOSE_FILE exec -T ollama ollama pull qwen3:1.7b 2>&1 | tail -3; \
	echo "$(GREEN)Environment ready$(NC)"; \
	echo "$(BLUE)Running tests...$(NC)"; \
	docker compose -f $$COMPOSE_FILE run --rm -T \
		-e QUICBAAS_URL=http://central:8080 \
		-e OLLAMA_URL=http://ollama:11434 \
		-e OLLAMA_MODEL=qwen3:1.7b \
		-e QUICBAAS_TEST_FILTER="$(QUICBAAS_TEST_FILTER)" \
		integration-test \
		go test -tags 'integration agent_runtime' -v -count=1 -timeout 5m \
			$${QUICBAAS_TEST_FILTER:-./test/integration/agent_runtime/...} 2>&1 | while IFS= read -r line; do echo "$$line"; done; \
	RESULT=$${PIPESTATUS[0]}; \
	echo "$(BLUE)Stopping agent runtime test environment...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans; \
	if [ "$$RESULT" -ne 0 ]; then \
		echo "$(RED)Agent runtime integration tests FAILED$(NC)"; \
		exit 1; \
	fi; \
	echo "$(GREEN)Agent runtime integration tests passed$(NC)"

.PHONY: integration-test-embedding
integration-test-embedding: ## Run embedding integration tests in Docker (Ollama via local CPU container; pass GPU_HOST=<host> for a remote GPU)
	@$(MAKE) integration-test QUICBAAS_TEST_FILTER="./test/integration/embedding/..." OLLAMA_CPU=$${OLLAMA_CPU:-1}

.PHONY: integration-test-orchestration
integration-test-orchestration: ## Run orchestrator E2E integration tests in Docker (Ollama via local CPU container; pass GPU_HOST=<host> for a remote GPU)
	@$(MAKE) integration-test QUICBAAS_TEST_FILTER="./test/integration/orchestration/..." OLLAMA_CPU=$${OLLAMA_CPU:-1}

.PHONY: integration-test-orchestrator
integration-test-orchestrator: ## Run multi-protocol CLI orchestrator inference tests (Ollama via local CPU container; pass GPU_HOST=<host> for a remote GPU)
	@$(MAKE) integration-test QUICBAAS_TEST_FILTER="-run TestOrchestratorInference ./test/integration/cli/..." OLLAMA_CPU=$${OLLAMA_CPU:-1}

.PHONY: e2e-remote-agent
e2e-remote-agent: ## Run E2E remote agent test (deploys Claude Code to remote host via SSH)
	@./scripts/e2e-remote-agent.sh

.PHONY: integration-test-agents
integration-test-agents: ## Run full E2E agent orchestration tests with real agents (requires Docker + host OIDC credentials)
	@echo "$(BLUE)Running E2E agent orchestration tests...$(NC)"
	@if [ ! -d "$$HOME/.claude" ]; then \
		echo "$(YELLOW)WARNING: ~/.claude not found - Claude Code agents may not authenticate$(NC)"; \
	fi
	@COMPOSE_FILE="deployments/agents/docker-compose.yml"; \
	if command -v nvidia-smi > /dev/null 2>&1 && nvidia-smi > /dev/null 2>&1; then \
		echo "$(GREEN)  ✓ GPU detected - enabling GPU acceleration$(NC)"; \
		COMPOSE_FILE="$$COMPOSE_FILE -f deployments/agents/docker-compose.gpu.yml"; \
	else \
		echo "$(YELLOW)  ⚠ No GPU detected - CPU inference only$(NC)"; \
	fi; \
	echo "$(YELLOW)Building agent containers...$(NC)"; \
	docker compose -f $$COMPOSE_FILE build 2>&1 | tail -5; \
	echo "$(YELLOW)Starting agent orchestration environment...$(NC)"; \
	docker compose -f $$COMPOSE_FILE up -d --wait quicbaas ollama 2>&1 | tail -10; \
	echo "$(YELLOW)Pulling Ollama models...$(NC)"; \
	docker compose -f $$COMPOSE_FILE exec -T ollama ollama pull all-minilm 2>&1 | tail -1; \
	docker compose -f $$COMPOSE_FILE exec -T ollama ollama pull tinyllama 2>&1 | tail -1; \
	echo "$(GREEN)Environment ready$(NC)"; \
	echo "$(BLUE)Running E2E agent orchestration tests...$(NC)"; \
	QUICBAAS_URL=http://localhost:8080 \
		OLLAMA_URL=http://localhost:11434 \
		GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' \
		$(GOTEST) -v -tags 'integration agents' -timeout 600s \
		./test/integration/agents/... 2>&1 | tee integration-test-agents.log; \
	TEST_EXIT=$$?; \
	echo "$(BLUE)Stopping agent environment...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --remove-orphans; \
	if [ $$TEST_EXIT -eq 0 ]; then \
		echo "$(GREEN)✓ E2E agent orchestration tests PASSED$(NC)"; \
	else \
		echo "$(RED)✗ E2E agent orchestration tests FAILED$(NC)"; \
	fi; \
	exit $$TEST_EXIT

.PHONY: coverage-orchestration
coverage-orchestration: ## Generate coverage report for orchestration package
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -tags 'rag mcp $(BUILD_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-orchestration.out -covermode=atomic ./pkg/orchestration/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-orchestration.out -o $(COVERAGE_DIR)/coverage-orchestration.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-orchestration.out | grep total

.PHONY: bench-orchestration
bench-orchestration: ## Run orchestration package benchmarks
	@echo "$(BLUE)Running orchestration benchmarks...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -tags 'rag mcp $(BUILD_TAGS)' -bench=. -benchmem -run=^$$ ./pkg/orchestration/...

# =============================================================================
# Pooled Provider Account Tests (the `quicbaas agent account` surface)
#
# The account surface spans six transports plus the SDK and CLI, so its
# targets fan out across every package that carries a piece of it rather than
# a single directory.
# =============================================================================

ACCOUNT_PKGS := ./pkg/service/... ./pkg/api/rest/... ./pkg/api/grpc/... ./pkg/api/quic/... ./pkg/api/mcp/... ./cmd/quicbaas/...

.PHONY: test-account
test-account: ## Run pooled provider account unit tests (service + all transports + CLI)
	@echo "$(BLUE)Running pooled provider account tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -count=1 -tags '$(BUILD_TAGS)' -run 'Account' $(ACCOUNT_PKGS)
	@echo "$(BLUE)Running SDK account client + transport tests...$(NC)"
	cd sdk/go && GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -count=1 -run 'Account' ./client/account/... ./transport/...

.PHONY: coverage-account
coverage-account: ## Generate coverage report for the pooled provider account surface
	@mkdir -p $(COVERAGE_DIR)
	cd sdk/go && GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -coverprofile=../../$(COVERAGE_DIR)/coverage-account.out -covermode=atomic ./client/account/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-account.out -o $(COVERAGE_DIR)/coverage-account.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-account.out | grep total

.PHONY: bench-account
bench-account: ## Run pooled provider account benchmarks
	@echo "$(BLUE)Running pooled provider account benchmarks...$(NC)"
	cd sdk/go && GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -bench=. -benchmem -run=^$$ ./client/account/...

.PHONY: integration-test-account
integration-test-account: ## Run pooled provider account CLI integration tests (all transports)
	@echo "$(BLUE)Running pooled provider account CLI integration tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -count=1 -tags 'integration $(INTEGRATION_BUILD_TAGS)' \
		-run 'TestAgentAccount' ./test/integration/cli/...

# =============================================================================
# KV Store Adapter Tests
# =============================================================================

.PHONY: test-kvstore
test-kvstore: ## Run kvstore package unit tests
	@echo "$(BLUE)Running kvstore tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -count=1 -tags '$(BUILD_TAGS)' ./pkg/kvstore/...

.PHONY: coverage-kvstore
coverage-kvstore: ## Generate coverage report for kvstore package
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -tags '$(BUILD_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-kvstore.out -covermode=atomic ./pkg/kvstore/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-kvstore.out -o $(COVERAGE_DIR)/coverage-kvstore.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-kvstore.out | grep total

.PHONY: bench-kvstore
bench-kvstore: ## Run kvstore package benchmarks
	@echo "$(BLUE)Running kvstore benchmarks...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -tags '$(BUILD_TAGS)' -bench=. -benchmem -run=^$$ ./pkg/kvstore/...

# =============================================================================
# Go SDK Tests
# =============================================================================

.PHONY: test-sdk-go
test-sdk-go: ## Run Go SDK unit tests (excludes integration tests)
	@echo "$(BLUE)Running Go SDK unit tests...$(NC)"
	@cd sdk/go && go test -v -race -count=1 $$(go list ./... | grep -v '/test/')
	@echo "$(GREEN)✓ Go SDK unit tests passed$(NC)"

.PHONY: test-sdk-go-coverage
test-sdk-go-coverage: ## Run Go SDK tests with coverage report (excludes integration tests)
	@echo "$(BLUE)Running Go SDK tests with coverage...$(NC)"
	@cd sdk/go && go test -coverprofile=coverage.out -covermode=atomic $$(go list ./... | grep -v '/test/')
	@echo ""
	@echo "$(GREEN)Coverage Summary:$(NC)"
	@cd sdk/go && go tool cover -func=coverage.out | grep -E "^(github|total)"
	@echo ""

.PHONY: test-sdk-go-coverage-html
test-sdk-go-coverage-html: test-sdk-go-coverage ## Generate HTML coverage report for Go SDK
	@echo "$(BLUE)Generating HTML coverage report...$(NC)"
	@cd sdk/go && go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: sdk/go/coverage.html$(NC)"

# Note: integration-test-sdk-go is defined in the dynamic section above

.PHONY: integration-test-sdk-go-mock
integration-test-sdk-go-mock: ## Run Go SDK mock-based tests only (no Docker required)
	@echo "$(BLUE)Running Go SDK mock-based tests...$(NC)"
	@cd sdk/go && MOCK_ONLY=true go test -v -race -count=1 ./test/integration/... ./test/mock/...
	@echo "$(GREEN)✓ Go SDK mock tests passed$(NC)"

.PHONY: bench-sdk-go
bench-sdk-go: ## Run Go SDK benchmarks
	@echo "$(BLUE)Running Go SDK benchmarks...$(NC)"
	@cd sdk/go && go test -bench=. -benchmem ./...

# ==============================================================================
# Secrets Config Resolution Package Targets
# ==============================================================================

.PHONY: test-secrets-resolver
test-secrets-resolver: ## Run secrets config resolution unit tests
	@echo "$(BLUE)Running secrets resolver unit tests...$(NC)"
	@$(GOTEST) -v -race -count=1 ./pkg/config/secrets/...
	@echo "$(GREEN)✓ Secrets resolver tests passed$(NC)"

.PHONY: coverage-secrets-resolver
coverage-secrets-resolver: ## Generate coverage report for secrets resolver
	@echo "$(BLUE)Running secrets resolver tests with coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) -tags '$(TEST_TAGS)' -coverprofile=$(COVERAGE_DIR)/secrets-resolver.out -covermode=atomic ./pkg/config/secrets/...
	@echo ""
	@echo "$(GREEN)Coverage Summary:$(NC)"
	@go tool cover -func=$(COVERAGE_DIR)/secrets-resolver.out | grep -E "^(github|total)"
	@echo ""

.PHONY: coverage-secrets-resolver-html
coverage-secrets-resolver-html: coverage-secrets-resolver ## Generate HTML coverage report for secrets resolver
	@echo "$(BLUE)Generating HTML coverage report...$(NC)"
	@go tool cover -html=$(COVERAGE_DIR)/secrets-resolver.out -o $(COVERAGE_DIR)/secrets-resolver.html
	@echo "$(GREEN)Coverage report generated: $(COVERAGE_DIR)/secrets-resolver.html$(NC)"

.PHONY: bench-secrets-resolver
bench-secrets-resolver: ## Run secrets resolver benchmarks
	@echo "$(BLUE)Running secrets resolver benchmarks...$(NC)"
	@$(GOTEST) -bench=. -benchmem -benchtime=$(BENCHTIME) ./pkg/config/secrets/...

# =============================================================================
# File Storage Targets
# =============================================================================

.PHONY: test-storage-file
test-storage-file: ## Run file storage unit tests
	@echo "$(BLUE)Running file storage unit tests...$(NC)"
	@$(GOTEST) -v -race -count=1 ./pkg/storage/file/...

.PHONY: coverage-storage-file
coverage-storage-file: ## Generate coverage report for file storage
	@echo "$(BLUE)Running file storage tests with coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) -tags '$(TEST_TAGS)' -coverprofile=$(COVERAGE_DIR)/storage-file.out -covermode=atomic ./pkg/storage/file/...
	@echo ""
	@echo "$(GREEN)Coverage Summary:$(NC)"
	@go tool cover -func=$(COVERAGE_DIR)/storage-file.out | grep -E "^(github|total)"
	@echo ""

.PHONY: coverage-storage-file-html
coverage-storage-file-html: coverage-storage-file ## Generate HTML coverage report for file storage
	@echo "$(BLUE)Generating HTML coverage report...$(NC)"
	@go tool cover -html=$(COVERAGE_DIR)/storage-file.out -o $(COVERAGE_DIR)/storage-file.html
	@echo "$(GREEN)Coverage report generated: $(COVERAGE_DIR)/storage-file.html$(NC)"

.PHONY: integration-test-storage-file
integration-test-storage-file: ## Run file storage integration tests
	@echo "$(BLUE)Running file storage integration tests...$(NC)"
	@$(GOTEST) -v -race -count=1 -tags=integration ./pkg/storage/file/...

.PHONY: bench-storage-file
bench-storage-file: ## Run file storage benchmarks
	@echo "$(BLUE)Running file storage benchmarks...$(NC)"
	@$(GOTEST) -bench=. -benchmem -benchtime=$(BENCHTIME) ./pkg/storage/file/...

# =============================================================================
# HNSW Distance Functions (go-qrdb)
# =============================================================================
# The HNSW distance tests live in go-qrdb and run in go-qrdb's own CI.
# They were previously delegated into .deps/go-qrdb, a vendored sibling
# working-tree copy removed at the 2026-08-13 dependency-resolution cutover
# (deps resolve from Gitea as tagged modules; nothing builds from .deps).

.PHONY: test-hnsw-distance
test-hnsw-distance: ## (moved) HNSW tests run in go-qrdb's own repo/CI
	@echo "test-hnsw-distance moved to go-qrdb (dependency-resolution.md); run it there."

.PHONY: bench-hnsw-distance
bench-hnsw-distance: ## (moved) HNSW benchmarks run in go-qrdb's own repo/CI
	@echo "bench-hnsw-distance moved to go-qrdb (dependency-resolution.md); run it there."

.PHONY: coverage-hnsw-distance
coverage-hnsw-distance: ## (moved) HNSW coverage runs in go-qrdb's own repo/CI
	@echo "coverage-hnsw-distance moved to go-qrdb (dependency-resolution.md); run it there."


# ==============================================================================
# Vector Filter Tests
# ==============================================================================

.PHONY: test-vector-filter
test-vector-filter: ## Run vector filter unit tests
	@echo "$(BLUE)Running vector filter unit tests...$(NC)"
	@$(GOTEST) -v -race -count=1 ./pkg/vector/filter/...

.PHONY: coverage-vector-filter
coverage-vector-filter: ## Generate vector filter coverage report
	@echo "$(BLUE)Running vector filter tests with coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/vector-filter.out -covermode=atomic ./pkg/vector/filter/...
	@echo ""
	@echo "$(GREEN)Coverage Summary:$(NC)"
	@go tool cover -func=$(COVERAGE_DIR)/vector-filter.out | grep -E "^(github|total)"
	@echo ""

.PHONY: coverage-vector-filter-html
coverage-vector-filter-html: coverage-vector-filter ## Generate HTML coverage report for vector filter
	@echo "$(BLUE)Generating HTML coverage report...$(NC)"
	@go tool cover -html=$(COVERAGE_DIR)/vector-filter.out -o $(COVERAGE_DIR)/vector-filter.html
	@echo "$(GREEN)Coverage report generated: $(COVERAGE_DIR)/vector-filter.html$(NC)"

.PHONY: bench-vector-filter
bench-vector-filter: ## Run vector filter benchmarks
	@echo "$(BLUE)Running vector filter benchmarks...$(NC)"
	@$(GOTEST) -bench=. -benchmem -benchtime=$(BENCHTIME) ./pkg/vector/filter/...

# ==============================================================================
# Vector Scalar Index Tests
# ==============================================================================

.PHONY: test-vector-scalar-index
test-vector-scalar-index: ## Run scalar index unit tests (go-qrdb)
	@echo "$(BLUE)Running scalar index unit tests...$(NC)"
	@echo "test-vector-scalar-index moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

.PHONY: coverage-vector-scalar-index
coverage-vector-scalar-index: ## Generate scalar index coverage (go-qrdb)
	@echo "$(BLUE)Running scalar index coverage...$(NC)"
	@echo "coverage-vector-scalar-index moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

.PHONY: bench-vector-scalar-index
bench-vector-scalar-index: ## Run scalar index benchmarks (go-qrdb)
	@echo "$(BLUE)Running scalar index benchmarks...$(NC)"
	@echo "bench-vector-scalar-index moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

# ==============================================================================
# GPU Vector Index Tests
# ==============================================================================

# GPU Build Configuration
WITH_GPU ?= 0

ifeq ($(WITH_GPU),1)
  BUILD_TAGS += gpu
  CGO_ENABLED := 1
endif

.PHONY: test-vector-gpu
test-vector-gpu: ## Run GPU unit tests (stubs without GPU hardware)
	@echo "$(BLUE)Running GPU vector index unit tests...$(NC)"
	@$(GOTEST) -v -race -count=1 -tags 'vector qrdb_vector' ./pkg/server/...
	@echo "test-vector-gpu moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

.PHONY: coverage-vector-gpu
coverage-vector-gpu: ## Generate GPU vector index coverage (go-qrdb)
	@echo "$(BLUE)Running GPU vector index coverage...$(NC)"
	@echo "coverage-vector-gpu moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

.PHONY: bench-vector-gpu
bench-vector-gpu: ## Run GPU benchmarks (requires GPU hardware)
	@echo "$(BLUE)Running GPU vector index benchmarks...$(NC)"
	@echo "bench-vector-gpu moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

.PHONY: build-gpu
build-gpu: ## Build with GPU acceleration (requires NVIDIA cuVS)
	WITH_GPU=1 $(MAKE) build


# ==============================================================================
# Vector Quantization Tests
# ==============================================================================

.PHONY: test-vector-quantization
test-vector-quantization: ## Run quantization unit tests (go-qrdb)
	@echo "$(BLUE)Running vector quantization unit tests...$(NC)"
	@echo "test-vector-quantization moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

.PHONY: coverage-vector-quantization
coverage-vector-quantization: ## Generate quantization coverage (go-qrdb)
	@echo "$(BLUE)Running vector quantization coverage...$(NC)"
	@echo "coverage-vector-quantization moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

.PHONY: bench-vector-quantization
bench-vector-quantization: ## Run quantization benchmarks (go-qrdb)
	@echo "$(BLUE)Running vector quantization benchmarks...$(NC)"
	@echo "bench-vector-quantization moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."
# ==============================================================================
# Vector Distributed Search Tests
# ==============================================================================

.PHONY: test-vector-distributed
test-vector-distributed: ## Run distributed search unit tests
	@echo "$(BLUE)Running distributed search unit tests...$(NC)"
	@$(GOTEST) -v -race -count=1 -run 'TestMerge|TestDistributed|TestShardRouter|TestDedup|TestResultHeap' ./pkg/service/...

.PHONY: coverage-vector-distributed
coverage-vector-distributed: ## Generate distributed search coverage
	@echo "$(BLUE)Running distributed search tests with coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/vector-distributed.out -covermode=atomic -run 'TestMerge|TestDistributed|TestShardRouter|TestDedup|TestResultHeap' ./pkg/service/...
	@echo ""
	@echo "$(GREEN)Coverage Summary:$(NC)"
	@go tool cover -func=$(COVERAGE_DIR)/vector-distributed.out | grep -E "^(github|total)"
	@echo ""

.PHONY: coverage-vector-distributed-html
coverage-vector-distributed-html: coverage-vector-distributed ## Generate HTML coverage report for distributed search
	@echo "$(BLUE)Generating HTML coverage report...$(NC)"
	@go tool cover -html=$(COVERAGE_DIR)/vector-distributed.out -o $(COVERAGE_DIR)/vector-distributed.html
	@echo "$(GREEN)Coverage report generated: $(COVERAGE_DIR)/vector-distributed.html$(NC)"

.PHONY: bench-vector-distributed
bench-vector-distributed: ## Run distributed search benchmarks
	@echo "$(BLUE)Running distributed search benchmarks...$(NC)"
	@$(GOTEST) -bench=BenchmarkMerge -benchmem -benchtime=$(BENCHTIME) ./pkg/service/...

# ==============================================================================
# Auto-Index Recommender Tests
# ==============================================================================

.PHONY: test-vector-autoindex
test-vector-autoindex: ## Run auto-index recommender tests
	@echo "$(BLUE)Running auto-index recommender unit tests...$(NC)"
	@$(GOTEST) -v -race -count=1 ./pkg/vector/autoindex/...

.PHONY: coverage-vector-autoindex
coverage-vector-autoindex: ## Generate auto-index coverage
	@echo "$(BLUE)Running auto-index recommender coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/vector-autoindex.out -covermode=atomic ./pkg/vector/autoindex/...
	@echo ""
	@echo "$(GREEN)Coverage Summary:$(NC)"
	@go tool cover -func=$(COVERAGE_DIR)/vector-autoindex.out | grep -E "^(github|total)"
	@echo ""

.PHONY: coverage-vector-autoindex-html
coverage-vector-autoindex-html: coverage-vector-autoindex ## Generate HTML coverage report for auto-index
	@echo "$(BLUE)Generating HTML coverage report...$(NC)"
	@go tool cover -html=$(COVERAGE_DIR)/vector-autoindex.out -o $(COVERAGE_DIR)/vector-autoindex.html
	@echo "$(GREEN)Coverage report generated: $(COVERAGE_DIR)/vector-autoindex.html$(NC)"

# ==============================================================================
# Compaction State Machine Tests
# ==============================================================================

.PHONY: test-vector-compaction
test-vector-compaction: ## Run compaction state machine tests
	@echo "$(BLUE)Running compaction state machine unit tests...$(NC)"
	@$(GOTEST) -v -race -count=1 -run 'TestCompaction|TestInvalidTransition' ./pkg/service/...

.PHONY: coverage-vector-compaction
coverage-vector-compaction: ## Generate compaction state machine coverage
	@echo "$(BLUE)Running compaction state machine coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/vector-compaction.out -covermode=atomic -run 'TestCompaction|TestInvalidTransition' ./pkg/service/...
	@echo ""
	@echo "$(GREEN)Coverage Summary:$(NC)"
	@go tool cover -func=$(COVERAGE_DIR)/vector-compaction.out | grep vector_compaction
	@echo ""

# ==============================================================================
# Bloom Filter Tests (go-qrdb)
# ==============================================================================

.PHONY: test-vector-bloom
test-vector-bloom: ## Run bloom filter unit tests (go-qrdb)
	@echo "$(BLUE)Running bloom filter unit tests...$(NC)"
	@echo "test-vector-bloom moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

.PHONY: coverage-vector-bloom
coverage-vector-bloom: ## Generate bloom filter coverage (go-qrdb)
	@echo "$(BLUE)Running bloom filter coverage...$(NC)"
	@echo "coverage-vector-bloom moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

.PHONY: bench-vector-bloom
bench-vector-bloom: ## Run bloom filter benchmarks (go-qrdb)
	@echo "$(BLUE)Running bloom filter benchmarks...$(NC)"
	@echo "bench-vector-bloom moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

# ==============================================================================
# Aggregate Vector Coverage
# ==============================================================================

.PHONY: coverage-vector
coverage-vector: ## Run all vector package coverage
	@echo "$(BLUE)Running all vector coverage targets...$(NC)"
	@$(MAKE) coverage-hnsw-distance
	@$(MAKE) coverage-vector-filter
	@$(MAKE) coverage-vector-scalar-index
	@$(MAKE) coverage-vector-gpu
	@$(MAKE) coverage-vector-quantization
	@$(MAKE) coverage-vector-autoindex
	@$(MAKE) coverage-vector-compaction
	@$(MAKE) coverage-vector-distributed
	@echo "coverage-vector-bloom moved to go-qrdb (dependency-resolution.md); .deps was removed at the 2026-08-13 cutover."

# ==============================================================================
# Cross-Project Naming + Consistency (Phase 14 of seal→barrier rename)
# ==============================================================================

.PHONY: verify-naming
verify-naming: ## Fail if non-deprecated Seal* code paths still ship outside the documented alias surface
	@echo "$(BLUE)verify-naming: scanning for non-aliased Seal* code paths...$(NC)"
	@scripts/verify-naming.sh

.PHONY: verify-cli-flags
verify-cli-flags: ## Diff init/barrier/bootstrap flag sets across qrdbctl, xkmsctl, and quicbaas
	@echo "$(BLUE)verify-cli-flags: comparing CLI flag surfaces across qrdbctl/xkmsctl/quicbaas...$(NC)"
	@scripts/verify-cli-flags.sh

.PHONY: verify-api-shapes
verify-api-shapes: ## Diff QuicBaaS Barrier*/Ceremony*/Bootstrap* TS types against go-qrdb canonical Go types
	@echo "$(BLUE)verify-api-shapes: comparing TypeScript types against go-qrdb canonical Go types...$(NC)"
	@scripts/verify-api-shapes.sh

# Read a Makefile variable from a script: `make print-DOCKER_LOCAL_IMAGES`.
print-%:
	@echo "$($*)"

.PHONY: verify-clean-docker
verify-clean-docker: ## Fail if clean-docker reaches beyond this project's image tags or hides a failure
	@echo "$(BLUE)verify-clean-docker: checking cleanup scope and failure propagation...$(NC)"
	@bash scripts/verify-clean-docker.sh

.PHONY: verify-integration-teardown
verify-integration-teardown: ## Fail if the integration stack can outlive a run that left early
	@echo "$(BLUE)verify-integration-teardown: checking the stack comes down on the early-exit path...$(NC)"
	@bash scripts/verify-integration-teardown.sh

.PHONY: verify
verify: verify-naming verify-cli-flags verify-api-shapes verify-clean-docker verify-integration-teardown ## Run all cross-project verifications
	@echo "$(GREEN)All verifications OK$(NC)"

# =============================================================================
# Server Package Tests
# =============================================================================

.PHONY: coverage-server-iam
coverage-server-iam: ## Generate coverage report for pkg/server (IAM always compiled)
	@mkdir -p $(COVERAGE_DIR)
	$(GOTEST) -count=1 -tags '$(IAM_TEST_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-server-iam.out -covermode=atomic ./pkg/server/
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-server-iam.out -o $(COVERAGE_DIR)/coverage-server-iam.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-server-iam.out | tail -1

.PHONY: coverage-server-full
coverage-server-full: ## Merge unit + integration coverage for pkg/server (real daemon-boot coverage)
	@echo "$(BLUE)Generating combined coverage for pkg/server/...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	@echo "$(YELLOW)Stage 1/3: unit coverage (covermode=atomic, full integration tags)$(NC)"
	@# covermode=atomic matches the daemon binary, which is built with
	@# 'go build -cover -covermode=atomic' so that runtime/coverage.WriteCountersDir
	@# can flush deterministically on graceful shutdown. Matching modes lets
	@# gocovmerge union the unit profile with the daemon's textfmt profile.
	@# Full integration build tags are used so the instrumented source blocks line
	@# up with the daemon binary (which is built with the same tag set).
	@$(GOTEST) -count=1 -tags '$(INTEGRATION_BUILD_TAGS)' \
		-coverpkg=github.com/jeremyhahn/go-quicbaas/pkg/server \
		-coverprofile=$(COVERAGE_DIR)/coverage-server-unit.out \
		-covermode=atomic ./pkg/server/
	@echo "$(YELLOW)Stage 2/3: daemon-boot coverage from the running cluster$(NC)"
	@if [ "$$INTEGRATION_COVERAGE" = "1" ]; then \
		echo "$(BLUE)Building coverage-instrumented daemon and running the integration cluster...$(NC)"; \
		COVDATA_BASE=$$(pwd)/$(COVERAGE_DIR)/covdata; \
		rm -rf "$$COVDATA_BASE"; \
		mkdir -p "$$COVDATA_BASE/node1" "$$COVDATA_BASE/node2" "$$COVDATA_BASE/node3"; \
		chmod -R 0777 "$$COVDATA_BASE"; \
		COVER=1 \
		QUICBAAS_NODE_GOCOVERDIR=/covdata \
		QUICBAAS_COVDATA_DIR_NODE1="$$COVDATA_BASE/node1" \
		QUICBAAS_COVDATA_DIR_NODE2="$$COVDATA_BASE/node2" \
		QUICBAAS_COVDATA_DIR_NODE3="$$COVDATA_BASE/node3" \
		$(MAKE) integration-test || true; \
		echo "$(YELLOW)Extracting daemon coverage from $$COVDATA_BASE (covdata textfmt)...$(NC)"; \
		COVDIRS=$$(find "$$COVDATA_BASE" -maxdepth 1 -mindepth 1 -type d | paste -sd, -); \
		if [ -n "$$COVDIRS" ] && [ -n "$$(find "$$COVDATA_BASE" -name 'covmeta.*' -print -quit)" ]; then \
			$(GOCMD) tool covdata textfmt -i="$$COVDIRS" -o=$(COVERAGE_DIR)/coverage-server-integration-raw.out; \
			echo "mode: atomic" > $(COVERAGE_DIR)/coverage-server-integration.out; \
			grep "github.com/jeremyhahn/go-quicbaas/pkg/server/" $(COVERAGE_DIR)/coverage-server-integration-raw.out >> $(COVERAGE_DIR)/coverage-server-integration.out || true; \
			echo "$(GREEN)  ✓ Daemon coverage extracted ($$(go tool cover -func=$(COVERAGE_DIR)/coverage-server-integration.out 2>/dev/null | tail -1 | awk '{print $$NF}') from daemon boot)$(NC)"; \
		else \
			echo "$(RED)  ✗ No covdata emitted by the daemon (covmeta.* missing under $$COVDATA_BASE)$(NC)"; \
			echo "$(RED)    The instrumented daemon did not flush coverage — check graceful shutdown / GOCOVERDIR.$(NC)"; \
			echo "mode: atomic" > $(COVERAGE_DIR)/coverage-server-integration.out; \
		fi; \
	else \
		echo "$(YELLOW)Skipping integration stage (set INTEGRATION_COVERAGE=1 to include)$(NC)"; \
		echo "mode: atomic" > $(COVERAGE_DIR)/coverage-server-integration.out; \
	fi
	@echo "$(YELLOW)Stage 3/3: merging unit + daemon-boot profiles$(NC)"
	@$(GOCMD) run github.com/wadey/gocovmerge@latest \
		$(COVERAGE_DIR)/coverage-server-unit.out \
		$(COVERAGE_DIR)/coverage-server-integration.out \
		> $(COVERAGE_DIR)/coverage-server-merged.out 2>$(COVERAGE_DIR)/gocovmerge.err || \
		{ echo "$(RED)gocovmerge failed:$(NC)"; cat $(COVERAGE_DIR)/gocovmerge.err; \
		  cp $(COVERAGE_DIR)/coverage-server-unit.out $(COVERAGE_DIR)/coverage-server-merged.out; }
	@$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-server-merged.out -o $(COVERAGE_DIR)/coverage-server-merged.html
	@echo "$(GREEN)Combined coverage report: $(COVERAGE_DIR)/coverage-server-merged.html$(NC)"
	@echo "$(BLUE)Unit-only:$(NC)"; $(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-server-unit.out | tail -1
	@echo "$(BLUE)Combined (unit + daemon-boot):$(NC)"; $(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-server-merged.out | tail -1

SUBMODULE_TAGS := sdk/go
# Builds never resolve through a workspace: modules come from Gitea tags
# (dependency-resolution.md §2). Immunizes against stray go.work files.
export GOWORK=off

# ---- dependency-resolution.md targets (2026-08-13 cutover) ----
# SUBMODULE_TAGS lists nested modules tagged in lockstep with the root
# (release-versioning.md P2). deps-latest runs before compiling in a
# consumer repo; release-alpha publishes this repo's next -alpha tag to dev.
# deps-latest picks the highest tag INCLUDING -alpha prereleases — bare
# 'go get @latest' ignores prereleases and would downgrade to legacy v0.0.0.

.PHONY: release-alpha deps-latest

release-alpha:
	@V=$$(git describe --tags --abbrev=0 --match 'v*' 2>/dev/null || echo v0.0.0-alpha); \
	NEXT=$$(echo $$V | awk -F'[.-]' '{printf "v%s.%s.%d-alpha", substr($$1,2), $$2, $$3+1}'); \
	echo $${NEXT#v} > VERSION && git add VERSION && git commit -q -m "chore(release): $$NEXT" || true; \
	git tag $$NEXT; \
	for s in $(SUBMODULE_TAGS); do git tag "$$s/$$NEXT"; done; \
	git push dev HEAD --tags && echo "Tagged $$NEXT"

deps-latest:
	@GOWORK=off sh -c ' \
	SELF=$$(go list -m); \
	for m in $$(go list -m -f "{{if not .Indirect}}{{.Path}}{{end}}" all 2>/dev/null | grep "^github.com/jeremyhahn/" | grep -vx "$$SELF"); do \
	  case "$$(go list -m -f "{{if .Replace}}{{.Replace.Path}}{{end}}" "$$m" 2>/dev/null)" in .*|/*) continue;; esac; \
	  V=$$(go list -m -versions "$$m" 2>/dev/null | tr " " "\n" | tail -1); \
	  if [ -n "$$V" ] && [ "$$V" != "$$m" ]; then go get "$$m@$$V" || exit 1; fi; \
	done; go mod tidy'

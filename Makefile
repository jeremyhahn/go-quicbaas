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
# The toolchain pins (GO_VERSION, GOLANGCI_VERSION, GOSEC_VERSION,
# GOVULNCHECK_VERSION, SERENA_VERSION, GOPLS_VERSION) live in ONE file that both
# this Makefile and the Go binaries read — see pkg/version/toolchain.mk for why.
include pkg/version/toolchain.mk

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
# GOLANGCI_VERSION, GOSEC_VERSION, GOVULNCHECK_VERSION, ACTIONLINT_VERSION,
# SHELLCHECK_VERSION (with its tarball digests), SERENA_VERSION, GOPLS_VERSION,
# GO_TOOL_DEPENDENCY_FLOORS (what gopls and gosec are built with) and
# OPENCODE_VERSION (with its tarball digests) are included above from
# pkg/version/toolchain.mk, the repository's only copy of them. `make install-tools` installs them, CI reads
# them through `make print-<VAR>` (.github/workflows/ci.yml), and the Software
# Factory worker image is built with them — by `make build-worker-image` and by
# the daemon's worker image refresh, which embeds the same file.

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
		$(PROTO_DIR)/*.proto $(PROTO_DIR)/iam/*.proto
	@echo "$(GREEN)Protobuf code generated successfully$(NC)"

.PHONY: proto-lint
proto-lint: ## Lint proto files
	@echo "$(BLUE)Linting proto files...$(NC)"
	@if ! command -v buf > /dev/null; then \
		echo "$(YELLOW)buf not installed, using protoc lint...$(NC)"; \
		$(PROTOC) --descriptor_set_out=/dev/null $(PROTO_DIR)/*.proto $(PROTO_DIR)/iam/*.proto; \
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
	@violations=$$(grep -rEn 'InsecureSkipVerify[[:space:]]*:[[:space:]]*true' --include="*.go" --exclude-dir='.deps' --exclude-dir='vendor' . \
		| grep -Ev '^[^:]+:[0-9]+:[[:space:]]*//|//nolint:gosec' \
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
	@$(MAKE) --no-print-directory vet-integration

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

# npm ci, not npm install — see build-sdk-typescript below.
_build-sdk-typescript:
	@echo "$(BLUE)Building TypeScript SDK...$(NC)"
	@cd sdk/typescript && npm ci --silent && node ./node_modules/typescript/bin/tsc
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

# npm ci, NOT npm install: install FROM sdk/typescript/package-lock.json
# without rewriting it. npm install re-resolves the tree and drops the optional
# platform-native entries that do not apply to the builder's host, so every
# build left a dirty tree, and committing that narrowed the lock to whichever
# platform built last. A lock npm ci refuses is stale: regenerate it on purpose,
# in its own commit.
.PHONY: build-sdk-typescript
build-sdk-typescript: ## Build TypeScript SDK (direct compilation)
	@echo "$(BLUE)Building TypeScript SDK...$(NC)"
	@cd sdk/typescript && npm ci --silent && node ./node_modules/typescript/bin/tsc
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
# file behind a constraint no test pass satisfies is skipped in silence:
# `go test` prints "no test files" and the target still goes green. Every
# CONSTRAINT under pkg/ or cmd/ must therefore evaluate TRUE under one of the
# passes below, or rest on a tag listed in TAG_GATE_EXEMPT with the reason it
# cannot run. `make check-build-tags` enforces that and is a prerequisite of
# `make test`.
#
# THE UNIT OF THE CHECK IS THE CONSTRAINT, NOT THE TAG, and the difference is
# the whole point (go#305). The gate used to ask whether every tag NAME
# appeared in some pass, which a `tr`-and-grep pipeline can answer and which is
# the wrong question: satisfiability is a property of the EXPRESSION.
# `rag_stub.go` is `rest && !rag`; both names appear in the maximal pass, so
# the old check passed, while the maximal pass sets both tags and the minimal
# pass sets neither, so nothing compiled the file. That is how the REST route
# goldens froze a surface the shipped build does not serve (go#299), and how
# `rest && !timeseries` survived after it. The inverse — turning a tag OFF in a
# pass with no `!tag` file behind it — cost pkg/sdlc every test in the minimal
# pass (go#304).
#
# scripts/buildtagcover parses each constraint with go/build/constraint, the
# same package the toolchain uses, and evaluates it against every pass, so
# `||`, `&&`, `!` and parentheses mean here exactly what they mean to the
# compiler. Platform tags and TAG_GATE_EXEMPT entries are treated as free
# variables: a constraint mentioning one is covered when SOME assignment of
# them satisfies it, which is honest, because such a tag is genuinely free to
# be either value depending on where the build runs.
#
#   TEST_TAGS          maximal pass: every default-on feature compiled in.
#   MINIMAL_TEST_TAGS  minimal pass: every tag WITH A DISABLED SIDE off -- not
#                      every optional tag, which is a different and wrong set
#                      (it took the codecs with it, go#304). So the
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

# MINIMAL_TEST_TAGS: the pass that compiles the DISABLED-FEATURE side of the
# tree -- the !rest / !quic / !mcp / !rag / !vector / !timeseries / !raft /
# !otel / !l2_redis / !desktop / !quicbaas_{mqtt,amqp,stomp} stub and no-op
# files, and the tests written to assert their behaviour. Nothing else
# compiles them: a tag and its negation are mutually exclusive within one
# `go test` invocation, so the maximal pass can never reach them.
#
# THE CRITERION, stated so the next person extending this knows it rather than
# guessing: A TAG BELONGS IN THE OFF-SET IF AND ONLY IF SOME FILE IN THE TREE
# IS GATED ON ITS NEGATION. Not "is it optional", not "does a WITH_* flag
# exist" — does something carry `!tag`. And it is MEASURED, not asserted:
# scripts/buildtagcover -list-negated computes the set from the constraints
# themselves, so it stays true as files are added.
#
# Two worked cases from getting it wrong, because the criterion is easier to
# apply than to derive:
#
#   codec_json  NO file is gated !codec_json, so it does not belong. Turning
#               it off bought no stub coverage and broke pkg/sdlc's DAO with
#               `codec "json" is not supported in this build` — go#304.
#   quicbaas_*  TWO files each are gated on their negations, so they DO
#               belong. An earlier attempt at this fix derived the set from
#               `make build-minimal` instead, which leaves the messaging
#               subprotocols ON, and would have deleted the only compilation
#               those three stub pairs get. Caught in review; the measurement
#               settled it.
#
# Both directions of getting it wrong are SILENT, which is why it is not a
# literal list. Omit a tag and its stubs are compiled by nothing while the
# pass still goes green -- go#299, and precisely what a hand list does the day
# someone adds a new `!tag` file. Include a tag with no off side and the pass
# turns off working code for no coverage: that was go#304, where the codec
# tags were disabled, nothing was gained, and pkg/sdlc's DAO failed every test
# with `codec "json" is not supported in this build`.
#
# The first version of this fix WAS a literal list, and review found it
# already false under valid overrides: `make test-minimal WITH_L2_REDIS=1`
# keeps l2_redis in BUILD_TAGS (see WITH_L2_REDIS above) and WITH_GUI=1 keeps
# desktop, so their stubs went uncompiled while this target claimed otherwise.
# Deriving the set is what makes the claim true for every override rather than
# for the default one someone happened to test.
#
# Note what the derivation does NOT remove: qrdb_ts survives, because no file
# is gated `!qrdb_ts`. It has no disabled side, so turning it off would be the
# go#304 mistake again.
#
# It adds NO TEST_EXTRA_TAGS: `otel` and `l2_redis` swap implementations, and
# compiling their !tag stubs is the point of this pass.
#
# $(shell) at parse time costs one `go run` (~0.2s warm). The $(if) guard is
# not decoration: a silent failure here would yield an EMPTY off-set, which
# subtracts nothing and quietly turns the minimal pass into the maximal one.
#
# BOTH ARE DEFERRED (`=`, not `:=`) SO ONLY THE TARGETS THAT NEED THEM PAY.
# An immediate assignment runs the helper during Makefile PARSING, which makes
# every target -- `make help` included -- depend on a working Go toolchain and
# a writable build cache. Review caught that as a real failure, not a
# hypothetical: in a sandbox with a read-only /tmp, plain `make -n help` died
# on `go: creating work dir ... read-only file system` before any recipe ran.
# A build-tag helper has no business breaking `make help`.
#
# The guard rides inside the expansion via $(or), which short-circuits: when
# the helper prints tags, $(error) is never expanded. It catches an EMPTY
# result, which is the failure mode this helper actually has -- it prints its
# one line at the end or not at all. $(shell) captures stdout and not exit
# status, so a command that printed a partial list AND THEN failed would slip
# through; that is not a shape this tool can produce, and the alternative
# (.SHELLSTATUS through a deferred expansion) buys less than it costs here.
MINIMAL_OFF_TAGS = $(or $(shell $(GOCMD) run ./scripts/buildtagcover -list-negated -dir pkg -dir cmd),$(error buildtagcover -list-negated produced no tags; refusing to build a minimal pass that turns nothing off))
MINIMAL_TEST_TAGS = !integration $(filter-out $(MINIMAL_OFF_TAGS),$(strip $(BUILD_TAGS)))

# STANDARD_TEST_TAGS: the `make build-standard` FEATURE SET -- REST on, the AI
# features off (WITH_VECTOR=0 WITH_RAG=0 WITH_MCP=0) -- carrying the same
# test-only gates every other pass carries.
#
# It is composed by exactly TEST_TAGS's rule with the AI features subtracted:
#
#   TEST_TAGS          = !integration + BUILD_TAGS + TEST_EXTRA_TAGS
#   STANDARD_TEST_TAGS = !integration + (the same, minus STANDARD_OFF_TAGS)
#
# so it cannot drift from TEST_TAGS in composition, only in feature set. It is
# NOT byte-identical to what `make build-standard` links: like TEST_TAGS, it
# adds the test-only gates (canary/otel/l2_redis), which swap in assertion and
# tracing variants that no shipped binary carries. That is the same liberty
# every pass here takes, and it is stated rather than implied.
#
# THE MAXIMAL AND MINIMAL PASSES CANNOT REACH THIS FEATURE SET, and go#299 is
# what that cost. A file gated on a CONJUNCTION of an enabled and a disabled
# tag compiles in neither: rag_stub.go is `rest && !rag`, so the maximal pass
# (rest AND rag) skips it and the minimal pass (neither) skips it too. The
# route goldens therefore froze the rag-enabled surface and were wrong by six
# routes for a binary this Makefile actually ships -- invisibly, because
# nothing compiled that combination.
#
# check-build-tags cannot catch this class on its own: it verifies every tag
# NAME appears in some pass, and both `rest` and `rag` do. Combinations need a
# pass, so this is one.
#
# KNOWN REMAINING HOLE: `rest && !timeseries` is still compiled by no pass
# (timeseries_handlers_noop.go and its test). This pass keeps timeseries
# because build-standard does; widening it would make this set correspond to
# no shipped configuration, which is the mistake go#299 was about. Tracked as
# go-quicbaas#305, together with the general point that check-build-tags
# checks tag NAMES and cannot see COMBINATIONS at all.
STANDARD_OFF_TAGS := vector qrdb_vector rag mcp
STANDARD_TEST_TAGS := !integration \
	$(filter-out $(STANDARD_OFF_TAGS),$(strip $(BUILD_TAGS)) $(TEST_EXTRA_TAGS))
DESKTOP_TEST_TAGS := !integration $(strip $(BUILD_TAGS)) $(TEST_EXTRA_TAGS) desktop webkit2_41

# LEAN_TEST_TAGS: the transports ON with the optional data-model features and
# the test-only gates OFF.
#
# NAMED FOR WHAT IT IS RATHER THAN FOR A BUILD TARGET, deliberately: unlike
# test-standard and test-minimal there is no `make build-lean`, and claiming
# otherwise is the go#299 mistake. The WITH_* combination is legal and
# supported (WITH_TIMESERIES=0 WITH_VECTOR=0 WITH_RAG=0 with REST/QUIC/MCP on),
# it simply has no named recipe.
#
# It exists because three constraints in the tree are satisfied by no other
# pass, and `make check-build-tags` can now prove it:
#
#	mcp && !rag		pkg/api/mcp/rag_mcp_stub.go, rag_index_tools_stub.go
#	rest && !otel		pkg/api/rest/tracing_noop.go
#	rest && !timeseries	pkg/api/rest/timeseries_handlers_noop.go + its test
#
# Each needs one feature ON and another OFF, which the maximal pass (all on)
# and the minimal pass (all off) both miss by construction. `rest && !otel` is
# the sharpest of the three: `otel` is a TEST-ONLY tag, so the no-op tracing
# path it guards is what EVERY shipped binary runs and no test compiled it.
#
# It arrived red, which is the argument for it: TestSDKToolNameContract failed
# immediately, because with mcp on and rag off the RAG tools are correctly
# unregistered while the SDK still advertises the verbs (go#305).
LEAN_OFF_TAGS := vector qrdb_vector rag timeseries qrdb_ts
LEAN_TEST_TAGS := !integration $(filter-out $(LEAN_OFF_TAGS),$(strip $(BUILD_TAGS)))

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
# openvino_live drives a REAL OpenVINO Model Server on a REAL NPU
# (pkg/orchestration/openvino_backend_live_test.go). It is exempt for the
# reason gpu and swtpm are: no CI runner in this fleet has the accelerator, and
# a pass that compiled it could not run it. Drive it by hand against a served
# model:
#
#   OPENVINO_BASE_URL=http://gpu-host:9000 OPENVINO_MODEL=qwen3-8b \
#     go test -tags openvino_live -run TestOpenVINOBackend_Live ./pkg/orchestration/
TAG_GATE_EXEMPT := linux darwin windows unix ignore integration coverage production \
	pkcs11 swtpm tpm_simulator \
	awskms gcpkms azurekv vault awss3 gcpstorage azureblob minio \
	gpu quicbaas_signal \
	openvino_live

# IAM_TEST_TAGS: retained alias for TEST_TAGS. IAM is now always compiled
# (the 'iam' build tag was removed in phase C1-G), so no extra tag is needed
# for packages under pkg/iam/... or pkg/server/. Kept as an alias so existing
# test/coverage/bench targets continue to reference a stable variable name.
IAM_TEST_TAGS := $(TEST_TAGS)

# The -timeout below is CONTENTION HEADROOM, not an expectation that any
# package needs ten minutes alone. The cap is a per-BINARY wall clock, so it has
# to survive `-p 2` plus whatever else shares the host, and this repo's runs land
# beside other agents' suites routinely. Measured 2026-08-20: pkg/server takes
# 58s alone under the full tag set and still hit the old 5m cap twice inside
# `make test` on a shared box -- a timeout with zero assertion failures, which
# reads exactly like a real red and costs an investigation to disprove. A false
# red is worse than a slow one. Ten minutes is the failure bound; a package that
# genuinely takes minutes alone is a defect to fix, not a budget to raise again.
#
# Keep prose OUT of the recipe: these lines sit inside a `\`-continued shell
# command, where a `@#` line is handed to bash as a COMMAND and fails with
# exit 127. That is exactly how this comment broke `make test` in CI run 4119.
.PHONY: test
test: check-insecure-tls check-build-tags check-coded-sentinels check-local-keygen check-case-collisions ## Run unit tests (excludes integration tests)
	@echo "$(BLUE)Running unit tests (excludes integration tests)...$(NC)"
ifneq ($(strip $(BUILD_TAGS)),)
	@echo "$(YELLOW)Build tags: $(TEST_TAGS)$(NC)"
endif
	@START_TIME=$$(date +%s); \
	$(GOTEST) -v -p 2 -tags '$(TEST_TAGS)' -timeout=10m ./pkg/... ./cmd/... ./scripts/...; \
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

.PHONY: check-case-collisions
# check-case-collisions fails when two tracked paths differ only by letter
# case. The Go module proxy refuses to zip such a tree ("case-insensitive file
# name collision"), so one release was unusable by every consumer (v0.13.0-alpha).
check-case-collisions: ## Fail if two tracked paths differ only by letter case
	@dups=$$(git ls-files | LC_ALL=C sort -f | uniq -di); \
	if [ -n "$$dups" ]; then echo "check-case-collisions: paths that differ only by case:"; echo "$$dups"; exit 1; fi

.PHONY: check-local-keygen
# go-xkms owns keys and certificates for the whole stack; go-quicbaas composes
# it (repo-root CLAUDE.md, Layer Ownership). A key pair created in this process
# cannot be protected by the TPM or the HSM that is the entire point of go-xkms,
# and a certificate signed here bypasses the CA's profiles, serial policy,
# SPIFFE synthesis and certificate store.
#
# go-quicbaas#910 found six local ecdsa/rsa/ed25519.GenerateKey calls across
# three packages -- one of them in an adapter whose own sibling file documents
# go-xkms as the canonical issuer -- and nothing in the build objected. The
# refactor is worth no more than this gate: without it the next one lands the
# same way and waits for an owner to notice.
#
# Scope is pkg, cmd, sdk AND test. test/ used to be excluded outright on the
# grounds that it is test fixtures all the way down; go-quicbaas#943 found what
# grew in that blind spot -- a 444-line parallel certificate authority the whole
# integration suite depended on, which meant the suite proved the system worked
# against certificates no part of the product issues. The fixtures now come from
# the product's CA (test/integration/certfixture) and the tree is in scope, so
# the next one cannot grow the same way.
#
# _test.go files are still excluded, uniformly across every tree: a unit test
# minting a throwaway self-signed certificate that chains to nothing is not a
# key-custody decision. `-include-tests` reports them anyway for an audit. A
# site that genuinely has no alternative waives itself in place with
# `// local-keygen-ok: <reason>`, so the justification lives next to the code;
# pkg/signal carries the only two.
check-local-keygen: ## Fail if any non-test site creates a key pair or signs a certificate itself
	@echo "$(BLUE)Checking for local key/certificate generation outside go-xkms...$(NC)"
	@$(GOCMD) run ./scripts/localkeygen -dir pkg -dir cmd -dir sdk -dir test

.PHONY: check-coded-sentinels
# A qrdb-coded sentinel is a *pkg/errors.QBError, which is a type alias for
# go-qrdb's *QRDBError, and QRDBError.Is compares the error CODE alone. So
# `errors.Is(err, iam.ErrInviteExpired)` is true for every Forbidden error that
# reaches it -- a spent invite, a rejected PIV certificate, a suspended tenant.
# The call site reads like sentinel matching and is not; it over-matches, and it
# does so silently. go-quicbaas#434 found a 422 branch that could never run, an
# authentication route that described why a proof failed, and a token-liveness
# rule that admitted a token because the store answered not-found.
#
# A site that really does mean the whole code class waives itself in place with
# a `// coded-sentinel-ok: <reason>` comment, so the justification lives next to
# the code rather than in a list somewhere else. An empty reason is not a
# waiver.
check-coded-sentinels: ## Fail if errors.Is targets a qrdb-coded sentinel without a stated reason
	@echo "$(BLUE)Checking errors.Is targets against qrdb-coded sentinels...$(NC)"
	@$(GOCMD) run ./scripts/codedsentinel -dir pkg -dir cmd -dir sdk

.PHONY: check-build-tags
# The -pass / -localpass split is not cosmetic: -pass means CI RUNS IT, and a
# constraint reachable only from a -localpass is reported as a note rather than
# counted, because "a make target covers it" and "the pipeline compiles it" are
# different claims. Conflating them would rebuild this gate's own blind spot
# one level up.
#
#   minimal  a -pass as of this change, which is the commit that wires
#            `make test-minimal` into the workflow. It was a -localpass while
#            that step did not exist; leaving it one afterwards would have the
#            gate report eight constraints as reaching no CI job when the job
#            had just been added — a false claim in the direction that hides
#            work already done.
#   desktop  needs webkit2gtk, which the runners do not have. `make test` must
#            stay runnable with no GUI toolchain, so this one stays a note
#            until that is a decision someone makes deliberately.
check-build-tags: ## Fail if any //go:build CONSTRAINT is satisfied by no test pass
	@echo "$(BLUE)Checking build-constraint coverage of the test gate...$(NC)"
	@$(GOCMD) run ./scripts/buildtagcover \
		-dir pkg -dir cmd \
		-free '$(TAG_GATE_EXEMPT)' \
		-pass 'maximal:$(TEST_TAGS)' \
		-pass 'standard:$(STANDARD_TEST_TAGS)' \
		-pass 'lean:$(LEAN_TEST_TAGS)' \
		-pass 'minimal:$(MINIMAL_TEST_TAGS)' \
		-localpass 'desktop:$(DESKTOP_TEST_TAGS)'

.PHONY: test-minimal
test-minimal: check-insecure-tls check-build-tags check-local-keygen ## Run unit tests with every tag that has a !tag side off (compiles the stub/no-op paths)
	@echo "$(BLUE)Running unit tests with every disabled-side tag off...$(NC)"
	@echo "$(YELLOW)Build tags: $(MINIMAL_TEST_TAGS)$(NC)"
	$(GOTEST) -p 2 -tags '$(MINIMAL_TEST_TAGS)' -timeout=10m ./pkg/... ./cmd/...

.PHONY: test-standard
test-standard: check-insecure-tls check-build-tags check-local-keygen ## Run unit tests in the build-standard configuration (REST on, AI features off)
	@echo "$(BLUE)Running unit tests in the build-standard configuration...$(NC)"
	@echo "$(YELLOW)Build tags: $(STANDARD_TEST_TAGS)$(NC)"
	$(GOTEST) -p 2 -tags '$(STANDARD_TEST_TAGS)' -timeout=10m ./pkg/... ./cmd/...

.PHONY: test-lean
test-lean: check-insecure-tls check-build-tags check-local-keygen ## Run unit tests with the transports on and the optional data-model features off
	@echo "$(BLUE)Running unit tests with the transports on, data-model extras off...$(NC)"
	@echo "$(YELLOW)Build tags: $(LEAN_TEST_TAGS)$(NC)"
	$(GOTEST) -p 2 -tags '$(LEAN_TEST_TAGS)' -timeout=10m ./pkg/... ./cmd/...

.PHONY: test-desktop
test-desktop: check-insecure-tls check-build-tags check-local-keygen ## Run the Wails desktop binding tests (needs webkit2gtk dev libraries)
	@echo "$(BLUE)Running desktop GUI binding tests...$(NC)"
	@echo "$(YELLOW)Build tags: $(DESKTOP_TEST_TAGS)$(NC)"
	$(GOTEST) -p 2 -tags '$(DESKTOP_TEST_TAGS)' -timeout=10m ./cmd/...

.PHONY: test-all-tags
test-all-tags: test test-minimal test-standard test-lean test-desktop ## Run every test pass in the tag matrix
	@echo "$(GREEN)All tag passes completed successfully$(NC)"

.PHONY: test-fips
test-fips: ## Run tests in FIPS mode
	@echo "$(BLUE)Running tests in FIPS 140-3 mode...$(NC)"
	@echo "$(YELLOW)Using GOFIPS140=latest (Go 1.24+)$(NC)"
	GOFIPS140=latest $(GOTEST) -v ./pkg/fips/...

# FIPS_ONLY_ENV activates the Go Cryptographic Module and puts it in the mode
# that REFUSES every unapproved algorithm instead of merely preferring approved
# ones. A package listed under test-fips-conformance passes here only if every
# primitive it reaches is FIPS 140-3 approved.
FIPS_ONLY_ENV = GOFIPS140=latest GODEBUG=fips140=only

# FIPS_LOGIN_TESTS are the client-secret login paths (PBKDF2-HMAC-SHA256
# derivation and verification). The rest of ./pkg/iam is deliberately not in
# this pass: the TOTP surface carries protocol-mandated HMAC-SHA1, which has
# its own FIPS treatment and its own refusal.
FIPS_LOGIN_TESTS = ClientSecret|HashClientSecret|ValidateClient

# FIPS_DEK_TESTS are the data-encryption-key algorithm gate. ./pkg/service is
# far larger than this change and is filtered for the same reason.
FIPS_DEK_TESTS = DEK

.PHONY: test-fips-conformance
test-fips-conformance: ## Run the FIPS conformance pass (GODEBUG=fips140=only)
	@echo "$(BLUE)Running FIPS 140-3 conformance tests (fips140=only)...$(NC)"
	@echo "$(YELLOW)Unapproved algorithms are refused by the module, not merely deprioritized$(NC)"
	$(FIPS_ONLY_ENV) $(GOTEST) ./pkg/fips/... ./pkg/util/hash/... ./pkg/storage/encryption/... ./pkg/config/...
	@echo "$(YELLOW)Login (client secret KDF): $(FIPS_LOGIN_TESTS)$(NC)"
	$(FIPS_ONLY_ENV) $(GOTEST) -run '$(FIPS_LOGIN_TESTS)' ./pkg/iam/
	@echo "$(YELLOW)Envelope DEK algorithm gate: $(FIPS_DEK_TESTS)$(NC)"
	$(FIPS_ONLY_ENV) $(GOTEST) -run '$(FIPS_DEK_TESTS)' ./pkg/service/
	@echo "$(GREEN)FIPS conformance pass complete$(NC)"
	@echo "$(YELLOW)NOT covered: ./pkg/security (keyring at-rest encryption).$(NC)"
	@echo "$(YELLOW)  Its AEAD comes from go-xkms pkg/crypto/aead.NewGCM, which builds$(NC)"
	@echo "$(YELLOW)  cipher.NewGCM with a caller-supplied IV and is refused under$(NC)"
	@echo "$(YELLOW)  fips140=only. The fix is upstream; adding the package here would$(NC)"
	@echo "$(YELLOW)  report a go-xkms defect as a quicbaas one.$(NC)"

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
# The live forge suite. Named per the integration-test-$PACKAGE convention so it
# is discoverable rather than reachable only by remembering a `go test -tags`
# incantation.
#
# It is NOT part of the blanket `integration-test` aggregate, deliberately: it
# needs credentials for a live Gitea, and a gate that silently requires secrets
# fails for everyone who does not have them, which teaches people to ignore it.
# Wiring it into a gate is an infrastructure decision about where those
# credentials live, not a Makefile one.
#
# The convention's "mock external API calls to 3rd party endpoints" does not
# apply here: Gitea is our own internal service, and the entire purpose of this
# suite is the server semantics a mock cannot know. Three real bugs were found
# exactly this way -- snake_case merge fields, drafts having no API field, and
# webhook event expansion -- each of which a mock reproduced happily.
.PHONY: integration-test-forge-gitea
integration-test-forge-gitea: ## Run the live Gitea forge suite (needs GITEA_ITEST_* env)
	@echo "$(BLUE)Running live Gitea forge integration tests...$(NC)"
	@echo "$(YELLOW)Requires GITEA_ITEST_BASE_URL, _TOKEN, _OWNER, _REPO against a SCRATCH repo$(NC)"
	$(GOTEST) -tags integration -count=1 -v -timeout=10m -run 'TestLive_' ./pkg/sdlc/
	@echo "$(GREEN)✓ Live Gitea forge tests passed$(NC)"

# The merge-result snapshot suite. It measures what the tree a verification
# judges actually HOLDS, which needs real git against a real working tree:
# `git add --all` into an index that starts empty silently drops every file
# the repository tracks despite an ignore rule, and a double that records the
# argument vector sees an identical command either way (go-quicbaas#982). The
# command plumbing stays in the unit tier, where the fakes hold it.
#
# It is tagged `integration` because of what it TOUCHES, not what it costs: it
# spawns git and writes a working tree, which the unit tier forbids, and it
# finishes in under a second. That is also why it runs IN A CONTAINER and
# never on the invoking host: "never run on the host OS" is about what a suite
# touches, not about how long it takes (AGENTS.md, Integration & Functional
# Testing; ../handbook/software/conventions/integration-testing.md section 4),
# and a suite spawning git against a working tree is outside the host either
# way.
#
# THE CONTAINER IS DISPOSABLE AND HAS NOTHING TO REACH. Like the live forge
# suite this is not part of the blanket `integration-test` aggregate, which
# brings up a Docker stack this suite has no use for: there is no server, no
# credential and no network (`--network none`), everything the tests write is
# under the container's own temp dir, and `--rm` takes that whole tree away
# afterwards.
#
# CI RUNS THIS TARGET on every push and every pull request -- the
# `Merge-Result Snapshot` job in .github/workflows/ci.yml, which the CI Gate
# requires. Staying out of the `integration-test` aggregate is why it needs
# its own job: nothing else in the pipeline executes this suite, since
# `make test` does not compile the `integration` tag at all and
# `make vet-integration` only typechecks it. A suite nothing executes
# measures nothing (integration-testing.md section 8), which is how the only
# tests of what a snapshot CONTAINS would otherwise have gated nothing.
#
# THE SOURCE IS BAKED INTO THE IMAGE AND NOTHING IS BIND-MOUNTED, which is
# what lets CI run this exact target rather than a second copy of it in a
# workflow file (devops.md §4). A bind mount cannot work on a Gitea runner:
# act_runner talks to the HOST's docker daemon over its socket, so
# `docker run -v $PWD:/workspace` asks that daemon to resolve a path that
# exists only inside the job container, and it dies with `error while creating
# mount source path` -- runner-log-proven, and why release.yml's build step
# uses `docker cp` and test/integration/Dockerfile bakes the CLI in rather
# than mounting it. A build context is streamed to the daemon over the API
# instead, so it resolves wherever the two filesystems actually live. That
# also settles the module cache: it is warmed at image build, where the
# BuildKit gomodauth secret can reach the private first-party modules, so the
# run itself needs neither a credential nor a network.
#
# This is a `docker build` rather than a `docker exec` into the devcontainer
# for the same reason: the devcontainer's own `go mod download` is
# best-effort and credential-free, so the first-party modules are not in it,
# and reaching the checkout from it means the bind mount above. The suite
# needs git and nothing else at run time, so the image builds the test binary
# in a golang-alpine stage and ships it on a bare alpine with git -- the
# toolchain, the module cache and the source stay behind in the build stage.
# See test/integration/Dockerfile.merge-result.
.PHONY: integration-test-merge-result
integration-test-merge-result: ## Run the merge-result snapshot suite in a container (real git, no services)
	@echo "$(BLUE)Running merge-result snapshot integration tests in a container...$(NC)"
	@if ! command -v $(DOCKER) > /dev/null 2>&1; then \
		echo "$(RED)Error: Docker is not installed$(NC)"; \
		echo "$(YELLOW)Install Docker: https://docs.docker.com/get-docker/$(NC)"; \
		exit 1; \
	fi
	@$(MAKE) --no-print-directory stage-gomodauth
	@# THE IMAGE IS NEVER TAGGED. It is identified only by the id the build
	@# writes to --iidfile. A fixed tag is forbidden outright
	@# (integration-testing.md section 7, "no fixed image tags shared between
	@# runs"), for a reason this target hits head-on: two runs on one host are
	@# routine, and under a `:latest` the second run's build moves the tag
	@# between the first run's build and its `docker run` -- so that run tests
	@# another commit's binary and reports the result under this commit. A
	@# per-run tag would fix that, but it would also be an image identity this
	@# repository creates and cannot write literally, which
	@# scripts/internal/dockerinv rejects and `make verify-clean-docker` fails
	@# on -- rightly, since an image nobody can name cannot be cleaned up by
	@# name. An id names exactly one image forever, so the scan, the run and
	@# the removal below are provably the same image, and the EXIT trap takes
	@# it away on every path out of this recipe.
	@#
	@# The id is normalised through the daemon before it is used, so the rest
	@# of the recipe carries the canonical `sha256:...` form whatever shape the
	@# BuildKit release wrote, and a build that somehow produced no usable
	@# image fails here rather than inside Trivy.
	@#
	@# NOTHING IS RUN FROM THE IMAGE BEFORE IT IS SCANNED: devops.md section 8
	@# gates `trivy image` on every image this repository builds, CI images
	@# included, and `trivy-image-merge-result` is that gate for this one.
	@#
	@# `docker build` under BuildKit rather than `docker buildx build`: the
	@# secret mount needs BuildKit either way, and this form needs no buildx
	@# plugin and no builder instance. It also cannot hit the trap where a
	@# container-driver builder leaves the image in its own cache instead of
	@# the daemon, which would make the `docker run` below fail on an image
	@# that had just built successfully.
	@#
	@# One logical line, because the EXIT trap has to outlive the build: make
	@# runs every recipe line in a shell of its own.
	@set -eu; \
	IIDFILE=$$(mktemp "$${TMPDIR:-/tmp}/quicbaas-merge-result-iid.XXXXXX"); \
	IMAGE=""; \
	cleanup() { \
		ref="$$IMAGE"; \
		[ -n "$$ref" ] || ref=$$(cat "$$IIDFILE" 2>/dev/null || true); \
		[ -z "$$ref" ] || $(DOCKER) image rm -f "$$ref" >/dev/null 2>&1 || true; \
		rm -f "$$IIDFILE"; \
	}; \
	trap cleanup EXIT; \
	DOCKER_BUILDKIT=1 $(DOCKER) build \
		--secret id=gomodauth,src=$(GOMODAUTH_ENV) \
		-f test/integration/Dockerfile.merge-result \
		--iidfile "$$IIDFILE" .; \
	IID=$$(cat "$$IIDFILE"); \
	if [ -z "$$IID" ]; then \
		echo "$(RED)The build wrote no image id to $$IIDFILE$(NC)"; \
		exit 1; \
	fi; \
	IMAGE=$$($(DOCKER) image inspect --format '{{.Id}}' "$$IID"); \
	echo "$(BLUE)Built the merge-result test image $$IMAGE$(NC)"; \
	$(MAKE) --no-print-directory trivy-image-merge-result MERGE_RESULT_IMAGE_ID="$$IMAGE"; \
	$(DOCKER) run --rm --network none "$$IMAGE"
	@echo "$(GREEN)✓ Merge-result snapshot tests passed$(NC)"

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
	@$(MAKE) --no-print-directory stage-gomodauth
	@COMPOSE_FILE="test/integration/docker-compose.seal.yml"; \
	trap '$(GOMODAUTH_CLEAR)' EXIT; \
	echo "$(YELLOW)Cleaning up any previous seal-test containers...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans 2>/dev/null || true; \
	echo "$(YELLOW)Building seal-test image (quicbaasd + quicbaas built with -tags 'awskms vault')...$(NC)"; \
	docker compose -f $$COMPOSE_FILE build seal-test 2>&1 | while IFS= read -r line; do echo "  $$line"; done; \
	$(GOMODAUTH_CLEAR); \
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
	@$(MAKE) --no-print-directory stage-gomodauth
	@COMPOSE_FILE="test/integration/docker-compose.seal.yml"; \
	trap '$(GOMODAUTH_CLEAR)' EXIT; \
	echo "$(YELLOW)Issuing integration mTLS/PIV fixtures through the QuicBaaS CA...$(NC)"; \
	$(GOCMD) run ./test/integration/certfixture/cmd test/integration/certs; \
	echo "$(YELLOW)Cleaning up any previous tenantbarrier containers...$(NC)"; \
	docker compose -p tenantbarrier -f $$COMPOSE_FILE down --volumes --remove-orphans 2>/dev/null || true; \
	echo "$(YELLOW)Building tenantbarrier images (seal-enabled quicbaasd node + test runner)...$(NC)"; \
	docker compose -p tenantbarrier -f $$COMPOSE_FILE build tenantbarrier-node tenantbarrier-test 2>&1 | while IFS= read -r line; do echo "  $$line"; done; \
	$(GOMODAUTH_CLEAR); \
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

# Where integration-test backs go.mod up while the stack runs, restored after.
# Overridable so a gate that drives the recipe never touches a real run's copy.
GOMOD_BACKUP ?= /tmp/quicbaas-gomod-backup
SDK_GOMOD_BACKUP ?= /tmp/quicbaas-sdk-gomod-backup

# Where stage-gomodauth finds a Gitea read token on a host that is not a Gitea
# runner (#1031). A runner injects the dependency-resolution.md section-6
# variables itself; a developer or agent host resolves the private modules
# through a ~/.gitconfig rewrite a container cannot see, so the variables are
# built here from a read token instead. The token comes from GITEA_READ_TOKEN
# in the environment, else from the GOMODAUTH_TOKEN_KEY line of the agent
# tokens file. It is only ever read inside the recipe's shell -- never into a
# make variable, a command line, a build arg or the log.
#
# GITEA_HTTP_ORIGIN is the forge the token is good for (http:// or https://,
# host and port). It has no default: the tree names no forge. Set it in the
# environment, or put a GITEA_HTTP_ORIGIN= line in the tokens file beside the
# token, which is where a private deployment keeps it. A token with no origin
# is refused rather than staged against a guess.
GOMODAUTH_TOKENS_FILE ?= $(HOME)/.agent-identity/tokens.env
GOMODAUTH_TOKEN_KEY ?= AGENT_TOKEN_GO_QUICBAAS
GITEA_HTTP_ORIGIN ?=
GOMODAUTH_OWNER ?= jeremyhahn

# Empties the staged file. Every target that stages it runs this once its
# images are built, and arms it in an EXIT trap so a failed or cancelled build
# clears it too. The file is emptied rather than removed: a later compose
# invocation against the same project still resolves the secret's path.
# umask applies only to a file this creates; stage-gomodauth chmods an existing
# one to 0600 before it writes a token into it.
GOMODAUTH_CLEAR = { umask 077; : > "$(GOMODAUTH_ENV)"; } 2>/dev/null || true

.PHONY: stage-gomodauth
stage-gomodauth: ## Stage Gitea module-fetch credentials for the image builds
	@umask 077; : > "$(GOMODAUTH_ENV)" && chmod 600 "$(GOMODAUTH_ENV)"
	@if [ -n "$${GIT_CONFIG_KEY_0:-}" ]; then \
		umask 077; \
		{ \
			printf 'export GOPRIVATE=%s\n' "$${GOPRIVATE:-}"; \
			printf 'export GIT_CONFIG_COUNT=%s\n' "$${GIT_CONFIG_COUNT:-}"; \
			printf 'export GIT_CONFIG_KEY_0=%s\n' "$${GIT_CONFIG_KEY_0}"; \
			printf 'export GIT_CONFIG_VALUE_0=%s\n' "$${GIT_CONFIG_VALUE_0:-}"; \
		} > "$(GOMODAUTH_ENV)"; \
		echo "$(GREEN)Staged Gitea module credentials for the image build (runner environment)$(NC)"; \
		exit 0; \
	fi; \
	token="$${GITEA_READ_TOKEN:-}"; source=GITEA_READ_TOKEN; \
	if [ -z "$$token" ] && [ -r "$(GOMODAUTH_TOKENS_FILE)" ]; then \
		token=$$(sed -n 's/^[[:space:]]*\(export[[:space:]]\{1,\}\)\{0,1\}$(GOMODAUTH_TOKEN_KEY)=//p' "$(GOMODAUTH_TOKENS_FILE)" | tail -n 1 | tr -d "\"' \r"); \
		source="$(GOMODAUTH_TOKEN_KEY) in $(GOMODAUTH_TOKENS_FILE)"; \
	fi; \
	if [ -z "$$token" ]; then \
		echo "$(YELLOW)No Gitea module credentials: no GIT_CONFIG_KEY_0 (Gitea runner), no GITEA_READ_TOKEN,$(NC)"; \
		echo "$(YELLOW)  and no $(GOMODAUTH_TOKEN_KEY) in $(GOMODAUTH_TOKENS_FILE).$(NC)"; \
		echo "$(YELLOW)  Image builds resolve modules from the public proxy, where private first-party$(NC)"; \
		echo "$(YELLOW)  modules not yet mirrored fail with 'unknown revision' (docs/testing/private-modules.md)$(NC)"; \
		exit 0; \
	fi; \
	case "$$token" in \
		*[!A-Za-z0-9_-]*) \
			echo "$(RED)Error: the Gitea read token from $$source is not token-shaped (expected [A-Za-z0-9_-]); nothing staged$(NC)"; \
			exit 1 ;; \
	esac; \
	origin="$(GITEA_HTTP_ORIGIN)"; \
	if [ -z "$$origin" ] && [ -r "$(GOMODAUTH_TOKENS_FILE)" ]; then \
		origin=$$(sed -n 's/^[[:space:]]*\(export[[:space:]]\{1,\}\)\{0,1\}GITEA_HTTP_ORIGIN=//p' "$(GOMODAUTH_TOKENS_FILE)" | tail -n 1 | tr -d "\"' \r"); \
	fi; \
	if [ -z "$$origin" ]; then \
		echo "$(RED)Error: a Gitea read token was found ($$source) but no Gitea origin is set; nothing staged$(NC)"; \
		echo "$(RED)  Set GITEA_HTTP_ORIGIN (e.g. https://gitea.example.com:3000) in the environment or in $(GOMODAUTH_TOKENS_FILE)$(NC)"; \
		exit 1; \
	fi; \
	origin="$${origin%/}"; \
	case "$$origin" in \
		http://*|https://*) ;; \
		*) echo "$(RED)Error: GITEA_HTTP_ORIGIN must be an http:// or https:// origin; nothing staged$(NC)"; exit 1 ;; \
	esac; \
	scheme="$${origin%%://*}"; hostport="$${origin#*://}"; \
	umask 077; \
	{ \
		printf 'export GOPRIVATE=%s\n' "$${GOPRIVATE:-github.com/$(GOMODAUTH_OWNER)/*}"; \
		printf 'export GIT_CONFIG_COUNT=1\n'; \
		printf 'export GIT_CONFIG_KEY_0=url.%s://x:%s@%s/%s/.insteadOf\n' "$$scheme" "$$token" "$$hostport" "$(GOMODAUTH_OWNER)"; \
		printf 'export GIT_CONFIG_VALUE_0=https://github.com/%s/\n' "$(GOMODAUTH_OWNER)"; \
	} > "$(GOMODAUTH_ENV)"; \
	echo "$(GREEN)Staged Gitea module credentials for the image build (token from $$source, origin $$origin)$(NC)"

.PHONY: clear-gomodauth
clear-gomodauth: ## Empty the staged Gitea module-fetch credentials file
	@$(GOMODAUTH_CLEAR)

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
	trap '$(GOMODAUTH_CLEAR); if [ $$TORN_DOWN -eq 0 ]; then echo "$(YELLOW)Tearing down the integration stack...$(NC)"; docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans >/dev/null 2>&1 || true; fi' EXIT; \
	cp go.mod "$(GOMOD_BACKUP)"; \
	cp sdk/go/go.mod "$(SDK_GOMOD_BACKUP)"; \
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
	$(GOMODAUTH_CLEAR); \
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
	if [ $$DOCKER_RESULT -ne 0 ]; then \
		echo "$(YELLOW)Integration tests failed — capturing service logs before teardown$(NC)"; \
		docker compose -f $$COMPOSE_FILE logs --no-color --timestamps > integration-compose.log 2>&1 || true; \
		echo "$(YELLOW)---- service logs (tail 200; full copy in integration-compose.log) ----$(NC)"; \
		tail -n 200 integration-compose.log || true; \
		echo "$(YELLOW)---- end service logs ----$(NC)"; \
	fi; \
	echo "$(BLUE)Stopping integration test cluster...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans; \
	TORN_DOWN=1; \
	echo "$(YELLOW)Restoring go.mod files (if modified by container)...$(NC)"; \
	if [ -f "$(GOMOD_BACKUP)" ]; then cp "$(GOMOD_BACKUP)" go.mod; fi; \
	if [ -f "$(SDK_GOMOD_BACKUP)" ]; then cp "$(SDK_GOMOD_BACKUP)" sdk/go/go.mod; fi; \
	rm -f "$(GOMOD_BACKUP)" "$(SDK_GOMOD_BACKUP)"; \
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
		--embedded-fleet-node --embedded-fleet-node-backend ollama \
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
		--embedded-fleet-node --embedded-fleet-node-backend claude \
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

# E2E_SPEC optionally narrows test-e2e-live to one spec, e.g.
#   make test-e2e-live E2E_SPEC=test/e2e/projects.spec.ts
E2E_SPEC ?=

.PHONY: test-e2e-live
test-e2e-live: ## Run Playwright E2E against an ALREADY-RUNNING node as an existing tenant user
	@echo "$(BLUE)Running Playwright E2E against a live node...$(NC)"
	@missing=""; \
	for v in QUICBAAS_UI_URL QUICBAAS_E2E_TENANT QUICBAAS_E2E_USER_EMAIL \
	         QUICBAAS_E2E_ADMIN_CERT QUICBAAS_E2E_ADMIN_KEY QUICBAAS_E2E_CA; do \
		if [ -z "$$(printenv $$v)" ]; then missing="$$missing $$v"; fi; \
	done; \
	if [ -n "$$missing" ]; then \
		echo "$(RED)Error: test-e2e-live needs these in the environment:$$missing$(NC)"; \
		echo "  QUICBAAS_UI_URL         https://<host>:<port>/ui/ of the running node"; \
		echo "  QUICBAAS_E2E_TENANT     tenant the user belongs to"; \
		echo "  QUICBAAS_E2E_USER_EMAIL the existing user to sign in as"; \
		echo "  QUICBAAS_E2E_ADMIN_CERT admin client cert (enrols the user's passkey)"; \
		echo "  QUICBAAS_E2E_ADMIN_KEY  its private key"; \
		echo "  QUICBAAS_E2E_CA         CA the node's serving certificate chains to"; \
		exit 1; \
	fi; \
	base=$$(echo "$$QUICBAAS_UI_URL" | sed -E 's#/ui/?$$##'); \
	code=$$(curl -s --cacert "$$QUICBAAS_E2E_CA" -o /dev/null -w '%{http_code}' \
		"$$base/api/v1/health/ready" 2>/dev/null || true); \
	if [ "$$code" != "200" ]; then \
		echo "$(RED)Error: $$base/api/v1/health/ready answered '$$code' with CA $$QUICBAAS_E2E_CA.$(NC)"; \
		echo "$(RED)This target does not start a node — bring one up first.$(NC)"; \
		exit 1; \
	fi; \
	echo "$(GREEN)live node ready at $$base$(NC)"; \
	cd cmd/quicbaas/frontend && \
		NODE_EXTRA_CA_CERTS="$$QUICBAAS_E2E_CA" \
		npx playwright test --project=chromium-no-backend $(E2E_SPEC)

.PHONY: test-e2e-docker
test-e2e-docker: ## Run the Playwright e2e suite fully in Docker (host needs only Docker)
	@echo "$(BLUE)Running dockerized Playwright e2e (chromium-no-backend)...$(NC)"
	@$(MAKE) --no-print-directory stage-gomodauth
	@COMPOSE_FILE="test/e2e/docker-compose.e2e.yml"; \
	trap '$(GOMODAUTH_CLEAR)' EXIT; \
	set -o pipefail; \
	if ! docker compose -f $$COMPOSE_FILE build e2e 2>&1 | while IFS= read -r line; do echo "  $$line"; done; then \
		echo "$(RED)Image build FAILED — refusing to run e2e against a stale cached image.$(NC)"; \
		exit 1; \
	fi; \
	$(GOMODAUTH_CLEAR); \
	docker compose -f $$COMPOSE_FILE run --rm e2e; \
	RESULT=$$?; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans >/dev/null 2>&1 || true; \
	exit $$RESULT

.PHONY: test-e2e-acp-docker
test-e2e-acp-docker: ## Run the Playwright ACP live suite in Docker (opencode + ollama on a GPU host). Requires GPU_HOST=<host>.
	@if [ -z "$(strip $(GPU_HOST))" ]; then \
		echo "$(RED)Error: GPU_HOST is required (e.g. GPU_HOST=198.51.100.10). The ACP suite runs opencode against a remote ollama.$(NC)"; \
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
	$(MAKE) --no-print-directory stage-gomodauth || exit 1; \
	trap '$(GOMODAUTH_CLEAR)' EXIT; \
	E2E_PROJECT=chromium-acp E2E_BACKEND=opencode E2E_OLLAMA_URL="http://$$GPU_HOST" \
	docker compose -f $$COMPOSE_FILE -f test/e2e/docker-compose.e2e-acp.yml build e2e 2>&1 | while IFS= read -r line; do echo "  $$line"; done; \
	$(GOMODAUTH_CLEAR); \
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

# fleet_runtime gates only test/integration/fleet_runtime, which runs from its
# own compose stack (integration-test-fleet-runtime) and so is in no other tag
# set; without it here nothing typechecked the suite and it rotted unseen
# (go-quicbaas#733).
INTEGRATION_VET_TAGS := integration fleet_runtime $(strip $(INTEGRATION_BUILD_TAGS)) $(TEST_EXTRA_TAGS)

# The scan that finds every package carrying an integration-tagged file. It is
# a text scan of the tracked files (milliseconds, no build, no network), the
# same instrument verify-naming uses, and it is DERIVED rather than listed
# because a listed scope is exactly what went stale: this target vetted
# `./test/...` alone while the daemon e2e suite in cmd/quicbaasd sat behind the
# same tag, vetted by nothing in Lint or anywhere else (go-quicbaas#686).
#
# `./...` under the integration tags is NOT the answer. It typechecks the
# unit-only packages in a configuration they never ship in -- every
# `//go:build !integration` helper has vanished -- and reports phantom errors
# (undefined buildMemoryStandaloneNode in pkg/server, undefined mockKVStoreForKMS
# in pkg/service), the same failure mode the tag-gated packages have under a
# bare `go vet ./...`.
INTEGRATION_VET_SCAN = git grep -lE '^//go:build integration([ &]|$$)' -- '*.go'

# The hardware tags gate code that needs a device to RUN but nothing to
# compile: pkcs11 (the HSM backend, seal strategy and local PIV session), tpm2
# (the swtpm seal suite) and gpu (the GPU control-loop suite). No other tag set
# names them, so no gate compiled that code and it rotted unseen through a
# go-xkms SecretValue migration and a control-loop API change. They are vetted
# together over the packages that carry a file enabled by one of them --
# derived by the same text scan as above, so a new hardware-gated package is
# covered without editing this list -- in the configuration each package ships
# in: the unit tag set outside ./test, the integration tag set inside it. The
# scan matches a POSITIVE use of a tag only; a `!gpu` stub is the default
# configuration and is already vetted by every other pass.
HARDWARE_VET_TAGS := pkcs11 gpu tpm2
HARDWARE_VET_SCAN = git grep -lE '^//go:build(.*[ (&|])?(pkcs11|gpu|tpm2)([ )&|]|$$)' -- '*.go'

# Every integration-tagged package is vetted; there is no exclusion list. The
# last excluded packages (pkg/backup, pkg/server, pkg/storage/file; pkg/sdlc
# before them, go-quicbaas#982) left it with their fixes (go-quicbaas#690), so a
# change that breaks one of them now fails this gate instead of rotting unseen.

.PHONY: vet-integration
vet-integration: ## Typecheck every integration- and hardware-tagged package (the tags no unit gate compiles)
	@echo "$(BLUE)Running go vet (integration tree)...$(NC)"
	$(GOCMD) vet -tags '$(INTEGRATION_VET_TAGS)' ./test/...
	@echo "$(BLUE)Running go vet (integration-tagged packages outside ./test)...$(NC)"
	@pkgs=$$($(INTEGRATION_VET_SCAN) | xargs -n1 dirname | sort -u | grep -v '^test/' | grep -v '^sdk/'); \
	test -n "$$pkgs" || { echo "$(RED)vet-integration: the build-tag scan matched no package -- it has rotted$(NC)"; exit 1; }; \
	scope=$$(printf '%s\n' "$$pkgs" | sed 's|^|./|; s|$$|/...|' | tr '\n' ' '); \
	echo "  $$scope"; \
	$(GOCMD) vet -tags '$(INTEGRATION_VET_TAGS)' $$scope
	@echo "$(BLUE)Running go vet (hardware tags: $(HARDWARE_VET_TAGS))...$(NC)"
	@pkgs=$$($(HARDWARE_VET_SCAN) | xargs -n1 dirname | sort -u); \
	unit=$$(printf '%s\n' "$$pkgs" | grep -v '^test/' | sed 's|^|./|' | tr '\n' ' '); \
	integ=$$(printf '%s\n' "$$pkgs" | grep '^test/' | sed 's|^|./|' | tr '\n' ' '); \
	test -n "$$unit" -a -n "$$integ" || { echo "$(RED)vet-integration: the hardware-tag scan lost a tier (unit: '$$unit', integration: '$$integ') -- it has rotted$(NC)"; exit 1; }; \
	echo "  unit: $$unit"; \
	$(GOCMD) vet -tags '$(TEST_TAGS) $(HARDWARE_VET_TAGS)' $$unit || exit 1; \
	echo "  integration: $$integ"; \
	$(GOCMD) vet -tags '$(INTEGRATION_VET_TAGS) $(HARDWARE_VET_TAGS)' $$integ
	@echo "$(BLUE)Running go vet (Go SDK integration tree)...$(NC)"
	@# sdk/go is its own module, so its integration tree needs its own pass:
	@# the main module's `./...` cannot name it, and `make test-sdk-go` runs
	@# `go list ./... | grep -v /test/`, which drops it too.
	cd sdk/go && $(GOCMD) vet -tags integration ./test/...

.PHONY: lint
# vet-integration first: `make test` never compiles test/integration (it runs
# ./pkg/... ./cmd/... ./scripts/... without the tag), so a call site there that
# an API change broke surfaced only in CI's `make vet` (go-quicbaas#572, the
# stage-4 validator registry). A lane that runs `make lint` now typechecks it.
#
# verify-naming after it: the gate is a text scan of the tracked files
# (milliseconds, no build, no network), so a naming regression reports before
# the 20-minute golangci-lint pass rather than after it. This is why CI's Lint
# job needs no separate step for the gate.
lint: vet-integration verify-naming ## Run linters (naming gate + golangci-lint)
	@echo "$(BLUE)Running linters...$(NC)"
	@test -x $(GOLINT) || (echo "$(RED)golangci-lint not installed. Run: make install-tools$(NC)" && exit 1)
	# 20m, not the template's 5m. This module is ~526k lines across ~1300 files
	# and every linter runs over the full tag set; on a contended runner the
	# pass took 16 minutes on 2026-08-14 and was killed by the deadline after
	# it had already printed "0 issues" -- a green analysis reported as a red
	# job. The budget exists to catch a hang, so it needs to sit above the
	# honest worst case rather than near the median.
	#
	# The exit code is triaged the way `vuln` and `gosec` already triage theirs.
	# A shell renders a signal death as 128+N, so an OOM kill leaves 137 -- and
	# without this, that is indistinguishable from golangci-lint reporting real
	# findings. Both are just "make: *** [lint] Error", which is how three kills
	# on 2026-08-26 (#154) read as lint failures on code that lints clean.
	@$(GOLINT) run --timeout=20m --build-tags '$(LINT_BUILD_TAGS)' ./...; rc=$$?; \
	if [ $$rc -eq 0 ]; then \
		exit 0; \
	elif [ $$rc -gt 128 ]; then \
		echo "$(RED)golangci-lint was KILLED by signal $$(($$rc - 128)) (exit $$rc) -- it did NOT report findings.$(NC)"; \
		echo "$(RED)This is the job container's memory cap, not a lint result: the linters hold the$(NC)"; \
		echo "$(RED)whole package graph and its type information live across ~1300 files, and the$(NC)"; \
		echo "$(RED)kill lands before any report is written. Re-run when the host is quieter, or$(NC)"; \
		echo "$(RED)raise the cap -- do not read this as a finding, and do not go looking through$(NC)"; \
		echo "$(RED)the diff for the cause.$(NC)"; \
		exit $$rc; \
	else \
		echo "$(RED)golangci-lint reported findings (exit $$rc)$(NC)"; \
		exit $$rc; \
	fi

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

# THE MERGE-RESULT TEST IMAGE'S SCAN. devops.md section 8 gates `trivy image`
# on EVERY image this repository builds -- "CI, release, builder", not only the
# ones that ship -- so the image `integration-test-merge-result` builds is
# scanned before anything is run from it.
#
# It is not in TRIVY_IMAGES and cannot be. That list names two images that
# persist under a fixed tag, and `trivy-image` builds them itself; this one
# carries no tag at all, exists only for the length of the target that builds
# it, and is removed by that target's EXIT trap. So the image arrives here as
# the id that build wrote, which is also what makes the scan and the run
# provably the same image. Trivy resolves a bare image id through its own
# <image_id> fallback, so the scan needs no name either.
#
# --ignore-unfixed on the OS layer is the 2026-08-14 ratification in section 8:
# an alpine base carries advisories with no fix to take, and a gate that is
# permanently red trains people to ignore it. A CRITICAL or HIGH that HAS a fix
# still fails this target, and with it `integration-test-merge-result` and the
# CI job that runs it.
#
# Trivy runs from its own image over the daemon socket when no CLI is
# installed, for the reason ci.yml's docker job records: trivy-action installs
# itself through a nested checkout that a self-hosted runner resolves against
# its own server. That socket bind is NOT the act_runner mount trap
# `integration-test-merge-result` describes -- /var/run/docker.sock is the same
# path on the host and inside the job container, which is why the docker job's
# own Trivy gate already works this way.
#
# IT FAILS WHEN NO SCANNER CAN BE REACHED. `trivy-image` above warns and
# returns 0, which is survivable for a target a human runs on purpose; this one
# is a mandatory gate in a CI job's path, and a gate that skips itself when its
# tool is missing reports green over an image nothing read.
MERGE_RESULT_IMAGE_ID ?=

.PHONY: trivy-image-merge-result
trivy-image-merge-result: ## Scan the merge-result test image (MERGE_RESULT_IMAGE_ID=<image id>, set by integration-test-merge-result)
	@if [ -z "$(MERGE_RESULT_IMAGE_ID)" ]; then \
		echo "$(RED)MERGE_RESULT_IMAGE_ID is required$(NC)"; \
		echo "$(YELLOW)Run 'make integration-test-merge-result', which builds the image and passes its id here$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Scanning the merge-result test image with Trivy...$(NC)"
	@if command -v trivy >/dev/null 2>&1; then \
		trivy image --severity CRITICAL,HIGH --ignore-unfixed --exit-code 1 \
			--no-progress --format table "$(MERGE_RESULT_IMAGE_ID)"; \
	elif command -v $(DOCKER) >/dev/null 2>&1; then \
		$(DOCKER) run --rm \
			-v /var/run/docker.sock:/var/run/docker.sock \
			aquasec/trivy:latest image \
				--severity CRITICAL,HIGH \
				--ignore-unfixed \
				--exit-code 1 \
				--no-progress \
				--format table \
				"$(MERGE_RESULT_IMAGE_ID)"; \
	else \
		echo "$(RED)No scanner available: install Trivy with 'make install-trivy', or install Docker$(NC)"; \
		exit 1; \
	fi
	@echo "$(GREEN)✓ Merge-result test image scan clean (no fixable CRITICAL/HIGH)$(NC)"

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
	# 30m, not 10m. -timeout is PER PACKAGE, and pkg/iam alone measured 570.9s
	# cache-cold on an idle host on 2026-09-08 -- under 30 seconds of headroom
	# in a 10m budget, and 655s on a loaded one, where the deadline panicked
	# the package and took the whole target red. A budget exists to catch a
	# hang, so it belongs well above the honest worst case rather than beside
	# it. Do not buy headroom by trimming the package set: every package races.
	CGO_ENABLED=1 $(GOCMD) test -race -p 2 $(BUILD_TAGS_ARG) -timeout=30m ./pkg/... ./cmd/... || \
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

# ==============================================================================
# The Software Factory worker image
# ==============================================================================
#
# The image a project binding's `worker.image` names: where an sdlc job's one
# worker session runs, and where the runner's checkout, verification and
# publish commands are exec'd. See deployments/agents/software-factory-worker/
# Dockerfile for what is in it and why, and docs/software-factory/
# worker-image.md for how it is kept current.
#
# IT IS BUILT FOR THE UID THAT WILL RUN IT. The ACP spawn runs a sandboxed
# session as the daemon's own uid:gid, and musl resolves users from
# /etc/passwd alone — an image with no entry for that uid cannot sign a commit,
# because ssh-keygen refuses to start. So the build passes the building user's
# ids; an operator building for a daemon that runs as someone else overrides
# WORKER_IMAGE_UID / WORKER_IMAGE_GID.
#
# IT IS BUILT WITH THIS REPOSITORY'S TOOL PINS (pkg/version/toolchain.mk), so
# the worker reaches the same lint and scan verdicts as CI (#772) -- including
# actionlint and shellcheck, where a project's `make lint` lints workflows
# (#1039).
#
# THE AGENT CLIS ARE PINNED TOO, and these two pins are the ones that MOVE. A
# manual build installs exactly CLAUDE_CODE_VERSION and CODEX_VERSION; the
# daemon's worker image refresh (pkg/sdlc/workerimage) rebuilds the same
# Dockerfile with whatever newer versions the npm registry publishes, gates
# the result and rebinds projects to it. Override them on the command line to
# build a newer image by hand:
#
#   make build-worker-image CLAUDE_CODE_VERSION=2.1.281 CODEX_VERSION=0.157.0
#
# THE TAG NAMES THE CONTENTS: <version>-cc<claude-code>-cx<codex>, the same
# content tag the refresh uses (softwarefactoryworker.Build.Tag), so a manual
# build and a refresh of the same inputs are the same image name, and a newer
# CLI never overwrites the tag a binding may be rolled back to. The plain
# :$(DOCKER_TAG) tag is kept alongside as "the last image built by hand".
WORKER_IMAGE_DIR := deployments/agents/software-factory-worker
WORKER_IMAGE_REPO := $(DOCKER_IMAGE)-software-factory-worker
WORKER_IMAGE_UID ?= $(shell id -u)
WORKER_IMAGE_GID ?= $(shell id -g)
CLAUDE_CODE_VERSION ?= 2.1.280
CODEX_VERSION ?= 0.156.0
WORKER_IMAGE_TAG := $(DOCKER_TAG)-cc$(CLAUDE_CODE_VERSION)-cx$(CODEX_VERSION)
# WORKER_IMAGE_REGISTRY is where `make push-worker-image` publishes, as
# host[:port]/owner/name. It has no default: the registry is the operator's.
WORKER_IMAGE_REGISTRY ?=

.PHONY: build-worker-image
build-worker-image: ## Build the Software Factory worker image (quicbaas-software-factory-worker:<version>-cc<claude>-cx<codex>)
	@echo "$(BLUE)Building the Software Factory worker image ($(WORKER_IMAGE_REPO):$(WORKER_IMAGE_TAG))...$(NC)"
	$(DOCKER) build \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg GOLANGCI_VERSION=$(GOLANGCI_VERSION) \
		--build-arg GOSEC_VERSION=$(GOSEC_VERSION) \
		--build-arg GOVULNCHECK_VERSION=$(GOVULNCHECK_VERSION) \
		--build-arg ACTIONLINT_VERSION=$(ACTIONLINT_VERSION) \
		--build-arg SHELLCHECK_VERSION=$(SHELLCHECK_VERSION) \
		--build-arg SHELLCHECK_SHA256_X86_64=$(SHELLCHECK_SHA256_X86_64) \
		--build-arg SHELLCHECK_SHA256_AARCH64=$(SHELLCHECK_SHA256_AARCH64) \
		--build-arg GOPLS_VERSION=$(GOPLS_VERSION) \
		--build-arg GO_TOOL_DEPENDENCY_FLOORS="$(GO_TOOL_DEPENDENCY_FLOORS)" \
		--build-arg SERENA_VERSION=$(SERENA_VERSION) \
		--build-arg OPENCODE_VERSION=$(OPENCODE_VERSION) \
		--build-arg OPENCODE_SHA256_X86_64=$(OPENCODE_SHA256_X86_64) \
		--build-arg OPENCODE_SHA256_AARCH64=$(OPENCODE_SHA256_AARCH64) \
		--build-arg CLAUDE_CODE_VERSION=$(CLAUDE_CODE_VERSION) \
		--build-arg CODEX_VERSION=$(CODEX_VERSION) \
		--build-arg QUICBAAS_VERSION=$(VERSION) \
		--build-arg AGENT_UID=$(WORKER_IMAGE_UID) \
		--build-arg AGENT_GID=$(WORKER_IMAGE_GID) \
		-f $(WORKER_IMAGE_DIR)/Dockerfile \
		-t $(WORKER_IMAGE_REPO):$(WORKER_IMAGE_TAG) \
		-t $(WORKER_IMAGE_REPO):$(DOCKER_TAG) \
		$(WORKER_IMAGE_DIR)
	@echo "$(GREEN)✓ $(WORKER_IMAGE_REPO):$(WORKER_IMAGE_TAG)$(NC)"

# THE PROBES ARE GO, AND THEY ARE THE REFRESH GATE'S PROBES. Each one — the
# toolchain on the login PATH and a signed commit, serena's MCP handshake and a
# Go symbol lookup with no network, a real lint, full control of the container,
# and a bind-mounted write owned by the spawning uid — is defined once, with
# the reason it exists, in deployments/agents/software-factory-worker/
# probes.go. This target and the daemon's worker image refresh both run that
# one list, so an image certified here and an image the factory promotes by
# itself pass the same bar.
.PHONY: test-worker-image
test-worker-image: build-worker-image ## Build the worker image and run its probes (the refresh gate's probes)
	@echo "$(BLUE)Probing $(WORKER_IMAGE_REPO):$(WORKER_IMAGE_TAG) as uid $(WORKER_IMAGE_UID):$(WORKER_IMAGE_GID)...$(NC)"
	$(GOCMD) run ./$(WORKER_IMAGE_DIR)/probe \
		-image $(WORKER_IMAGE_REPO):$(WORKER_IMAGE_TAG) \
		-base $(VERSION) \
		-claude-code $(CLAUDE_CODE_VERSION) \
		-codex $(CODEX_VERSION) \
		-uid $(WORKER_IMAGE_UID) \
		-gid $(WORKER_IMAGE_GID)
	@echo "$(GREEN)✓ $(WORKER_IMAGE_REPO):$(WORKER_IMAGE_TAG) passed every probe$(NC)"

# Publish a worker image built by hand to the fleet's registry, so every node
# can pull it rather than having it copied to it. Log in first with the
# engine's own client, or point DOCKER at a private configuration:
#
#   make push-worker-image WORKER_IMAGE_REGISTRY=<host>[:port]/<owner>/<name> \
#       DOCKER="docker --config $$cfg"
#
# For a plain-HTTP registry the engine's insecure-registries entry is what
# permits it, and nothing here weakens TLS. WORKER_IMAGE_REGISTRY has no
# default: the registry is the deployment's to name, never this repository's.
#
# THE SCAN GATES THE PUSH. devops.md section 8 requires `trivy image` on every
# image this repository builds, blocking, before it is published; a fixable
# CRITICAL or HIGH fails the target and nothing is tagged or pushed. The
# --ignore-unfixed policy is the section 8 ratification trivy-image-merge-result
# records. TRIVY is resolved from PATH; override it to name another binary.
#
# THE DIGEST COMES FROM THE PUSH ITSELF ("<tag>: digest: sha256:... size: N"),
# not from RepoDigests, which some engines leave empty for an image built
# locally. The last line is the one callers read, and the only one they need:
#
#   WORKER_IMAGE_REF=<registry>:<tag>@sha256:<digest>
#
# The daemon's worker image refresh publishes the images it promotes itself
# (sdlc.worker_image.registry); this target is for the image a person built.
TRIVY ?= trivy
WORKER_IMAGE_LOCAL_REF = $(WORKER_IMAGE_REPO):$(WORKER_IMAGE_TAG)
WORKER_IMAGE_REMOTE_REF = $(WORKER_IMAGE_REGISTRY):$(WORKER_IMAGE_TAG)

.PHONY: push-worker-image
push-worker-image: ## Scan, then push the worker image to WORKER_IMAGE_REGISTRY (host[:port]/owner/name); prints WORKER_IMAGE_REF=<ref>@<digest>
	@if [ -z "$(WORKER_IMAGE_REGISTRY)" ]; then \
		echo "$(RED)WORKER_IMAGE_REGISTRY is required, e.g. make push-worker-image WORKER_IMAGE_REGISTRY=registry.example.com/owner/software-factory-worker$(NC)" >&2; \
		exit 1; \
	fi
	@if ! command -v $(firstword $(TRIVY)) >/dev/null 2>&1; then \
		echo "$(RED)trivy not found ($(TRIVY)): the scan gates the push; install it with 'make install-trivy' or set TRIVY=<path>$(NC)" >&2; \
		exit 1; \
	fi
	@echo "$(BLUE)Scanning $(WORKER_IMAGE_LOCAL_REF) before it is published...$(NC)"
	$(TRIVY) image --severity CRITICAL,HIGH --ignore-unfixed --exit-code 1 $(WORKER_IMAGE_LOCAL_REF)
	$(DOCKER) tag $(WORKER_IMAGE_LOCAL_REF) $(WORKER_IMAGE_REMOTE_REF)
	@set -e; \
	out=$$($(DOCKER) push $(WORKER_IMAGE_REMOTE_REF)); \
	printf '%s\n' "$$out"; \
	digest=$$(printf '%s\n' "$$out" | sed -n 's/^.*digest: \(sha256:[0-9a-f]\{64\}\).*$$/\1/p' | tail -n 1); \
	if [ -z "$$digest" ]; then \
		echo "$(RED)the push reported no manifest digest; nothing to pin to$(NC)" >&2; \
		exit 1; \
	fi; \
	echo "WORKER_IMAGE_REF=$(WORKER_IMAGE_REMOTE_REF)@$$digest"

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

# gosec is BUILT, not `go install`ed, and always: its newest release requires a
# vulnerable google.golang.org/grpc, so it is built from a throwaway module that
# raises the GO_TOOL_DEPENDENCY_FLOORS it depends on (toolchain.mk says which
# CVEs they close) -- the same build the worker image ships and CI scans with,
# so all three run one gosec. It is rebuilt even when a gosec is on the PATH,
# because an installed gosec of another version or build is exactly the drift
# the pin exists to stop. main.Version is stamped because a bare build reports
# "Version: dev" (go-luks2#21).
.PHONY: install-gosec
install-gosec: ## Build and install gosec at GOSEC_VERSION with GO_TOOL_DEPENDENCY_FLOORS
	@echo "$(BLUE)Building gosec $(GOSEC_VERSION) with $(GO_TOOL_DEPENDENCY_FLOORS)...$(NC)"
	@set -e; \
	gobin="$$(go env GOBIN)"; gobin="$${gobin:-$$(go env GOPATH)/bin}"; \
	dir="$$(mktemp -d)"; trap 'rm -rf "$$dir"' EXIT; \
	cd "$$dir"; \
	export GOWORK=off; \
	go mod init build.local/gosec >/dev/null 2>&1; \
	go get github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION); \
	for floor in $(GO_TOOL_DEPENDENCY_FLOORS); do \
		if go list -m "$${floor%@*}" >/dev/null 2>&1; then go get "$$floor"; fi; \
	done; \
	CGO_ENABLED=0 go build -ldflags "-X main.Version=$(GOSEC_VERSION)" -o "$$gobin/gosec" github.com/securego/gosec/v2/cmd/gosec; \
	"$$gobin/gosec" -version
	@echo "$(GREEN)gosec installed$(NC)"

.PHONY: install-govulncheck
install-govulncheck: ## Install govulncheck vulnerability scanner (pinned -- see GOVULNCHECK_VERSION)
	@echo "$(BLUE)Installing govulncheck $(GOVULNCHECK_VERSION)...$(NC)"
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION); \
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
	@# PINNED, devops.md §9: v2.13.x over-reports and panics under Go 1.26,
	@# so @latest turns a green branch red for reasons unrelated to the
	@# branch — and ~/go/bin is SHARED across lanes, so one lane's @latest
	@# install drifts every other lane's local lint. Matches ci.yml's pin;
	@# unpin tracker: go-qrdb#42.
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
# scripts/internal/dockerinv re-derives all of that from those commands and
# files -- `make verify-clean-docker` links it, and `go run ./scripts/dockerinventory`
# prints it -- so `make verify-clean-docker` fails when this list has drifted from
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
#
# The merge-result snapshot test image is absent for a related reason: it has
# no name at all. `integration-test-merge-result` builds it with --iidfile and
# no `-t`, because integration-testing.md section 7 forbids a fixed tag shared
# between runs and a per-run tag would be an identity this Makefile cannot
# write literally. It is removed by that target's own EXIT trap, by the id it
# just built, on every path out of that recipe.
DOCKER_LOCAL_IMAGES := \
	$(DOCKER_IMAGE):$(DOCKER_TAG) \
	$(DOCKER_IMAGE):latest \
	$(DOCKER_IMAGE):dev \
	$(DOCKER_IMAGE):fips \
	$(DOCKER_IMAGE)-test:latest \
	$(DOCKER_IMAGE)-devcontainer:latest \
	$(DOCKER_IMAGE)-integration-test:latest \
	$(DOCKER_IMAGE)-integration-node:latest \
	$(DOCKER_IMAGE)-e2e-sandbox-agent:latest \
	$(DOCKER_IMAGE)-software-factory-worker:$(DOCKER_TAG) \
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
ci: deps fmt-check vet lint security build test test-minimal test-standard test-lean race check-coverage ## Run CI pipeline locally (matches GitHub CI)
	@echo "$(GREEN)CI pipeline completed successfully$(NC)"

# integration-test-merge-result joins this list rather than `ci` above: `ci`
# is the Docker-free pipeline, and this suite runs in a container by
# construction. In GitHub CI it is its own job (go-quicbaas#982).
.PHONY: ci-full
ci-full: deps fmt-check vet lint security build test test-minimal test-standard test-lean test-desktop race coverage integration-test integration-test-merge-result ## Run full CI pipeline with integration tests
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
	@$(MAKE) --no-print-directory stage-gomodauth
	@trap '$(GOMODAUTH_CLEAR)' EXIT; \
	$(DOCKER_COMPOSE) -f test/integration/docker-compose.integration.yml up -d; \
	RC=$$?; $(GOMODAUTH_CLEAR); exit $$RC
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

.PHONY: test-fleet
test-fleet: ## Run fleet node daemon unit tests (pkg/fleet/*)
	@echo "$(BLUE)Running fleet node daemon tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -count=1 -tags 'rag mcp $(BUILD_TAGS)' ./pkg/fleet/...
	@echo "$(GREEN)Fleet node daemon tests passed$(NC)"

.PHONY: coverage-acp
coverage-acp: ## Generate coverage report for ACP package
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -tags 'rag $(BUILD_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-acp.out -covermode=atomic ./pkg/acp/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-acp.out -o $(COVERAGE_DIR)/coverage-acp.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-acp.out | grep total

.PHONY: bench-fleet
bench-fleet: ## Run fleet node daemon benchmarks
	@echo "$(BLUE)Running fleet node daemon benchmarks...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -bench=. -benchmem -benchtime=$(BENCHTIME) -run=^$$ -tags 'rag mcp $(BUILD_TAGS)' ./pkg/fleet/...
	@echo "$(GREEN)Fleet node daemon benchmarks completed$(NC)"

.PHONY: coverage-fleet
coverage-fleet: ## Generate coverage report for the fleet node daemon package
	@echo "$(BLUE)Generating fleet node daemon coverage report...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -v -race -count=1 -tags 'rag mcp $(BUILD_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-fleet.out -covermode=atomic ./pkg/fleet/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-fleet.out -o $(COVERAGE_DIR)/coverage-fleet.html
	@echo "$(YELLOW)Coverage report: file://$(COVERAGE_DIR)/coverage-fleet.html$(NC)"
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-fleet.out | grep total

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

.PHONY: integration-test-placement
integration-test-placement: ## Run scheduler placement + job integration tests over REST, gRPC, QUIC, Unix and MCP
	@echo "$(BLUE)Running scheduler placement integration tests...$(NC)"
	@QUICBAAS_TEST_FILTER="./test/integration/placement/..." WITH_RAFT=1 WITH_REST=1 WITH_QUIC=1 WITH_MCP=1 $(MAKE) integration-test

.PHONY: test-placement
test-placement: ## Run scheduler placement unit tests (engine, store, service, every transport, SDK, CLI)
	@echo "$(BLUE)Running scheduler placement unit tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) $(GOTEST) -race -count=1 -tags '$(BUILD_TAGS)' ./pkg/scheduler/placement/... ./pkg/service/ ./pkg/api/grpc/ ./pkg/api/rest/ ./pkg/api/mcp/ ./cmd/quicbaas/ -run 'Placement|Scheduler|RealNode|Evaluate|Validate|Register|Node|Release|Corrupt|Conversions|SDKREST'
	cd sdk/go && GOTOOLCHAIN=go$(GO_VERSION) $(GOTEST) -race -count=1 ./client/scheduler/... ./transport/embedded/ ./transport/grpc/ ./transport/unix/ ./transport/mcp/ -run 'Placement|Scheduler|Dispatch|Conversions'

.PHONY: coverage-placement
coverage-placement: ## Generate the scheduler placement engine coverage report
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) $(GOTEST) -count=1 -tags '$(BUILD_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-placement.out -covermode=atomic ./pkg/scheduler/placement/
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-placement.out -o $(COVERAGE_DIR)/coverage-placement.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-placement.out | grep total

.PHONY: bench-placement
bench-placement: ## Run scheduler placement engine benchmarks
	@echo "$(BLUE)Running scheduler placement benchmarks...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) $(GOTEST) -bench=. -benchmem -run=^$$ ./pkg/scheduler/placement/

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

.PHONY: integration-test-fleet-enrollment
integration-test-fleet-enrollment: ## Run the fleet enrollment E2E (register -> heartbeat with peer cert + bearer -> listed -> dispatch)
	@echo "$(BLUE)Running fleet enrollment E2E...$(NC)"
	@go test -v -count=1 -timeout 5m -tags 'integration $(strip $(INTEGRATION_BUILD_TAGS)) rest' ./test/integration/fleet_enrollment/...
	@echo "$(GREEN)Fleet enrollment E2E passed$(NC)"

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

.PHONY: integration-test-fleet-runtime
integration-test-fleet-runtime: ## Run fleet node daemon integration tests in Docker (central + Ollama qwen3:1.7b)
	@echo "$(BLUE)Running fleet runtime integration tests...$(NC)"
	@COMPOSE_FILE="test/integration/fleet_runtime/docker-compose.yml"; \
	if command -v nvidia-smi > /dev/null 2>&1 && nvidia-smi > /dev/null 2>&1; then \
		GPU_NAME=$$(nvidia-smi --query-gpu=name --format=csv,noheader | head -1); \
		GPU_MEM=$$(nvidia-smi --query-gpu=memory.total --format=csv,noheader | head -1); \
		echo "$(GREEN)  ✓ NVIDIA GPU detected: $$GPU_NAME ($$GPU_MEM)$(NC)"; \
	else \
		echo "$(YELLOW)  ⚠ No NVIDIA GPU detected - Ollama will use CPU inference$(NC)"; \
	fi; \
	echo "$(YELLOW)Starting fleet runtime test environment...$(NC)"; \
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
		go test -tags 'integration fleet_runtime' -v -count=1 -timeout 5m \
			$${QUICBAAS_TEST_FILTER:-./test/integration/fleet_runtime/...} 2>&1 | while IFS= read -r line; do echo "$$line"; done; \
	RESULT=$${PIPESTATUS[0]}; \
	echo "$(BLUE)Stopping fleet runtime test environment...$(NC)"; \
	docker compose -f $$COMPOSE_FILE down --volumes --remove-orphans; \
	if [ "$$RESULT" -ne 0 ]; then \
		echo "$(RED)Fleet runtime integration tests FAILED$(NC)"; \
		exit 1; \
	fi; \
	echo "$(GREEN)Fleet runtime integration tests passed$(NC)"

# =============================================================================
# Embedding Provider Tests
#
# pkg/embedding's real Provider interface sits behind `vector && qrdb_vector`.
# Without both tags, stub.go supplies a stub whose RegisterProvider returns
# ErrVectorNotEnabled -- so the NPU provider only exists in vector builds and
# these targets must set the tags explicitly.
# =============================================================================

EMBEDDING_TEST_TAGS := $(strip $(BUILD_TAGS)) vector qrdb_vector

.PHONY: test-embedding
test-embedding: ## Run embedding provider unit tests (pkg/embedding, vector build)
	@echo "$(BLUE)Running embedding unit tests...$(NC)"
	@$(GOTEST) -race -count=1 -tags '$(EMBEDDING_TEST_TAGS)' ./pkg/embedding/...
	@echo "$(GREEN)✓ Embedding unit tests passed$(NC)"

.PHONY: coverage-embedding
coverage-embedding: ## Generate embedding provider coverage report
	@echo "$(BLUE)Running embedding tests with coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) -race -count=1 -tags '$(EMBEDDING_TEST_TAGS)' \
		-coverprofile=$(COVERAGE_DIR)/coverage-embedding.out -covermode=atomic ./pkg/embedding/...
	@$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-embedding.out -o $(COVERAGE_DIR)/coverage-embedding.html
	@$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-embedding.out | grep -E "^(github|total)"

.PHONY: bench-embedding
bench-embedding: ## Run embedding provider benchmarks
	@echo "$(BLUE)Running embedding benchmarks...$(NC)"
	@$(GOTEST) -run '^$$' -bench=. -benchmem -benchtime=$(BENCHTIME) \
		-tags '$(EMBEDDING_TEST_TAGS)' ./pkg/embedding/...

# =============================================================================
# NPU Embedding Sidecar (Python)
#
# The sidecar owns tokenisation, shape bucketing, padding, masked mean pooling,
# the compiled-shape cache and CPU fallback. Its pure layers need only numpy and
# fastapi; OpenVINO is confined to engine.py and is exercised by the separate
# device tier below. That tier is a distinct suite, not a skipped part of the
# first, so nothing is ever silently not run.
# =============================================================================

NPU_SIDECAR_DIR := sidecar/npu-embedding
# PYTHON is the interpreter used to BUILD the sidecar's virtualenv, so a caller
# can still select a version (make test-npu-sidecar PYTHON=python3.13). The
# tests themselves always run inside that venv, never against ambient
# interpreter state, so a clean checkout needs nothing installed by hand.
PYTHON ?= python3
NPU_SIDECAR_VENV := $(NPU_SIDECAR_DIR)/.venv
NPU_SIDECAR_PY := $(CURDIR)/$(NPU_SIDECAR_VENV)/bin/python
# One stamp per installed extra set, rebuilt whenever pyproject.toml changes.
NPU_SIDECAR_DEV_STAMP := $(NPU_SIDECAR_VENV)/.stamp-dev
NPU_SIDECAR_DEVICE_STAMP := $(NPU_SIDECAR_VENV)/.stamp-device

$(NPU_SIDECAR_VENV)/bin/python:
	@echo "$(BLUE)Creating NPU sidecar virtualenv with $(PYTHON)...$(NC)"
	@$(PYTHON) -m venv $(NPU_SIDECAR_VENV)
	@$(NPU_SIDECAR_PY) -m pip install -q --upgrade pip

$(NPU_SIDECAR_DEV_STAMP): $(NPU_SIDECAR_DIR)/pyproject.toml | $(NPU_SIDECAR_VENV)/bin/python
	@echo "$(BLUE)Installing NPU sidecar with the dev extra...$(NC)"
	@cd $(NPU_SIDECAR_DIR) && $(NPU_SIDECAR_PY) -m pip install -q -e '.[dev]'
	@touch $@

$(NPU_SIDECAR_DEVICE_STAMP): $(NPU_SIDECAR_DIR)/pyproject.toml | $(NPU_SIDECAR_VENV)/bin/python
	@echo "$(BLUE)Installing NPU sidecar with the device and dev extras...$(NC)"
	@cd $(NPU_SIDECAR_DIR) && $(NPU_SIDECAR_PY) -m pip install -q -e '.[device,dev]'
	@touch $@

.PHONY: test-npu-sidecar
test-npu-sidecar: $(NPU_SIDECAR_DEV_STAMP) ## Run NPU embedding sidecar tests (no device required)
	@echo "$(BLUE)Running NPU embedding sidecar tests...$(NC)"
	@cd $(NPU_SIDECAR_DIR) && $(NPU_SIDECAR_PY) -m pytest tests/ -q -p no:warnings
	@echo "$(GREEN)✓ NPU sidecar tests passed$(NC)"

.PHONY: coverage-npu-sidecar
coverage-npu-sidecar: $(NPU_SIDECAR_DEV_STAMP) ## Generate NPU embedding sidecar coverage report
	@echo "$(BLUE)Running NPU sidecar tests with coverage...$(NC)"
	@cd $(NPU_SIDECAR_DIR) && $(NPU_SIDECAR_PY) -m pytest tests/ -q -p no:warnings \
		--cov=npu_embedding --cov-report=term-missing

.PHONY: test-npu-sidecar-device
test-npu-sidecar-device: $(NPU_SIDECAR_DEVICE_STAMP) ## Run NPU sidecar tests against real OpenVINO (installs the `device` extra into the sidecar venv)
	@echo "$(BLUE)Running NPU sidecar device tests (real OpenVINO)...$(NC)"
	@echo "$(YELLOW)Set NPU_EMBEDDING_TEST_DEVICE=NPU on the NPU host to exercise the device.$(NC)"
	@cd $(NPU_SIDECAR_DIR) && $(NPU_SIDECAR_PY) -m pytest tests_device/ -q -p no:warnings
	@echo "$(GREEN)✓ NPU sidecar device tests passed$(NC)"

.PHONY: clean-npu-sidecar
clean-npu-sidecar: ## Remove the NPU sidecar virtualenv (rebuilt on next use)
	@rm -rf $(NPU_SIDECAR_VENV)

.PHONY: integration-test-embedding
integration-test-embedding: ## Run embedding integration tests in Docker (Ollama via local CPU container; pass GPU_HOST=<host> for a remote GPU)
	@$(MAKE) integration-test QUICBAAS_TEST_FILTER="./test/integration/embedding/..." OLLAMA_CPU=$${OLLAMA_CPU:-1}

.PHONY: integration-test-orchestration
integration-test-orchestration: ## Run orchestrator E2E integration tests in Docker (Ollama via local CPU container; pass GPU_HOST=<host> for a remote GPU)
	@$(MAKE) integration-test QUICBAAS_TEST_FILTER="./test/integration/orchestration/..." OLLAMA_CPU=$${OLLAMA_CPU:-1}

.PHONY: integration-test-orchestrator
integration-test-orchestrator: ## Run multi-protocol CLI orchestrator inference tests (Ollama via local CPU container; pass GPU_HOST=<host> for a remote GPU)
	@$(MAKE) integration-test QUICBAAS_TEST_FILTER="-run TestOrchestratorInference ./test/integration/cli/..." OLLAMA_CPU=$${OLLAMA_CPU:-1}

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

# =============================================================================
# UI wire contract (pkg/uicontract)
# =============================================================================
#
# Compares the TypeScript SDK's hand-written response types against the Go
# structs the server serialises. Untagged on purpose: a contract test some
# build pass omits is a contract nothing checks.

.PHONY: test-uicontract
test-uicontract: ## Run the TypeScript/Go wire contract test
	@echo "$(BLUE)Running UI wire contract test...$(NC)"
	$(GOTEST) -v -count=1 ./pkg/uicontract/...

.PHONY: coverage-uicontract
coverage-uicontract: ## Generate coverage report for the uicontract package
	@mkdir -p $(COVERAGE_DIR)
	$(GOTEST) -v -count=1 -coverprofile=$(COVERAGE_DIR)/coverage-uicontract.out -covermode=atomic ./pkg/uicontract/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-uicontract.out -o $(COVERAGE_DIR)/coverage-uicontract.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-uicontract.out | grep total

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
# Pooled Provider Account Tests (the `quicbaas account` surface)
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

.PHONY: test-registry
test-registry: ## Run service registry unit tests (storage, service, every transport, SDK client, CLI)
	@echo "$(BLUE)Running service registry unit tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -count=1 -race -tags '$(TEST_TAGS)' ./pkg/registry/...
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -count=1 -race -tags '$(TEST_TAGS)' -run 'Registry' ./pkg/service/ ./pkg/api/grpc/ ./pkg/api/rest/ ./pkg/api/mcp/ ./cmd/quicbaas/
	cd sdk/go && GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -count=1 -race ./client/registry/... && \
		GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -count=1 -race -run 'Registry' ./transport/...

.PHONY: coverage-registry
coverage-registry: ## Generate coverage report for the service registry packages
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -tags '$(TEST_TAGS)' -coverprofile=$(COVERAGE_DIR)/coverage-registry.out -covermode=atomic ./pkg/registry/...
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-registry.out | grep total
	cd sdk/go && GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -cover ./client/registry/...

.PHONY: bench-registry
bench-registry: ## Run service registry benchmarks
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -bench=. -benchmem -run=^$$ ./pkg/registry/...

.PHONY: integration-test-registry
integration-test-registry: ## Run service registry CLI integration tests (all transports, in the devcontainer)
	@$(MAKE) integration-test WITH_REST=1 WITH_QUIC=1 WITH_MCP=1 QUICBAAS_TEST_FILTER="-run TestServiceRegistry ./test/integration/cli/..."

.PHONY: test-attestation
test-attestation: ## Run attestation Verifier unit tests (service, every transport, SDK, CLI)
	@echo "$(BLUE)Running attestation unit tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -count=1 -race -tags '$(TEST_TAGS)' -run 'Attestation|Verifier|ComparePCRBaseline' ./pkg/service/... ./pkg/api/grpc/ ./pkg/api/rest/ ./pkg/api/mcp/ ./cmd/quicbaas/
	cd sdk/go && GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -count=1 -race -run 'Attestation' . ./transport/...

.PHONY: coverage-attestation
coverage-attestation: ## Generate coverage report for the attestation Verifier
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -tags '$(TEST_TAGS)' -run 'Verifier|ComparePCRBaseline|Attestation' -coverprofile=$(COVERAGE_DIR)/coverage-attestation.out -covermode=atomic ./pkg/service/
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-attestation.out | grep -E 'attestation_verifier.go|total'

.PHONY: bench-attestation
bench-attestation: ## Run attestation Verifier benchmarks
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -tags '$(TEST_TAGS)' -bench='Appraise' -benchmem -run=^$$ ./pkg/service/

.PHONY: integration-test-attestation
integration-test-attestation: ## Run attestation Verifier CLI integration tests (all transports, in the devcontainer)
	@$(MAKE) integration-test WITH_REST=1 WITH_QUIC=1 WITH_MCP=1 QUICBAAS_TEST_FILTER="-run TestAttestationVerifier ./test/integration/cli/..."

.PHONY: integration-test-account
integration-test-account: ## Run pooled provider account CLI integration tests (all transports, in the devcontainer)
	@$(MAKE) integration-test WITH_REST=1 WITH_QUIC=1 WITH_MCP=1 QUICBAAS_TEST_FILTER="-run TestAgentAccount ./test/integration/cli/..."

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

# The `grep -v '/test/'` in test-sdk-go and test-sdk-go-coverage is a tracked
# exclusion, not an oversight: it keeps sdk/go/test/integration -- the
# docker-compose harness -- out of the per-push gate because the harness is
# known-red. Tracker: #50. Cause: its compose file builds the server from
# test/integration/Dockerfile.dev, which still copies sibling repos out of a
# `workspace` named build context and generates a go.work. That is the model
# dependency-resolution.md §2 abolished and that test/integration/Dockerfile
# already left behind. On a runner, the parent directory has no siblings, so the
# image build fails before a single test runs. The harness still runs, off the
# gate, in the `sdk-go-integration` job of .github/workflows/nightly.yml
# (schedule + workflow_dispatch) via `make integration-test-sdk-go`. When #50
# lands, moving it back into ci.yml is a deliberate change, not an automatic one.
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

# ==============================================================================
# Dynamic Secrets / Credential Lease Targets
# ==============================================================================
#
# The capability Security().DynamicSecrets() exposes: the lease manager whose
# state lives in the embedded qrdb node (pkg/lease), the credential generator SPI
# (pkg/credentials), the service handler, and the pooled-credential DELIVERY
# grants built over the same lease manager (go-quicbaas#902).

DYNSECRETS_PKGS = ./pkg/lease/... ./pkg/credentials/...

.PHONY: test-dynamic-secrets
test-dynamic-secrets: ## Run dynamic secrets + credential lease unit tests
	@echo "$(BLUE)Running dynamic secrets and credential lease tests...$(NC)"
	@$(GOTEST) -v -race -count=1 -tags '$(TEST_TAGS)' $(DYNSECRETS_PKGS)
	@echo "$(BLUE)Running the service handler and the delivery-grant surface...$(NC)"
	@$(GOTEST) -race -count=1 -tags '$(TEST_TAGS)' \
		-run 'DynamicSecret|Lease|Grant|Materialize' \
		./pkg/service/... ./pkg/server/... ./pkg/orchestration/... ./pkg/fleet/...
	@echo "$(GREEN)✓ Dynamic secrets tests passed$(NC)"

.PHONY: coverage-dynamic-secrets
coverage-dynamic-secrets: ## Generate coverage report for the dynamic secrets subsystem
	@echo "$(BLUE)Running dynamic secrets tests with coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) -tags '$(TEST_TAGS)' -coverprofile=$(COVERAGE_DIR)/dynamic-secrets.out -covermode=atomic $(DYNSECRETS_PKGS)
	@echo ""
	@echo "$(GREEN)Coverage Summary:$(NC)"
	@go tool cover -func=$(COVERAGE_DIR)/dynamic-secrets.out | grep -E "^(github|total)"
	@echo ""

.PHONY: bench-dynamic-secrets
bench-dynamic-secrets: ## Run lease manager benchmarks
	@echo "$(BLUE)Running lease manager benchmarks...$(NC)"
	@$(GOTEST) -bench=. -benchmem -benchtime=$(BENCHTIME) -tags '$(TEST_TAGS)' ./pkg/lease/...

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
verify-naming: ## Fail if the retired product word is back, or Seal* ships outside the documented alias surface
	@echo "$(BLUE)verify-naming: scanning for the retired product word and non-aliased Seal* code paths...$(NC)"
	@$(GOCMD) run ./scripts/verifynaming

.PHONY: check-reciprocal-pins
check-reciprocal-pins: ## Fail when this repo's modules pin each other, or a shared first-party dep, inconsistently
	@echo "$(BLUE)check-reciprocal-pins: comparing module pins across go.mod files...$(NC)"
	@scripts/check-reciprocal-pins.sh

.PHONY: verify-cli-flags
verify-cli-flags: ## Diff the init flag set of xkmsctl against quicbaas (STRICT=1 to fail on drift)
	@echo "$(BLUE)verify-cli-flags: comparing the xkmsctl and quicbaas init flag surfaces...$(NC)"
	@$(GOCMD) run ./scripts/verifycliflags $(if $(filter 1,$(STRICT)),-strict,)

.PHONY: verify-api-shapes
verify-api-shapes: ## Diff QuicBaaS Barrier*/Ceremony*/Bootstrap* TS types against go-qrdb canonical Go types
	@echo "$(BLUE)verify-api-shapes: comparing TypeScript types against go-qrdb canonical Go types...$(NC)"
	@$(GOCMD) run ./scripts/verifyapishapes

# Read a Makefile variable from a gate: `make print-DOCKER_LOCAL_IMAGES`. The
# gates under scripts/ ask make for the variables a producer may name, because
# make is the authority on what $(DOCKER_IMAGE) expands to.
print-%:
	@echo "$($*)"

.PHONY: verify-clean-docker
verify-clean-docker: ## Fail if clean-docker reaches beyond this project's image tags or hides a failure
	@echo "$(BLUE)verify-clean-docker: checking cleanup scope and failure propagation...$(NC)"
	@$(GOCMD) run ./scripts/verifycleandocker

.PHONY: verify-integration-teardown
verify-integration-teardown: ## Fail if the integration stack can outlive a run that left early
	@echo "$(BLUE)verify-integration-teardown: checking the stack comes down on the early-exit path...$(NC)"
	@$(GOCMD) run ./scripts/verifyteardown

.PHONY: verify-gomodauth
verify-gomodauth: ## Fail if the Docker test builds cannot fetch private modules off a runner, or keep the token after the build
	@echo "$(BLUE)verify-gomodauth: checking module credentials are staged on any host and cleared after the build...$(NC)"
	@$(GOCMD) run ./scripts/verifygomodauth

.PHONY: verify
verify: verify-naming verify-cli-flags verify-api-shapes verify-clean-docker verify-integration-teardown verify-gomodauth ## Run all cross-project verifications
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

# =============================================================================
# Issue triage (pkg/sdlc triage runner)
# =============================================================================
#
# Triage is the front half of the factory loop: which issues are ready to be
# worked. Its tests live beside the rest of the sdlc module, so test-sdlc is
# the whole module and test-triage is the narrow pass a lane working on the
# runner iterates with.

.PHONY: test-sdlc
test-sdlc: ## Run SDLC module unit tests (bindings, delivery, runner, triage)
	@echo "$(BLUE)Running SDLC module tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -race -count=1 -tags '$(TEST_TAGS)' ./pkg/sdlc/...
	@echo "$(GREEN)SDLC module unit tests passed$(NC)"

.PHONY: test-triage
test-triage: ## Run issue-triage unit tests (verdict, prompt, runner, scheduler)
	@echo "$(BLUE)Running issue triage tests...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -race -count=1 -tags '$(TEST_TAGS)' \
		-run 'Triage|TriageRunner|TriageScheduler|TriageSweep|WebhookIngest_Wakes' ./pkg/sdlc/...
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -race -count=1 -tags '$(TEST_TAGS)' \
		-run 'Triage' ./pkg/server/...
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -race -count=1 -tags '$(TEST_TAGS)' \
		-run 'ProjectCapacity|CountLiveJobLeases|JobLeasePolicy' ./pkg/orchestration/...
	@echo "$(GREEN)Issue triage unit tests passed$(NC)"

.PHONY: coverage-sdlc
coverage-sdlc: ## Generate coverage report for the SDLC module
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -count=1 -tags '$(TEST_TAGS)' \
		-coverprofile=$(COVERAGE_DIR)/coverage-sdlc.out -covermode=atomic ./pkg/sdlc/...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage-sdlc.out -o $(COVERAGE_DIR)/coverage-sdlc.html
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-sdlc.out | grep total

.PHONY: coverage-triage
coverage-triage: ## Report coverage of the issue-triage sources specifically
	@mkdir -p $(COVERAGE_DIR)
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -count=1 -tags '$(TEST_TAGS)' \
		-coverprofile=$(COVERAGE_DIR)/coverage-triage.out -covermode=atomic ./pkg/sdlc/...
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage-triage.out | grep -E 'triage_|^total'

.PHONY: bench-sdlc
bench-sdlc: ## Run SDLC module benchmarks
	@echo "$(BLUE)Running SDLC module benchmarks...$(NC)"
	GOTOOLCHAIN=go$(GO_VERSION) GONOSUMCHECK='*' $(GOTEST) -bench=. -benchmem -benchtime=$(BENCHTIME) \
		-run=^$$ -tags '$(TEST_TAGS)' ./pkg/sdlc/...
	@echo "$(GREEN)SDLC module benchmarks completed$(NC)"

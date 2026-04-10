# QuicBaaS Integration Test Runner Dockerfile
# Uses the devcontainer as base to ensure consistent environment
#
# Usage:
#   docker build -t quicbaas-integration-test -f test/integration/Dockerfile .
#
# The devcontainer provides all development dependencies including:
# - Go 1.26 with all tools (golangci-lint, gotestsum, delve, etc.)
# - Docker CLI and docker-compose for container management
# - Networking tools (curl, netcat, bind-tools)
# - Build tools (make, git)

# Build arguments
# BUILD_TAGS: Feature flags (iam, etc.) + codec tags (codec_json, codec_msgpack, etc.)
# Default includes all codecs for full integration test coverage
ARG BUILD_TAGS="local quic raft rest rag timeseries vector qrdb_vector iam mcp quicbaas_signal hashicorpmetrics codec_json codec_yaml codec_toml codec_msgpack codec_bson codec_cbor codec_avro codec_protobuf awss3 gcpstorage azureblob minio"

# ============================================================================
# Stage 1: Build binaries (lightweight Alpine builder)
# ============================================================================
FROM golang:1.26-alpine AS builder

ARG BUILD_TAGS

# Install minimal build dependencies
RUN apk add --no-cache git make bash ca-certificates

WORKDIR /build

# Copy module files first for caching
COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

# Copy local dependencies (staged by Makefile into .deps/)
COPY .deps/ /deps/

# Rewrite local replace directives to point to staged deps (root and sdk/go)
RUN sed -i 's|=> [^ ]*/go-quicraft|=> /deps/go-quicraft|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-qrdb/sdk/go|=> /deps/go-qrdb/sdk/go|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-qrdb$|=> /deps/go-qrdb|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-xkms/sdk/go|=> /deps/go-xkms/sdk/go|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-xkms$|=> /deps/go-xkms|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-signal|=> /deps/go-signal|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-libsignal|=> /deps/go-libsignal|g' go.mod sdk/go/go.mod

# Download remaining dependencies (remove go.sum to avoid checksum mismatch
# between local and Docker module proxy; COPY . . restores it for the build)
RUN rm -f go.sum sdk/go/go.sum && GONOSUMCHECK='*' GONOSUMDB='*' go mod download

# Copy source code
COPY . .

# Rewrite replace directives again (COPY . . overwrites go.mod and sdk/go/go.mod)
RUN sed -i 's|=> [^ ]*/go-quicraft|=> /deps/go-quicraft|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-qrdb/sdk/go|=> /deps/go-qrdb/sdk/go|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-qrdb$|=> /deps/go-qrdb|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-xkms/sdk/go|=> /deps/go-xkms/sdk/go|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-xkms$|=> /deps/go-xkms|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-signal|=> /deps/go-signal|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-libsignal|=> /deps/go-libsignal|g' go.mod sdk/go/go.mod

# Remove any stale debug files
RUN rm -f pkg/api/rest/debug_integration.go pkg/api/rest/debug_noop.go || true

# Build server binary only (CLI is built by Makefile and mounted)
RUN CGO_ENABLED=0 GOOS=linux GONOSUMCHECK='*' go build -mod=mod -tags "${BUILD_TAGS}" -buildvcs=false -ldflags "-w -s" -o bin/quicbaasd ./cmd/quicbaasd

# ============================================================================
# Stage 2: Integration test runtime (based on devcontainer)
# ============================================================================
FROM golang:1.26-alpine

# Re-declare ARG to make it available in this stage
ARG BUILD_TAGS

LABEL maintainer="Jeremy Hahn"
LABEL description="QuicBaaS integration test runner"
LABEL version="2.0.0"

# Install runtime dependencies matching devcontainer
RUN apk add --no-cache \
    bash \
    coreutils \
    curl \
    wget \
    jq \
    ca-certificates \
    tzdata \
    netcat-openbsd \
    bind-tools \
    git \
    make \
    docker-cli \
    docker-cli-compose

# Install Go development tools needed for testing
RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && \
    go install gotest.tools/gotestsum@latest

WORKDIR /app

# Copy source code for test execution
COPY . .

# Copy local dependencies (staged by Makefile into .deps/)
COPY .deps/ /deps/

# Rewrite local replace directives to point to staged deps (root and sdk/go)
RUN sed -i 's|=> [^ ]*/go-quicraft|=> /deps/go-quicraft|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-qrdb/sdk/go|=> /deps/go-qrdb/sdk/go|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-qrdb$|=> /deps/go-qrdb|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-xkms/sdk/go|=> /deps/go-xkms/sdk/go|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-xkms$|=> /deps/go-xkms|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-signal|=> /deps/go-signal|g' go.mod sdk/go/go.mod && \
    sed -i 's|=> [^ ]*/go-libsignal|=> /deps/go-libsignal|g' go.mod sdk/go/go.mod

# Download dependencies for test execution (remove go.sum to avoid checksum
# mismatch between local and Docker module proxy environments)
RUN rm -f go.sum sdk/go/go.sum && GONOSUMCHECK='*' GONOSUMDB='*' go mod download

# Copy built server binary from builder stage
COPY --from=builder /build/bin/quicbaasd /usr/local/bin/quicbaasd

# Copy health check script and set permissions
COPY test/integration/health_check.sh /usr/local/bin/health_check.sh
RUN chmod +x /usr/local/bin/health_check.sh /usr/local/bin/quicbaasd

# Create bin directory for mounted CLI binary
RUN mkdir -p /app/bin

# Environment variables for testing
# TEST_BUILD_TAGS: Persists BUILD_TAGS ARG to runtime for test execution
ENV QUICBAAS_TEST_MODE=integration \
    QUICBAAS_LOG_LEVEL=debug \
    QUICBAAS_LOG_FORMAT=text \
    CGO_ENABLED=0 \
    GO111MODULE=on \
    PATH="/app/bin:/usr/local/bin:${PATH}" \
    TEST_BUILD_TAGS="${BUILD_TAGS}"

# Default command: wait for cluster readiness, then run integration tests
CMD ["sh", "-c", "echo 'Waiting for cluster to be fully operational...' && /usr/local/bin/health_check.sh && echo 'Starting integration tests...' && echo \"Build tags: integration ${TEST_BUILD_TAGS}\" && go test -v -tags \"integration ${TEST_BUILD_TAGS}\" -timeout 30m ./test/integration/..."]

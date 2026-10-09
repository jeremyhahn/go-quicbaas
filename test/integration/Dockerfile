# QuicBaaS Integration Test Runner Dockerfile
# Runs the integration test binary with the cluster.
#
# Build model: first-party dependencies resolve from Gitea as tagged modules,
# exactly as they do for `go build` on a developer machine and in every other
# job (dependency-resolution.md §1). go.mod is the single source of truth.
#
# Until 2026-08-14 this image instead copied nine sibling repos out of a
# `workspace` named build context (`../../..` on the host) and generated a
# go.work inside the image. That model was abolished by §2, could not work on
# a CI runner -- where the parent directory holds no siblings, so the build
# died on `COPY --from=workspace go-xkms: "/go-xkms": not found` -- and had
# rotted unnoticed: the generated file pinned go-codec v1.3.0, listed go-frost
# twice at two different versions, and the build then deleted go.sum and ran
# with GOSUMDB=off GOFLAGS=-insecure, which is not a defensible way to build a
# security product.
#
# What the suite tests is unchanged. Only module resolution moved.

ARG BUILD_TAGS="local quic raft rest rag timeseries vector qrdb_vector iam mcp hashicorpmetrics codec_json codec_yaml codec_toml codec_msgpack codec_bson codec_cbor codec_protobuf gms_pure_go awss3 gcpstorage azureblob minio"

# ============================================================================
# Stage 1: Build the server binary
# ============================================================================
FROM golang:1.27.2-alpine AS builder

ARG BUILD_TAGS

RUN apk add --no-cache git make bash ca-certificates

WORKDIR /workspace/go-quicbaas

COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

# Gitea credentials arrive as a BuildKit secret, mounted on tmpfs for this RUN
# alone and sourced rather than exported, so the token reaches neither a layer,
# image metadata, nor a command line (dependency-resolution.md §6). The mount
# is optional: on GitHub no secret is supplied and these modules resolve from
# the public proxy once tags are mirrored.
RUN --mount=type=secret,id=gomodauth \
    set -eu; \
    if [ -f /run/secrets/gomodauth ]; then . /run/secrets/gomodauth; fi; \
    go mod download

COPY . .

# Remove any stale debug files
RUN rm -f pkg/api/rest/debug_integration.go pkg/api/rest/debug_noop.go || true

RUN CGO_ENABLED=0 GOOS=linux \
    go build -tags "${BUILD_TAGS}" -buildvcs=false -ldflags "-w -s" -o bin/quicbaasd ./cmd/quicbaasd

# The CLI the tests dispatch through. It used to be built on the host by
# `make integration-test` and bind-mounted in; it is built from tracked source,
# so it belongs in the image -- and a bind mount cannot work here anyway, since
# the source path would be resolved by the host daemon, which cannot see the
# job container's workspace.
RUN CGO_ENABLED=0 GOOS=linux \
    go build -tags "${BUILD_TAGS}" -buildvcs=false -ldflags "-w -s" -o bin/quicbaas ./cmd/quicbaas

# ============================================================================
# Stage 2: Test runtime — full Go toolchain plus the built server binary
# ============================================================================
FROM golang:1.27.2-alpine

ARG BUILD_TAGS

LABEL maintainer="Jeremy Hahn"
LABEL description="QuicBaaS integration test runner"
LABEL version="3.0.0"

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

RUN go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest && \
    go install gotest.tools/gotestsum@latest

WORKDIR /workspace/go-quicbaas

# The tests compile in this stage, so its module cache is populated here --
# `go mod download` covers the packages and tests of the main module, which is
# what `go test ./test/integration/...` needs. Warming it at build time is what
# lets the container run the suite without credentials of its own.
COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

RUN --mount=type=secret,id=gomodauth \
    set -eu; \
    if [ -f /run/secrets/gomodauth ]; then . /run/secrets/gomodauth; fi; \
    go mod download

COPY . .

# Binaries built in stage 1. The CLI lands on the PATH the tests already expect
# (/workspace/go-quicbaas/bin, first entry below) instead of arriving as a bind
# mount.
COPY --from=builder /workspace/go-quicbaas/bin/quicbaasd /usr/local/bin/quicbaasd
COPY --from=builder /workspace/go-quicbaas/bin/quicbaas /workspace/go-quicbaas/bin/quicbaas

# Health check script comes from the project context
COPY test/integration/health_check.sh /usr/local/bin/health_check.sh
RUN chmod +x /usr/local/bin/health_check.sh /usr/local/bin/quicbaasd /workspace/go-quicbaas/bin/quicbaas

# GOSUMDB=off and GOINSECURE are deliberately absent: the module cache is
# warmed above, and go.sum is the integrity record rather than something to
# delete. TEST_BUILD_TAGS persists BUILD_TAGS to runtime for test execution.
ENV QUICBAAS_TEST_MODE=integration \
    QUICBAAS_LOG_LEVEL=debug \
    QUICBAAS_LOG_FORMAT=text \
    CGO_ENABLED=0 \
    GO111MODULE=on \
    PATH="/workspace/go-quicbaas/bin:/usr/local/bin:${PATH}" \
    TEST_BUILD_TAGS="${BUILD_TAGS}"

# Default command: wait for cluster readiness, then run integration tests.
CMD ["sh", "-c", "echo 'Waiting for cluster to be fully operational...' && /usr/local/bin/health_check.sh && echo 'Starting integration tests...' && echo \"Build tags: integration ${TEST_BUILD_TAGS}\" && go test -v -tags \"integration ${TEST_BUILD_TAGS}\" -timeout 30m ./test/integration/..."]

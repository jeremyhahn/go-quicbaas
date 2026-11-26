# QuicBaaS integration test runner image.

ARG BUILD_TAGS="local quic raft rest rag timeseries vector qrdb_vector iam mcp hashicorpmetrics codec_json codec_yaml codec_toml codec_msgpack codec_bson codec_cbor codec_protobuf gms_pure_go awss3 gcpstorage azureblob minio"

# Stage 1: build the server binary.
FROM golang:1.27.4-alpine AS builder

ARG BUILD_TAGS

RUN apk add --no-cache git make bash ca-certificates

WORKDIR /workspace/go-quicbaas

COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

# Module credentials arrive as a BuildKit secret on tmpfs, sourced for this run only, so the token reaches no layer or command line.
RUN --mount=type=secret,id=gomodauth \
    set -eu; \
    if [ -f /run/secrets/gomodauth ]; then set -a; . /run/secrets/gomodauth; set +a; fi; \
    go mod download

COPY . .

# Remove any stale debug files
RUN rm -f pkg/api/rest/debug_integration.go pkg/api/rest/debug_noop.go || true

RUN CGO_ENABLED=0 GOOS=linux \
    go build -tags "${BUILD_TAGS}" -buildvcs=false -ldflags "-w -s" -o bin/quicbaasd ./cmd/quicbaasd

# The CLI the tests dispatch through.
RUN CGO_ENABLED=0 GOOS=linux \
    go build -tags "${BUILD_TAGS}" -buildvcs=false -ldflags "-w -s" -o bin/quicbaas ./cmd/quicbaas

# Stage 2: test runtime, the Go toolchain and the built server binary.
FROM golang:1.27.4-alpine

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
    openssh-keygen \
    make \
    docker-cli \
    docker-cli-compose

# golangci-lint at the pin in pkg/version/toolchain.mk.
COPY pkg/version/toolchain.mk /tmp/toolchain.mk
RUN set -eu; \
    golangci_version=$(sed -n 's/^GOLANGCI_VERSION := //p' /tmp/toolchain.mk); \
    test -n "$golangci_version" || { echo "GOLANGCI_VERSION missing from toolchain.mk" >&2; exit 1; }; \
    go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${golangci_version}"; \
    go install gotest.tools/gotestsum@latest; \
    rm -f /tmp/toolchain.mk

WORKDIR /workspace/go-quicbaas

# The tests compile in this stage, so its module cache is filled here.
COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

RUN --mount=type=secret,id=gomodauth \
    set -eu; \
    if [ -f /run/secrets/gomodauth ]; then set -a; . /run/secrets/gomodauth; set +a; fi; \
    go mod download

COPY . .

# Binaries built in stage 1.
COPY --from=builder /workspace/go-quicbaas/bin/quicbaasd /usr/local/bin/quicbaasd
COPY --from=builder /workspace/go-quicbaas/bin/quicbaas /workspace/go-quicbaas/bin/quicbaas

# Health check script comes from the project context
COPY test/integration/health_check.sh /usr/local/bin/health_check.sh
RUN chmod +x /usr/local/bin/health_check.sh /usr/local/bin/quicbaasd /workspace/go-quicbaas/bin/quicbaas

# GOSUMDB=off and GOINSECURE are deliberately absent; go.sum is the integrity record.
ENV QUICBAAS_TEST_MODE=integration \
    QUICBAAS_LOG_LEVEL=debug \
    QUICBAAS_LOG_FORMAT=text \
    CGO_ENABLED=0 \
    GO111MODULE=on \
    PATH="/workspace/go-quicbaas/bin:/usr/local/bin:${PATH}" \
    TEST_BUILD_TAGS="${BUILD_TAGS}"

# Default command: wait for cluster readiness, then run integration tests.
CMD ["sh", "-c", "echo 'Waiting for cluster to be fully operational...' && /usr/local/bin/health_check.sh && echo 'Starting integration tests...' && echo \"Build tags: integration ${TEST_BUILD_TAGS}\" && go test -v -tags \"integration ${TEST_BUILD_TAGS}\" -timeout 30m ./test/integration/... ./cmd/quicbaasd"]

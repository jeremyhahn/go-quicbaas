# QuicBaaS Integration Test Runner Dockerfile
# Runs the integration test binary with the cluster.
#
# Build model: a Go workspace (go.work) is reconstructed inside the image
# from sibling repos supplied via Docker BuildKit's `workspace` named
# context (see docker-compose.integration.yml). go.work is then the single
# source of truth for cross-module resolution — same model as local dev.
# No `.deps/` staging or replace-directive rewriting required.

ARG BUILD_TAGS="local quic raft rest rag timeseries vector qrdb_vector iam mcp hashicorpmetrics codec_json codec_yaml codec_toml codec_msgpack codec_bson codec_cbor codec_avro codec_protobuf awss3 gcpstorage azureblob minio"

# ============================================================================
# Stage 1: Build server binary inside a Go workspace mirroring host layout
# ============================================================================
FROM golang:1.26-alpine AS builder

ARG BUILD_TAGS

RUN apk add --no-cache git make bash ca-certificates

WORKDIR /workspace

# Pull each sibling go-quicbaas needs from the workspace named context.
COPY --from=workspace go-libsignal  ./go-libsignal
COPY --from=workspace go-objstore   ./go-objstore
COPY --from=workspace go-qrdb       ./go-qrdb
COPY --from=workspace go-quicraft   ./go-quicraft
COPY --from=workspace go-signal     ./go-signal
COPY --from=workspace go-truststrap ./go-truststrap
COPY --from=workspace go-xkms       ./go-xkms
COPY --from=workspace go-codec      ./go-codec
COPY --from=workspace go-frost       ./go-frost

# go-quicbaas itself comes from the default context (project root).
COPY . ./go-quicbaas

# Pruned go.work — see Dockerfile.node for rationale.
RUN { \
        echo 'go 1.26.4'; \
        echo ''; \
        echo 'use ('; \
        echo '	./go-libsignal'; \
        echo '	./go-objstore'; \
        echo '	./go-objstore/api/sdks/go'; \
        echo '	./go-qrdb'; \
        echo '	./go-qrdb/sdk/go'; \
        echo '	./go-quicbaas'; \
        echo '	./go-quicbaas/sdk/go'; \
        echo '	./go-quicraft'; \
        echo '	./go-signal'; \
        echo '	./go-xkms'; \
        echo '	./go-codec'; \
        echo '	./go-frost'; \
        echo '	./go-xkms/sdk/go'; \
        echo '	./go-xkms/xkey'; \
        echo ')'; \
        echo ''; \
        echo 'replace ('; \
        echo '	github.com/jeremyhahn/go-libsignal v0.0.0       => ./go-libsignal'; \
        echo '	github.com/jeremyhahn/go-objstore v0.0.0        => ./go-objstore'; \
        echo '	github.com/jeremyhahn/go-qrdb v0.0.0            => ./go-qrdb'; \
        echo '	github.com/jeremyhahn/go-qrdb/sdk/go v0.0.0     => ./go-qrdb/sdk/go'; \
        echo '	github.com/jeremyhahn/go-quicbaas v0.0.0        => ./go-quicbaas'; \
        echo '	github.com/jeremyhahn/go-quicbaas/sdk/go v0.0.0 => ./go-quicbaas/sdk/go'; \
        echo '	github.com/jeremyhahn/go-quicraft v0.0.0        => ./go-quicraft'; \
        echo '	github.com/jeremyhahn/go-signal v0.0.0          => ./go-signal'; \
        echo '	github.com/jeremyhahn/go-xkms v0.0.0            => ./go-xkms'; \
        echo '	github.com/jeremyhahn/go-codec v1.3.0          => ./go-codec'; \
        echo '	github.com/jeremyhahn/go-frost v0.1.4-alpha    => ./go-frost'; \
        echo '	github.com/jeremyhahn/go-frost v0.1.6-alpha    => ./go-frost'; \
        echo '	github.com/jeremyhahn/go-xkms/sdk/go v0.0.0     => ./go-xkms/sdk/go'; \
        echo '	github.com/jeremyhahn/go-objstore v0.1.4-alpha => ./go-objstore'; \
        echo '	github.com/jeremyhahn/go-xkms v0.2.2-alpha    => ./go-xkms'; \
        echo ')'; \
    } > go.work

WORKDIR /workspace/go-quicbaas

# Remove any stale debug files
RUN rm -f pkg/api/rest/debug_integration.go pkg/api/rest/debug_noop.go || true

# Drop go.sum: workspace `use` directives resolve sibling modules locally,
# but go.sum still gets consulted and may be out of sync with workspace
# versions (e.g. an in-flight sibling change). Letting Go regenerate sums
# under workspace mode is the correct behavior here.
RUN rm -f go.sum sdk/go/go.sum

# Build server binary
RUN CGO_ENABLED=0 GOOS=linux GOSUMDB=off GOFLAGS=-insecure \
    go build -tags "${BUILD_TAGS}" -buildvcs=false -ldflags "-w -s" -o bin/quicbaasd ./cmd/quicbaasd

# ============================================================================
# Stage 2: Test runtime — full Go toolchain, workspace, built server binary
# ============================================================================
FROM golang:1.26-alpine

ARG BUILD_TAGS

LABEL maintainer="Jeremy Hahn"
LABEL description="QuicBaaS integration test runner"
LABEL version="2.0.0"

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

RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && \
    go install gotest.tools/gotestsum@latest

WORKDIR /workspace

# Reconstruct the same workspace layout for the test runtime so `go test`
# resolves all sibling modules. (Same COPY shape as the builder stage; the
# stages cannot share filesystem, so each lays out its own workspace.)
COPY --from=workspace go-libsignal  ./go-libsignal
COPY --from=workspace go-objstore   ./go-objstore
COPY --from=workspace go-qrdb       ./go-qrdb
COPY --from=workspace go-quicraft   ./go-quicraft
COPY --from=workspace go-signal     ./go-signal
COPY --from=workspace go-truststrap ./go-truststrap
COPY --from=workspace go-xkms       ./go-xkms
COPY --from=workspace go-codec      ./go-codec
COPY --from=workspace go-frost       ./go-frost
COPY . ./go-quicbaas

RUN { \
        echo 'go 1.26.4'; \
        echo ''; \
        echo 'use ('; \
        echo '	./go-libsignal'; \
        echo '	./go-objstore'; \
        echo '	./go-objstore/api/sdks/go'; \
        echo '	./go-qrdb'; \
        echo '	./go-qrdb/sdk/go'; \
        echo '	./go-quicbaas'; \
        echo '	./go-quicbaas/sdk/go'; \
        echo '	./go-quicraft'; \
        echo '	./go-signal'; \
        echo '	./go-xkms'; \
        echo '	./go-codec'; \
        echo '	./go-frost'; \
        echo '	./go-xkms/sdk/go'; \
        echo '	./go-xkms/xkey'; \
        echo ')'; \
        echo ''; \
        echo 'replace ('; \
        echo '	github.com/jeremyhahn/go-libsignal v0.0.0       => ./go-libsignal'; \
        echo '	github.com/jeremyhahn/go-objstore v0.0.0        => ./go-objstore'; \
        echo '	github.com/jeremyhahn/go-qrdb v0.0.0            => ./go-qrdb'; \
        echo '	github.com/jeremyhahn/go-qrdb/sdk/go v0.0.0     => ./go-qrdb/sdk/go'; \
        echo '	github.com/jeremyhahn/go-quicbaas v0.0.0        => ./go-quicbaas'; \
        echo '	github.com/jeremyhahn/go-quicbaas/sdk/go v0.0.0 => ./go-quicbaas/sdk/go'; \
        echo '	github.com/jeremyhahn/go-quicraft v0.0.0        => ./go-quicraft'; \
        echo '	github.com/jeremyhahn/go-signal v0.0.0          => ./go-signal'; \
        echo '	github.com/jeremyhahn/go-xkms v0.0.0            => ./go-xkms'; \
        echo '	github.com/jeremyhahn/go-codec v1.3.0          => ./go-codec'; \
        echo '	github.com/jeremyhahn/go-frost v0.1.4-alpha    => ./go-frost'; \
        echo '	github.com/jeremyhahn/go-frost v0.1.6-alpha    => ./go-frost'; \
        echo '	github.com/jeremyhahn/go-xkms/sdk/go v0.0.0     => ./go-xkms/sdk/go'; \
        echo '	github.com/jeremyhahn/go-objstore v0.1.4-alpha => ./go-objstore'; \
        echo '	github.com/jeremyhahn/go-xkms v0.2.2-alpha    => ./go-xkms'; \
        echo ')'; \
    } > go.work

# Server binary built in stage 1
COPY --from=builder /workspace/go-quicbaas/bin/quicbaasd /usr/local/bin/quicbaasd

# Health check script comes from the project context
COPY test/integration/health_check.sh /usr/local/bin/health_check.sh
RUN chmod +x /usr/local/bin/health_check.sh /usr/local/bin/quicbaasd

# CLI binary directory is mounted at runtime (built by Makefile)
RUN mkdir -p /workspace/go-quicbaas/bin

WORKDIR /workspace/go-quicbaas

# TEST_BUILD_TAGS persists BUILD_TAGS ARG to runtime for test execution
ENV QUICBAAS_TEST_MODE=integration \
    QUICBAAS_LOG_LEVEL=debug \
    QUICBAAS_LOG_FORMAT=text \
    CGO_ENABLED=0 \
    GO111MODULE=on \
    GOSUMDB=off \
    GOINSECURE=github.com/jeremyhahn/* \
    PATH="/workspace/go-quicbaas/bin:/usr/local/bin:${PATH}" \
    TEST_BUILD_TAGS="${BUILD_TAGS}"

# Default command: wait for cluster readiness, then run integration tests.
CMD ["sh", "-c", "rm -f go.sum sdk/go/go.sum && echo 'Waiting for cluster to be fully operational...' && /usr/local/bin/health_check.sh && echo 'Starting integration tests...' && echo \"Build tags: integration ${TEST_BUILD_TAGS}\" && go test -v -tags \"integration ${TEST_BUILD_TAGS}\" -timeout 30m ./test/integration/..."]

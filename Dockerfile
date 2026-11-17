# QuicBaaS Production Dockerfile
# Multi-stage build for minimal final image with static binary

# Build arguments
ARG GO_VERSION=1.27
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
# gms_pure_go is the floor, not a feature: the builder below is CGO_ENABLED=0
# and dolthub/go-mysql-server's internal/regex has no file satisfying
# `cgo && !gms_pure_go` without cgo, so the link fails on "build constraints
# exclude all Go files". Callers passing BUILD_TAGS must keep it in the list.
ARG BUILD_TAGS=gms_pure_go

# Stage 1: Build stage
FROM golang:${GO_VERSION}-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make ca-certificates tzdata file

# Set working directory
WORKDIR /build

# Copy module files first for caching
COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

# Until 2026-08-14 this stage copied working-tree clones out of a .deps/
# directory and rewrote a set of relative `replace` directives to point at
# them. Both are gone: .deps/ was deleted with the decoupling, and the only
# replaces left in go.mod are the intra-repo sdk/go one and an
# armon/go-metrics version pin, neither of which needs rewriting. The COPY of
# a directory that no longer exists failed the build outright.
#
# go.mod pins first-party modules, which this download resolves through the
# module proxy and the checksum database like any other module.
#
# Fetch credentials arrive as a BuildKit secret, never a build arg -- an arg is recorded in
# image metadata and readable with `docker history`, while a secret is mounted
# on tmpfs for this RUN alone and never becomes a layer. It is sourced rather
# than exported so the value never reaches a command line either (`ps` on the
# build host would show it).
#
# The mount is optional by design. On GitHub Actions no secret is supplied and
# these modules resolve from the public proxy once tags are mirrored (§7), so
# the same Dockerfile builds on both hosts.
RUN --mount=type=secret,id=gomodauth \
    set -eu; \
    if [ -f /run/secrets/gomodauth ]; then . /run/secrets/gomodauth; fi; \
    go mod download

# Copy source code
COPY . .

# Build arguments
ARG VERSION
ARG COMMIT
ARG DATE
ARG BUILD_TAGS

# Build static binaries using downloaded dependencies
RUN CGO_ENABLED=0 GOOS=linux go build -mod=mod -tags "${BUILD_TAGS}" -buildvcs=false \
    -ldflags="-w -s -X github.com/jeremyhahn/go-quicbaas/pkg/version.Version=${VERSION} -X github.com/jeremyhahn/go-quicbaas/pkg/version.BuildTime=${DATE} -X github.com/jeremyhahn/go-quicbaas/pkg/version.GitCommit=${COMMIT}" \
    -o quicbaasd ./cmd/quicbaasd

RUN CGO_ENABLED=0 GOOS=linux go build -mod=mod -tags "${BUILD_TAGS}" -buildvcs=false \
    -ldflags="-w -s -X github.com/jeremyhahn/go-quicbaas/pkg/version.Version=${VERSION} -X github.com/jeremyhahn/go-quicbaas/pkg/version.BuildTime=${DATE} -X github.com/jeremyhahn/go-quicbaas/pkg/version.GitCommit=${COMMIT}" \
    -o quicbaas ./cmd/quicbaas

# Verify static binaries
RUN file quicbaasd && file quicbaas

# Stage 2: Runtime stage (minimal)
#
# The base is PINNED, and the packages are upgraded on top of it. Those pull in
# opposite directions on purpose, and the image needs both:
#
#   * `alpine:latest` made the shipped image unreproducible — whether a given
#     build carried a vulnerability depended on when it ran, with no commit
#     behind the change. Same defect class as installing a scanner at
#     `@latest`, which ci.yml pins for the same reason.
#   * A pin alone would freeze the base at whatever packages it shipped with,
#     so a patched libcrypto3 could never reach the image without a bump here.
#     `apk upgrade` is what actually closes a CVE between base releases.
#
# Bump the pin deliberately; `apk upgrade` covers the interval in between.
FROM alpine:3.24.1

# Install runtime dependencies, patched. Measured 2026-08-26: alpine 3.24.1
# ships libcrypto3/libssl3 3.5.7-r0, which carries CVE-2026-14456 (HIGH,
# openssl DoS); `apk upgrade` takes them to 3.5.8-r0 and the image scans clean.
# The daemon runs worker and train containers through the docker CLI and needs git (go-quicbaas#2228).
RUN apk upgrade --no-cache && \
    apk --no-cache add ca-certificates tzdata docker-cli git

# Create non-root user
RUN addgroup -g 1000 quicbaas && \
    adduser -D -u 1000 -G quicbaas quicbaas

# Create directories
RUN mkdir -p /data /var/log/quicbaas /var/run/quicbaas && \
    chown -R quicbaas:quicbaas /data /var/log/quicbaas /var/run/quicbaas

# Copy binaries from builder
COPY --from=builder /build/quicbaasd /usr/local/bin/quicbaasd
COPY --from=builder /build/quicbaas /usr/local/bin/quicbaas

# Set ownership
RUN chown quicbaas:quicbaas /usr/local/bin/quicbaasd /usr/local/bin/quicbaas

# Switch to non-root user
USER quicbaas

# Set working directory
WORKDIR /data

# Expose ports
# 8080: HTTP/REST API
# 9090: gRPC API
# 63001-63010: Raft cluster communication
EXPOSE 8080 9090 63001-63010

# Health check using CLI
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD quicbaas --address /var/run/quicbaas/quicbaas.sock cluster status || exit 1

# Volume for data persistence
VOLUME ["/data"]

# Default environment variables
ENV QUICBAAS_LOG_LEVEL=info \
    QUICBAAS_LOG_FORMAT=json \
    QUICBAAS_STORAGE_DATA_DIR=/data \
    QUICBAAS_SERVER_UNIX_SOCKET=/var/run/quicbaas/quicbaas.sock

# Entry point - the daemon
ENTRYPOINT ["quicbaasd"]

# Default command - use config file if present
CMD ["--config", "/data/config.yaml"]

# Labels
LABEL org.opencontainers.image.title="QuicBaaS" \
      org.opencontainers.image.description="High-performance distributed key-value database" \
      org.opencontainers.image.vendor="QuicBaaS" \
      org.opencontainers.image.licenses="Apache-2.0"

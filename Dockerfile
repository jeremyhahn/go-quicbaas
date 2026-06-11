# QuicBaaS Production Dockerfile
# Multi-stage build for minimal final image with static binary

# Build arguments
ARG GO_VERSION=1.26
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
ARG BUILD_TAGS=

# Stage 1: Build stage
FROM golang:${GO_VERSION}-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make ca-certificates tzdata file

# Set working directory
WORKDIR /build

# Copy module files first for caching
COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

# Copy local dependencies (staged by Makefile into .deps/)
COPY .deps/ /deps/

# Rewrite local replace directives to point to staged deps
RUN sed -i 's|=> \.\./go-quicraft|=> /deps/go-quicraft|g' go.mod && \
    sed -i 's|=> \.\./go-qrdb/sdk/go|=> /deps/go-qrdb/sdk/go|g' go.mod && \
    sed -i 's|=> \.\./go-qrdb$|=> /deps/go-qrdb|g' go.mod && \
    sed -i 's|=> \.\./go-xkms/sdk/go|=> /deps/go-xkms/sdk/go|g' go.mod && \
    sed -i 's|=> \.\./go-xkms$|=> /deps/go-xkms|g' go.mod && \
    sed -i 's|=> \.\./go-signal$|=> /deps/go-signal|g' go.mod && \
    sed -i 's|=> \.\./go-libsignal$|=> /deps/go-libsignal|g' go.mod && \
    sed -i 's|=> \.\./\.\./\.\./go-quicraft|=> /deps/go-quicraft|g' sdk/go/go.mod && \
    sed -i 's|=> \.\./\.\./\.\./go-qrdb/sdk/go|=> /deps/go-qrdb/sdk/go|g' sdk/go/go.mod && \
    sed -i 's|=> \.\./\.\./\.\./go-qrdb$|=> /deps/go-qrdb|g' sdk/go/go.mod && \
    sed -i 's|=> \.\./\.\./\.\./go-xkms/sdk/go|=> /deps/go-xkms/sdk/go|g' sdk/go/go.mod && \
    sed -i 's|=> \.\./\.\./\.\./go-xkms$|=> /deps/go-xkms|g' sdk/go/go.mod

# Download remaining dependencies
RUN GONOSUMCHECK='*' go mod download

# Copy source code
COPY . .

# Rewrite replace directives again (COPY . . overwrites go.mod)
RUN sed -i 's|=> \.\./go-quicraft|=> /deps/go-quicraft|g' go.mod && \
    sed -i 's|=> \.\./go-qrdb/sdk/go|=> /deps/go-qrdb/sdk/go|g' go.mod && \
    sed -i 's|=> \.\./go-qrdb$|=> /deps/go-qrdb|g' go.mod && \
    sed -i 's|=> \.\./go-xkms/sdk/go|=> /deps/go-xkms/sdk/go|g' go.mod && \
    sed -i 's|=> \.\./go-xkms$|=> /deps/go-xkms|g' go.mod && \
    sed -i 's|=> \.\./go-signal$|=> /deps/go-signal|g' go.mod && \
    sed -i 's|=> \.\./go-libsignal$|=> /deps/go-libsignal|g' go.mod && \
    sed -i 's|=> \.\./\.\./\.\./go-quicraft|=> /deps/go-quicraft|g' sdk/go/go.mod && \
    sed -i 's|=> \.\./\.\./\.\./go-qrdb/sdk/go|=> /deps/go-qrdb/sdk/go|g' sdk/go/go.mod && \
    sed -i 's|=> \.\./\.\./\.\./go-qrdb$|=> /deps/go-qrdb|g' sdk/go/go.mod && \
    sed -i 's|=> \.\./\.\./\.\./go-xkms/sdk/go|=> /deps/go-xkms/sdk/go|g' sdk/go/go.mod && \
    sed -i 's|=> \.\./\.\./\.\./go-xkms$|=> /deps/go-xkms|g' sdk/go/go.mod

# Build arguments
ARG VERSION
ARG COMMIT
ARG DATE
ARG BUILD_TAGS

# Build static binaries using downloaded dependencies
RUN CGO_ENABLED=0 GOOS=linux GONOSUMCHECK='*' go build -mod=mod -tags "${BUILD_TAGS}" -buildvcs=false \
    -ldflags="-w -s -X github.com/jeremyhahn/go-quicbaas/pkg/version.Version=${VERSION} -X github.com/jeremyhahn/go-quicbaas/pkg/version.BuildTime=${DATE} -X github.com/jeremyhahn/go-quicbaas/pkg/version.GitCommit=${COMMIT}" \
    -o quicbaasd ./cmd/quicbaasd

RUN CGO_ENABLED=0 GOOS=linux GONOSUMCHECK='*' go build -mod=mod -tags "${BUILD_TAGS}" -buildvcs=false \
    -ldflags="-w -s -X github.com/jeremyhahn/go-quicbaas/pkg/version.Version=${VERSION} -X github.com/jeremyhahn/go-quicbaas/pkg/version.BuildTime=${DATE} -X github.com/jeremyhahn/go-quicbaas/pkg/version.GitCommit=${COMMIT}" \
    -o quicbaas ./cmd/quicbaas

# Verify static binaries
RUN file quicbaasd && file quicbaas

# Stage 2: Runtime stage (minimal)
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

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

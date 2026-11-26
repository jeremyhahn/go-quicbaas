# QuicBaaS agent daemon container.

# Builder stage: compile the quicbaas binary
FROM golang:1.27.4-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /build

# Copy go mod files (assumes this is at repo root during build)
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build quicbaas binary (CLI + daemon)
RUN make build BIN_DIR=/build/bin

# Final image: Alpine, the quicbaas binary and minimal tooling.
FROM alpine:3.21

# Install minimal runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    && rm -rf /var/cache/apk/*

# Create daemon user and group (UID/GID match systemd unit)
RUN addgroup -g 1098 quicbaas-agent && \
    adduser -D -u 1098 -G quicbaas-agent -h /var/lib/quicbaas-agent quicbaas-agent

# Copy quicbaas binary from builder
COPY --from=builder /build/bin/quicbaas /usr/local/bin/quicbaas
RUN chmod 755 /usr/local/bin/quicbaas

# Create daemon work directory with correct permissions
RUN mkdir -p /var/lib/quicbaas-agent && \
    chown quicbaas-agent:quicbaas-agent /var/lib/quicbaas-agent && \
    chmod 700 /var/lib/quicbaas-agent

# Switch to non-root user
USER quicbaas-agent
WORKDIR /var/lib/quicbaas-agent

# The daemon's AgentRuntime gRPC service; default bind 127.0.0.1:9091, override with QUICBAAS_BIND_ADDR.
EXPOSE 9091

# Health check: verify daemon is responding to enrollment ping
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD quicbaas --help > /dev/null 2>&1 || exit 1

# Entrypoint: start the daemon.
ENTRYPOINT ["/bin/sh", "-c"]
CMD ["exec /usr/local/bin/quicbaas fleet serve \
  --server \"${QUICBAAS_SERVER}\" \
  --tenant \"${QUICBAAS_TENANT}\" \
  --bind \"${QUICBAAS_BIND_ADDR:-0.0.0.0:9091}\" \
  --backends \"${QUICBAAS_BACKENDS:-acp,ollama}\" \
  --sandbox-policy \"${QUICBAAS_SANDBOX_POLICY:-required}\" \
  --container-runtime \"${QUICBAAS_CONTAINER_RUNTIME:-auto}\" \
  --node-cert \"${QUICBAAS_CREDENTIALS_DIR:-/etc/quicbaas/agent}/agent.crt\" \
  --node-key \"${QUICBAAS_CREDENTIALS_DIR:-/etc/quicbaas/agent}/agent.key\" \
  --server-ca \"${QUICBAAS_CREDENTIALS_DIR:-/etc/quicbaas/agent}/server-ca.crt\" \
  --work-dir \"${QUICBAAS_WORK_DIR:-/var/lib/quicbaas-agent}\" \
  ${QUICBAAS_SANDBOX_NETWORK:+--sandbox-network \"${QUICBAAS_SANDBOX_NETWORK}\"} \
  ${QUICBAAS_SANDBOX_CAP_ADD:+--sandbox-cap-add \"${QUICBAAS_SANDBOX_CAP_ADD}\"} \
  ${QUICBAAS_SANDBOX_PIDS_LIMIT:+--sandbox-pids-limit \"${QUICBAAS_SANDBOX_PIDS_LIMIT}\"} \
  ${QUICBAAS_SANDBOX_ULIMITS:+--sandbox-ulimit \"${QUICBAAS_SANDBOX_ULIMITS}\"} \
  ${QUICBAAS_ALLOWED_MOUNT_ROOTS:+--allowed-mount-root \"${QUICBAAS_ALLOWED_MOUNT_ROOTS}\"} \
  ${QUICBAAS_AUTO_PULL:+--auto-pull} \
  ${QUICBAAS_IMAGE_MAP:+--image-map \"${QUICBAAS_IMAGE_MAP}\"} \
  ${QUICBAAS_IMAGE_REGISTRY:+--image-registry \"${QUICBAAS_IMAGE_REGISTRY}\"} \
  ${QUICBAAS_MCP_ENDPOINT:+--mcp-endpoint \"${QUICBAAS_MCP_ENDPOINT}\"} \
  ${QUICBAAS_HEARTBEAT_INTERVAL:+--heartbeat-interval \"${QUICBAAS_HEARTBEAT_INTERVAL}\"}"]

LABEL org.opencontainers.image.title="QuicBaaS Agent Daemon" \
      org.opencontainers.image.description="Fleet node daemon for QuicBaaS sandbox-first execution" \
      org.opencontainers.image.vendor="QuicBaaS" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later AND Commercial"

# QuicBaaS Agent Daemon Container
#
# Multi-stage production image for the fleet node daemon.
#
# Base: Alpine 3.21 (lightweight, secure, ~7MB footprint)
#
# Design: The daemon spawns sandboxed sessions via the host's Docker socket
# mounted at /var/run/docker.sock. This avoids nested container complexity
# (Docker-in-Docker) and reuses the host's container runtime.
#
# For production deployment:
#   docker run -v /var/run/docker.sock:/var/run/docker.sock \
#     -v /etc/quicbaas/agent:/etc/quicbaas/agent:ro \
#     -e QUICBAAS_SERVER=https://quicbaas.example.com:8443 \
#     -e QUICBAAS_TENANT=acme \
#     quicbaas-agent:latest
#
# The daemon authenticates with an mTLS client certificate issued by the
# server's CA. Mount agent.crt / agent.key / server-ca.crt into the
# credentials directory (default /etc/quicbaas/agent) -- there is no
# bootstrap token.
#
# The /var/run/docker.sock mount gives the daemon permission to spawn
# containers on the host. Restrict this to trusted daemon instances only.

# Builder stage: compile the quicbaas binary
FROM golang:1.27.0-alpine AS builder

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

# ============================================================================
# Final image: Alpine + quicbaas binary + minimal tooling
# ============================================================================
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

# Expose the daemon's AgentRuntime gRPC service
# Default bind address is 127.0.0.1:9091; override with QUICBAAS_BIND_ADDR
EXPOSE 9091

# Health check: verify daemon is responding to enrollment ping
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD quicbaas --help > /dev/null 2>&1 || exit 1

# Entrypoint: start the daemon.
#
# Every flag below exists on `quicbaas fleet serve`; the drift test in
# cmd/quicbaas checks that against the live cobra command, because a CMD
# naming a removed flag makes the container exit at startup with
# "unknown flag".
#
# The command runs under /bin/sh for two reasons: JSON-form CMD is passed
# straight to execve, so a "${VAR}" in it reaches the daemon as that
# literal string rather than the operator's value; and ${VAR:-default} /
# ${VAR:+--flag} are shell expansions. `exec` hands PID 1 to the daemon so
# `docker stop` delivers SIGTERM to it and the graceful session drain runs.
#
# Environment variables supply config.
#
#   Required:
#     QUICBAAS_SERVER             https URL of the QuicBaaS server
#     QUICBAAS_TENANT             tenant this daemon serves
#
#   Optional, defaulted (see the expansions below):
#     QUICBAAS_BIND_ADDR          local listen addr (default 0.0.0.0:9091)
#     QUICBAAS_BACKENDS           enabled backends (default acp,ollama)
#     QUICBAAS_SANDBOX_POLICY     required|preferred|opt-in (default required)
#     QUICBAAS_CONTAINER_RUNTIME  docker|podman|auto|none (default auto)
#     QUICBAAS_CREDENTIALS_DIR    mTLS material dir (default /etc/quicbaas/agent)
#     QUICBAAS_WORK_DIR           workspace path (default /var/lib/quicbaas-agent)
#
#   Optional, flag omitted entirely when unset:
#     QUICBAAS_SANDBOX_NETWORK      container network for sandboxed sessions
#     QUICBAAS_SANDBOX_CAP_ADD      capabilities kept after --cap-drop ALL
#     QUICBAAS_SANDBOX_PIDS_LIMIT   process+thread bound per sandboxed container
#     QUICBAAS_SANDBOX_ULIMITS      name=soft[:hard] limits (default memlock 4 GiB)
#     QUICBAAS_ALLOWED_MOUNT_ROOTS  host dirs a sandboxed spawn may bind-mount
#     QUICBAAS_AUTO_PULL            pull missing images at startup
#     QUICBAAS_IMAGE_MAP            backend=image pairs
#     QUICBAAS_IMAGE_REGISTRY       registry prefix for unqualified images
#     QUICBAAS_MCP_ENDPOINT         MCP address spawned agents dial
#     QUICBAAS_HEARTBEAT_INTERVAL   duration (default 30s)
#
# Overriding the command therefore takes a shell string, e.g.
#   docker run quicbaas-agent:latest \
#     "exec /usr/local/bin/quicbaas fleet list --server $SERVER"
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

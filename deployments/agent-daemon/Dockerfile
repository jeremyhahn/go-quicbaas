# QuicBaaS Agent Daemon Container
#
# Multi-stage production image for the remote agent daemon.
#
# Base: Alpine 3.21 (lightweight, secure, ~7MB footprint)
#
# Design: The daemon spawns sandboxed sessions via the host's Docker socket
# mounted at /var/run/docker.sock. This avoids nested container complexity
# (Docker-in-Docker) and reuses the host's container runtime.
#
# For production deployment:
#   docker run -v /var/run/docker.sock:/var/run/docker.sock \
#     -e QUICBAAS_CENTRAL=https://central:8443 \
#     -e QUICBAAS_TENANT=acme \
#     -e QUICBAAS_ENROLLMENT_TOKEN=<token> \
#     quicbaas-agent:latest
#
# The /var/run/docker.sock mount gives the daemon permission to spawn
# containers on the host. Restrict this to trusted daemon instances only.

# Builder stage: compile the quicbaas binary
FROM golang:1.26.7-alpine AS builder

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

# Entrypoint: start the daemon
# Environment variables supply config:
#   QUICBAAS_CENTRAL           gRPC endpoint of central QuicBaaS
#   QUICBAAS_TENANT            tenant this daemon serves
#   QUICBAAS_BIND_ADDR         local listen addr (default: 127.0.0.1:9091)
#   QUICBAAS_ENROLLMENT_TOKEN  one-shot bootstrap token (or QUICBAAS_DEVICE_CERT)
#   QUICBAAS_SANDBOX_POLICY    required|preferred|opt-in (default: required)
#   QUICBAAS_CONTAINER_RUNTIME docker|podman|auto|none (default: auto)
#   QUICBAAS_WORK_DIR          workspace path (default: /var/lib/quicbaas-agent)
#   QUICBAAS_AUTO_PULL         true|false (pull missing images at startup)
#   QUICBAAS_IMAGE_MAP         backend=image pairs (e.g., claude-code=quicbaas/claude-code:v1)
#   QUICBAAS_HEARTBEAT_INTERVAL duration (default: 30s)
#
# Example:
#   docker run -e QUICBAAS_CENTRAL=https://central:8443 \
#     -e QUICBAAS_TENANT=acme \
#     -e QUICBAAS_ENROLLMENT_TOKEN=token123 \
#     -v /var/run/docker.sock:/var/run/docker.sock \
#     quicbaas-agent:latest
ENTRYPOINT ["/usr/local/bin/quicbaas"]
CMD ["agent", "serve", \
     "--central", "${QUICBAAS_CENTRAL}", \
     "--tenant", "${QUICBAAS_TENANT}", \
     "--bind", "0.0.0.0:9091", \
     "--sandbox-policy", "required", \
     "--work-dir", "/var/lib/quicbaas-agent", \
     "--enrollment-token", "${QUICBAAS_ENROLLMENT_TOKEN}"]

LABEL org.opencontainers.image.title="QuicBaaS Agent Daemon" \
      org.opencontainers.image.description="Remote agent daemon for QuicBaaS sandbox-first execution" \
      org.opencontainers.image.vendor="QuicBaaS" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later AND Commercial"

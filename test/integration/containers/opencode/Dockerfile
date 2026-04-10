# OpenCode Agent Container
# Multi-stage: Alpine 3.21 + Go 1.23 to install opencode binary, minimal Alpine runtime
# Non-root agent user, workspace at /workspace

FROM alpine:3.21 AS installer

# Install Go 1.23 and build dependencies for opencode
RUN apk add --no-cache \
    go \
    git \
    ca-certificates \
    curl \
    && go version

# Install opencode from source via go install
# Using the canonical Go install pattern for reproducible builds
RUN GOPATH=/opt/go GOBIN=/opt/go/bin go install github.com/opencode-ai/opencode@latest \
    && /opt/go/bin/opencode --version

# Runtime stage - minimal Alpine
FROM alpine:3.21

# Install only runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl

# Copy compiled opencode binary from installer stage
COPY --from=installer /opt/go/bin/opencode /usr/local/bin/opencode

# Create non-root agent user
RUN addgroup -g 1001 agent \
    && adduser -D -u 1001 -G agent -h /home/agent agent

# Create workspace and config directories with correct ownership
RUN mkdir -p /workspace /home/agent/.config/opencode \
    && chown -R agent:agent /workspace /home/agent

# Copy entrypoint script
COPY --chown=agent:agent entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Switch to non-root user
USER agent

WORKDIR /workspace

# Environment variables - secrets injected at runtime, never baked in
ENV OPENAI_API_KEY="" \
    OPENAI_BASE_URL="" \
    QUICBAAS_ADDRESS=""

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
    CMD opencode --version || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

LABEL org.opencontainers.image.title="QuicBaaS OpenCode Agent" \
      org.opencontainers.image.description="OpenCode AI agent container for QuicBaaS integration testing" \
      org.opencontainers.image.vendor="QuicBaaS" \
      org.opencontainers.image.licenses="Apache-2.0"

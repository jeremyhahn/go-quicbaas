# Claude Code Agent Container
# Alpine 3.21 + Node.js 20 LTS with @anthropic-ai/claude-code CLI
# Includes gopls (Go LSP) and Serena (semantic code MCP server)
# Non-root agent user, workspace at /workspace

# Build gopls binary in an isolated Go stage
FROM golang:1.26.6-alpine AS gopls-builder
RUN go install golang.org/x/tools/gopls@latest

FROM alpine:3.21 AS base

# Install Node.js 20 LTS, Python 3, and runtime dependencies
RUN apk add --no-cache \
    nodejs \
    npm \
    python3 \
    py3-pip \
    ca-certificates \
    tzdata \
    curl \
    git \
    bash \
    && node --version \
    && npm --version

# Install Claude Code CLI globally
RUN npm install -g @anthropic-ai/claude-code@latest \
    && claude --version

# Install Serena MCP server for semantic code navigation via LSP
RUN pip3 install --break-system-packages serena

# Copy gopls binary from builder stage
COPY --from=gopls-builder /go/bin/gopls /usr/local/bin/gopls

# Create non-root agent user
RUN addgroup -g 1001 agent \
    && adduser -D -u 1001 -G agent -h /home/agent agent

# Create workspace and config directories with correct ownership
RUN mkdir -p /workspace /home/agent/.claude \
    && chown -R agent:agent /workspace /home/agent

# Copy entrypoint script
COPY --chown=agent:agent entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Switch to non-root user
USER agent

WORKDIR /workspace

# Environment variables - secrets injected at runtime, never baked in
ENV ANTHROPIC_BASE_URL="" \
    ANTHROPIC_API_KEY="" \
    QUICBAAS_ADDRESS="" \
    NODE_ENV=production

# Verify claude CLI is accessible as non-root user
HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
    CMD node --version && claude --version || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

LABEL org.opencontainers.image.title="QuicBaaS Claude Code Agent" \
      org.opencontainers.image.description="Claude Code AI agent container for QuicBaaS integration testing" \
      org.opencontainers.image.vendor="QuicBaaS" \
      org.opencontainers.image.licenses="Apache-2.0"

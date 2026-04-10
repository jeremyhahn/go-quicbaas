# Codex Agent Container
# Alpine 3.21 + Node.js 20 LTS with @openai/codex CLI
# Non-root agent user, workspace at /workspace

FROM alpine:3.21 AS base

# Install Node.js 20 LTS and runtime dependencies
RUN apk add --no-cache \
    nodejs \
    npm \
    ca-certificates \
    tzdata \
    curl \
    && node --version \
    && npm --version

# Install Codex CLI globally
# Note: package name subject to change pending official OpenAI release
RUN npm install -g @openai/codex@latest \
    && codex --version

# Create non-root agent user
RUN addgroup -g 1001 agent \
    && adduser -D -u 1001 -G agent -h /home/agent agent

# Create workspace and config directories with correct ownership
RUN mkdir -p /workspace /home/agent/.codex \
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
    QUICBAAS_ADDRESS="" \
    NODE_ENV=production

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
    CMD node --version && codex --version || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

LABEL org.opencontainers.image.title="QuicBaaS Codex Agent" \
      org.opencontainers.image.description="OpenAI Codex AI agent container for QuicBaaS integration testing" \
      org.opencontainers.image.vendor="QuicBaaS" \
      org.opencontainers.image.licenses="Apache-2.0"

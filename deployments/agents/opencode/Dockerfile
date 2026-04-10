# OpenCode Agent Container
# Uses the official install script for the latest OpenCode with ACP support.
# Includes gopls for Go language server and Serena for semantic code navigation.
# Non-root agent user, workspace at /workspace

FROM alpine:3.21 AS installer

# Install dependencies for the install script and gopls build
RUN apk add --no-cache \
    go \
    git \
    ca-certificates \
    curl \
    bash \
    && go version

# Install OpenCode via official install script (gets latest with ACP support)
RUN curl -fsSL https://opencode.ai/install | bash \
    && /root/.opencode/bin/opencode -v

# gopls installed separately if needed at runtime

# Runtime stage - minimal Alpine
FROM alpine:3.21

# Install only runtime dependencies (includes Python 3 for Serena)
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    bash \
    python3 \
    py3-pip \
    ripgrep

# Install Serena MCP server for semantic code navigation via LSP
RUN pip3 install --break-system-packages serena

# Copy compiled binaries from installer stage
COPY --from=installer /root/.opencode/bin/opencode /usr/local/bin/opencode
# gopls is optional for LSP support

# Install the OpenAI-compatible AI SDK package required for Ollama provider.
# OpenCode dynamically loads this when opencode.json configures ollama.
RUN apk add --no-cache nodejs npm \
    && mkdir -p /opt/opencode-providers \
    && cd /opt/opencode-providers \
    && npm init -y > /dev/null 2>&1 \
    && npm install @ai-sdk/openai-compatible > /dev/null 2>&1

# Create non-root agent user
RUN addgroup -g 1001 agent \
    && adduser -D -u 1001 -G agent -h /home/agent agent

# Create workspace and config directories with correct ownership
RUN mkdir -p /workspace /home/agent/.config/opencode /home/agent/.opencode \
    && chown -R agent:agent /workspace /home/agent

# Make the AI SDK available globally via NODE_PATH
ENV NODE_PATH=/opt/opencode-providers/node_modules

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
    CMD opencode -v || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

LABEL org.opencontainers.image.title="QuicBaaS OpenCode Agent" \
      org.opencontainers.image.description="OpenCode AI agent with ACP support for QuicBaaS orchestration" \
      org.opencontainers.image.vendor="QuicBaaS" \
      org.opencontainers.image.licenses="Apache-2.0"

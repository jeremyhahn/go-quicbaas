#!/bin/sh
set -e

# OpenCode agent entrypoint
# Writes MCP server config and OpenCode settings from env vars, then execs the agent.

CONFIG_DIR="/home/agent/.config/opencode"
MCP_CONFIG="/workspace/.mcp.json"
OPENCODE_CONFIG="${CONFIG_DIR}/config.json"

# Validate required environment variables
if [ -z "${OPENAI_API_KEY}" ]; then
    echo "ERROR: OPENAI_API_KEY is required" >&2
    exit 1
fi

if [ -z "${QUICBAAS_ADDRESS}" ]; then
    echo "ERROR: QUICBAAS_ADDRESS is required" >&2
    exit 1
fi

echo "Configuring OpenCode agent..."
echo "  QuicBaaS address: ${QUICBAAS_ADDRESS}"
echo "  Base URL: ${OPENAI_BASE_URL:-https://api.openai.com}"

# Write MCP server configuration pointing to QuicBaaS
cat > "${MCP_CONFIG}" <<EOF
{
  "mcpServers": {
    "quicbaas": {
      "type": "http",
      "url": "${QUICBAAS_ADDRESS}/mcp",
      "headers": {
        "Content-Type": "application/json"
      }
    }
  }
}
EOF

# Write OpenCode agent configuration
cat > "${OPENCODE_CONFIG}" <<EOF
{
  "providers": {
    "openai": {
      "apiKey": "${OPENAI_API_KEY}",
      "baseUrl": "${OPENAI_BASE_URL:-https://api.openai.com}"
    }
  },
  "mcp": {
    "configPath": "/workspace/.mcp.json"
  }
}
EOF

echo "MCP config written to: ${MCP_CONFIG}"
echo "OpenCode config written to: ${OPENCODE_CONFIG}"
echo "Starting OpenCode agent..."

# Execute opencode with any arguments passed to the container
exec opencode "$@"

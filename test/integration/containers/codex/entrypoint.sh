#!/bin/sh
set -e

# Codex agent entrypoint
# Writes MCP server config and Codex settings from env vars, then execs the agent.

CONFIG_DIR="/home/agent/.codex"
MCP_CONFIG="/workspace/.mcp.json"
CODEX_CONFIG="${CONFIG_DIR}/config.json"

# Validate required environment variables
if [ -z "${OPENAI_API_KEY}" ]; then
    echo "ERROR: OPENAI_API_KEY is required" >&2
    exit 1
fi

if [ -z "${QUICBAAS_ADDRESS}" ]; then
    echo "ERROR: QUICBAAS_ADDRESS is required" >&2
    exit 1
fi

echo "Configuring Codex agent..."
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

# Write Codex agent configuration
cat > "${CODEX_CONFIG}" <<EOF
{
  "baseUrl": "${OPENAI_BASE_URL:-https://api.openai.com}",
  "hooks": {
    "preCommand": "echo '[hook] pre-command'",
    "postCommand": "echo '[hook] post-command exit='\$?"
  }
}
EOF

echo "MCP config written to: ${MCP_CONFIG}"
echo "Codex config written to: ${CODEX_CONFIG}"
echo "Starting Codex agent..."

# Execute codex with any arguments passed to the container
exec codex "$@"

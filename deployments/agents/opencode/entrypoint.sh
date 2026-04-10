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
# Use QUICBAAS_MCP_ADDRESS if set, otherwise derive from QUICBAAS_ADDRESS with port 11443
MCP_URL="${QUICBAAS_MCP_ADDRESS:-$(echo "${QUICBAAS_ADDRESS}" | sed 's|https://|http://|; s|:[0-9]*$||'):11443/mcp}"
echo "  MCP URL: ${MCP_URL}"

# Build MCP server configuration with QuicBaaS and optionally Serena
SERENA_BLOCK=""
if command -v serena >/dev/null 2>&1; then
    echo "  Serena MCP server: available"
    SERENA_BLOCK=$(cat <<'SERENA_EOF'
,
    "serena": {
      "type": "stdio",
      "command": "serena",
      "args": ["--workspace", "/workspace"]
    }
SERENA_EOF
)
else
    echo "  Serena MCP server: not available (skipped)"
fi

cat > "${MCP_CONFIG}" <<EOF
{
  "mcpServers": {
    "quicbaas": {
      "type": "http",
      "url": "${MCP_URL}",
      "headers": {
        "Content-Type": "application/json"
      }
    }${SERENA_BLOCK}
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

# Self-register with QuicBaaS fleet registry
AGENT_ID="$(hostname)-$(date +%s)"
TENANT_ID="${QUICBAAS_TENANT_ID:-default}"

if [ -n "${QUICBAAS_ADDRESS}" ]; then
    echo "Registering agent with fleet: ${AGENT_ID}"
    curl -sf -X POST "${QUICBAAS_ADDRESS}/api/v1/orchestrator/agents?tenant_id=${TENANT_ID}" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"${TENANT_ID}\",\"name\":\"$(hostname)\",\"host\":\"$(hostname -i 2>/dev/null || echo unknown)\",\"agent_type\":\"opencode\",\"backend\":\"acp\",\"work_dir\":\"/workspace\",\"id\":\"${AGENT_ID}\"}" \
        >/dev/null 2>&1 && echo "  Registered as ${AGENT_ID}" || echo "  Registration failed (non-fatal)"

    # Background heartbeat (every 30s)
    (while true; do
        sleep 30
        curl -sf -X POST "${QUICBAAS_ADDRESS}/api/v1/orchestrator/agents/${AGENT_ID}/heartbeat?tenant_id=${TENANT_ID}" \
            >/dev/null 2>&1
    done) &
    HEARTBEAT_PID=$!

    # Deregister on exit
    trap "curl -sf -X DELETE '${QUICBAAS_ADDRESS}/api/v1/orchestrator/agents/${AGENT_ID}?tenant_id=${TENANT_ID}' >/dev/null 2>&1; kill ${HEARTBEAT_PID} 2>/dev/null" EXIT
fi

# Execute opencode with any arguments passed to the container
exec opencode "$@"

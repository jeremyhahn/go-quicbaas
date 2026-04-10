#!/bin/sh
set -e

# Codex agent entrypoint
# Writes MCP server config and Codex settings from env vars, then execs the agent.

CONFIG_DIR="/home/agent/.codex"
MCP_CONFIG="/workspace/.mcp.json"
CODEX_CONFIG="${CONFIG_DIR}/config.json"

# Validate required environment variables
if [ -z "${OPENAI_API_KEY}" ]; then
    echo "INFO: OPENAI_API_KEY not set, using mounted credentials from ~/.codex"
fi

if [ -z "${QUICBAAS_ADDRESS}" ]; then
    echo "ERROR: QUICBAAS_ADDRESS is required" >&2
    exit 1
fi

echo "Configuring Codex agent..."
echo "  QuicBaaS address: ${QUICBAAS_ADDRESS}"
echo "  Base URL: ${OPENAI_BASE_URL:-https://api.openai.com}"

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
    }${SERENA_BLOCK}
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

# Self-register with QuicBaaS fleet registry
AGENT_ID="$(hostname)-$(date +%s)"
TENANT_ID="${QUICBAAS_TENANT_ID:-default}"

if [ -n "${QUICBAAS_ADDRESS}" ]; then
    echo "Registering agent with fleet: ${AGENT_ID}"
    curl -sf -X POST "${QUICBAAS_ADDRESS}/api/v1/orchestrator/agents?tenant_id=${TENANT_ID}" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"${TENANT_ID}\",\"name\":\"$(hostname)\",\"host\":\"$(hostname -i 2>/dev/null || echo unknown)\",\"agent_type\":\"codex\",\"backend\":\"acp\",\"work_dir\":\"/workspace\",\"id\":\"${AGENT_ID}\"}" \
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

# Execute codex with any arguments passed to the container
if [ "${CODEX_E2E_MODE}" = "true" ]; then
    echo "E2E mode enabled: running with --full-auto"
    exec codex --full-auto "$@"
else
    exec codex "$@"
fi

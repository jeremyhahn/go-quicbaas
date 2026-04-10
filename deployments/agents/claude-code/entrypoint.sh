#!/bin/sh
set -e

# Claude Code agent entrypoint
# Writes MCP server config and Claude settings from env vars, then execs the agent.

CONFIG_DIR="${HOME}/.claude"
MCP_CONFIG="/workspace/.mcp.json"
CLAUDE_SETTINGS="${CONFIG_DIR}/settings.json"

# Validate required environment variables
if [ -z "${ANTHROPIC_API_KEY}" ]; then
    echo "INFO: ANTHROPIC_API_KEY not set, using mounted OIDC credentials from ~/.claude"
fi

if [ -z "${QUICBAAS_ADDRESS}" ]; then
    echo "ERROR: QUICBAAS_ADDRESS is required" >&2
    exit 1
fi

echo "Configuring Claude Code agent..."
echo "  QuicBaaS address: ${QUICBAAS_ADDRESS}"
echo "  Base URL: ${ANTHROPIC_BASE_URL:-https://api.anthropic.com}"

# Ensure .claude.json exists (Claude Code requires it).
# Try HOME first, fall back to workspace if not writable.
CLAUDE_JSON="${HOME}/.claude.json"
if [ ! -f "${CLAUDE_JSON}" ]; then
    echo '{}' > "${CLAUDE_JSON}" 2>/dev/null || \
    echo '{}' > "/workspace/.claude.json" 2>/dev/null || true
fi

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

# Write Claude settings with hook configuration for task lifecycle (non-fatal if read-only mount)
cat > "${CLAUDE_SETTINGS}" <<EOF
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "echo '[hook] pre-tool: '\$CLAUDE_TOOL_NAME"
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "echo '[hook] post-tool: '\$CLAUDE_TOOL_NAME' exit='\$CLAUDE_TOOL_EXIT_CODE"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo '[hook] agent stopped'"
          }
        ]
      }
    ]
  }
}
EOF

echo "MCP config written to: ${MCP_CONFIG}"
echo "Claude settings written to: ${CLAUDE_SETTINGS}"
echo "Starting Claude Code agent..."

# Self-register with QuicBaaS fleet registry
AGENT_ID="$(hostname)-$(date +%s)"
TENANT_ID="${QUICBAAS_TENANT_ID:-default}"

if [ -n "${QUICBAAS_ADDRESS}" ]; then
    echo "Registering agent with fleet: ${AGENT_ID}"
    curl -sf -X POST "${QUICBAAS_ADDRESS}/api/v1/orchestrator/agents?tenant_id=${TENANT_ID}" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"${TENANT_ID}\",\"name\":\"$(hostname)\",\"host\":\"$(hostname -i 2>/dev/null || echo unknown)\",\"agent_type\":\"claude-code\",\"backend\":\"acp\",\"work_dir\":\"/workspace\",\"id\":\"${AGENT_ID}\"}" \
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

# Execute claude with any arguments passed to the container
if [ "${CLAUDE_E2E_MODE}" = "true" ]; then
    echo "E2E mode enabled: running with --dangerously-skip-permissions"
    exec claude --dangerously-skip-permissions "$@"
else
    exec claude "$@"
fi

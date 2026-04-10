#!/bin/sh
set -e

# Claude Code agent entrypoint
# Writes MCP server config and Claude settings from env vars, then execs the agent.

CONFIG_DIR="/home/agent/.claude"
MCP_CONFIG="/workspace/.mcp.json"
CLAUDE_SETTINGS="${CONFIG_DIR}/settings.json"

# Validate required environment variables
if [ -z "${ANTHROPIC_API_KEY}" ]; then
    echo "ERROR: ANTHROPIC_API_KEY is required" >&2
    exit 1
fi

if [ -z "${QUICBAAS_ADDRESS}" ]; then
    echo "ERROR: QUICBAAS_ADDRESS is required" >&2
    exit 1
fi

echo "Configuring Claude Code agent..."
echo "  QuicBaaS address: ${QUICBAAS_ADDRESS}"
echo "  Base URL: ${ANTHROPIC_BASE_URL:-https://api.anthropic.com}"

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

# Write Claude settings with hook configuration for task lifecycle
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

# Execute claude with any arguments passed to the container
exec claude "$@"

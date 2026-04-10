#!/usr/bin/env bash
# E2E Remote Agent Orchestration Test
#
# Deploys a Claude Code agent to a remote host via SSH, connects it to a local
# QuicBaaS server, and verifies the agent can execute tasks and report results.
#
# Usage:
#   ./scripts/e2e-remote-agent.sh [options]
#
# Environment:
#   REMOTE_HOST       Remote host (default: 198.51.100.52)
#   REMOTE_USER       Remote SSH user (default: $USER)
#   BRIDGE_IFACE      Network interface for QuicBaaS binding (default: trusted0)
#   WORKSPACE         Remote workspace to mount (default: /home/$USER/sources/go-recountly)
#   AGENT_PROMPT      Prompt to send to the agent
#   QUICBAAS_PORT     QuicBaaS server port (default: 8080)
#
# Prerequisites:
#   - Docker on local and remote hosts
#   - SSH key access to remote host
#   - ~/.claude credentials on remote host (OIDC authenticated)
#   - QuicBaaS Docker image built (make docker-build)
#   - Claude Code agent image built (deployments/agents/claude-code/)

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
REMOTE_HOST="${REMOTE_HOST:-198.51.100.52}"
REMOTE_USER="${REMOTE_USER:-$USER}"
BRIDGE_IFACE="${BRIDGE_IFACE:-trusted0}"
WORKSPACE="${WORKSPACE:-/home/${REMOTE_USER}/sources/go-recountly}"
QUICBAAS_PORT="${QUICBAAS_PORT:-8443}"
AGENT_PROMPT="${AGENT_PROMPT:-Run the unit tests in this project using 'make test' and report the results. Include the number of tests passed, failed, and any error output.}"

QUICBAAS_CONTAINER="quicbaas-e2e"
AGENT_CONTAINER="claude-code-e2e"
AGENT_IMAGE="quicbaas-claude-code-agent"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cleanup() {
    echo -e "${BLUE}Cleaning up...${NC}"
    docker rm -f "$QUICBAAS_CONTAINER" 2>/dev/null || true
    ssh "${REMOTE_USER}@${REMOTE_HOST}" "docker rm -f $AGENT_CONTAINER 2>/dev/null" || true
    echo -e "${GREEN}Cleanup complete${NC}"
}
trap cleanup EXIT

# Step 1: Detect local IP
echo -e "${BLUE}Detecting local IP on ${BRIDGE_IFACE}...${NC}"
LOCAL_IP=$(ip addr show "$BRIDGE_IFACE" 2>/dev/null | grep "inet " | awk '{print $2}' | cut -d/ -f1)
if [ -z "$LOCAL_IP" ]; then
    echo -e "${RED}ERROR: Could not detect IP on interface ${BRIDGE_IFACE}${NC}" >&2
    echo "Available interfaces:"
    ip -br addr show | grep UP
    exit 1
fi
echo -e "${GREEN}  Local IP: ${LOCAL_IP}${NC}"

# Step 2: Verify remote connectivity
echo -e "${BLUE}Verifying remote host ${REMOTE_HOST}...${NC}"
if ! ssh -o ConnectTimeout=5 "${REMOTE_USER}@${REMOTE_HOST}" "docker --version" >/dev/null 2>&1; then
    echo -e "${RED}ERROR: Cannot reach ${REMOTE_HOST} via SSH or Docker not installed${NC}" >&2
    exit 1
fi
echo -e "${GREEN}  Remote host ready: $(ssh ${REMOTE_USER}@${REMOTE_HOST} hostname)${NC}"

# Step 3: Verify credentials exist on remote
echo -e "${BLUE}Checking OIDC credentials on remote...${NC}"
if ! ssh "${REMOTE_USER}@${REMOTE_HOST}" "test -d ~/.claude"; then
    echo -e "${RED}ERROR: ~/.claude not found on ${REMOTE_HOST} - run 'claude login' on the remote first${NC}" >&2
    exit 1
fi
echo -e "${GREEN}  Credentials found on remote${NC}"

# Step 4: Build and transfer agent image
echo -e "${BLUE}Building Claude Code agent image...${NC}"
docker build -t "$AGENT_IMAGE" "${PROJECT_DIR}/deployments/agents/claude-code/" 2>&1 | tail -3

echo -e "${BLUE}Transferring agent image to ${REMOTE_HOST}...${NC}"
docker save "$AGENT_IMAGE" | ssh "${REMOTE_USER}@${REMOTE_HOST}" docker load 2>&1 | tail -1
echo -e "${GREEN}  Agent image ready on remote${NC}"

# Step 5: Start QuicBaaS server locally
echo -e "${BLUE}Starting QuicBaaS server on ${LOCAL_IP}:${QUICBAAS_PORT}...${NC}"
docker rm -f "$QUICBAAS_CONTAINER" 2>/dev/null || true

QUICBAAS_IMAGE=$(docker images --format '{{.Repository}}:{{.Tag}}' | grep -E '^quicbaas:' | head -1)
if [ -z "$QUICBAAS_IMAGE" ]; then
    echo -e "${RED}ERROR: No quicbaas Docker image found. Run 'make docker-build' first.${NC}" >&2
    exit 1
fi

docker run -d --name "$QUICBAAS_CONTAINER" --network host \
    -e QUICBAAS_ORCHESTRATION_ENABLED=true \
    -e QUICBAAS_SERVER_HOST=0.0.0.0 \
    -e QUICBAAS_SERVER_PORT="$QUICBAAS_PORT" \
    -e QUICBAAS_LOGGING_LEVEL=info \
    "$QUICBAAS_IMAGE" start >/dev/null

# Wait for QuicBaaS health
echo -e "${YELLOW}  Waiting for QuicBaaS to be ready...${NC}"
READY=false
for i in $(seq 1 30); do
    if curl -sk "https://${LOCAL_IP}:${QUICBAAS_PORT}/api/v1/health/live" >/dev/null 2>&1; then
        READY=true
        break
    fi
    sleep 2
done

if [ "$READY" != "true" ]; then
    echo -e "${RED}ERROR: QuicBaaS server did not become ready${NC}" >&2
    docker logs "$QUICBAAS_CONTAINER" 2>&1 | tail -20
    exit 1
fi
echo -e "${GREEN}  QuicBaaS server ready at https://${LOCAL_IP}:${QUICBAAS_PORT}${NC}"

# Step 6: Check orchestrator health
echo -e "${BLUE}Checking orchestrator health...${NC}"
ORCH_HEALTH=$(curl -sk "https://${LOCAL_IP}:${QUICBAAS_PORT}/api/v1/orchestrator/health" 2>&1)
if echo "$ORCH_HEALTH" | grep -q "status"; then
    echo -e "${GREEN}  Orchestrator is healthy${NC}"
else
    echo -e "${YELLOW}  WARNING: Orchestrator health check returned: ${ORCH_HEALTH}${NC}"
fi

# Step 7: Run Claude Code agent on remote
echo -e "${BLUE}Spawning Claude Code agent on ${REMOTE_HOST}...${NC}"
echo -e "${YELLOW}  Workspace: ${WORKSPACE}${NC}"
echo -e "${YELLOW}  QuicBaaS:  https://${LOCAL_IP}:${QUICBAAS_PORT}${NC}"
echo -e "${YELLOW}  Prompt:    ${AGENT_PROMPT}${NC}"
echo ""
echo -e "${BLUE}=== Agent Output ===${NC}"

REMOTE_UID=$(ssh "${REMOTE_USER}@${REMOTE_HOST}" id -u)
ssh -t "${REMOTE_USER}@${REMOTE_HOST}" docker run --rm \
    --name "$AGENT_CONTAINER" \
    --user "${REMOTE_UID}:${REMOTE_UID}" \
    -v "/home/${REMOTE_USER}/.claude:/home/${REMOTE_USER}/.claude" \
    -v "/home/${REMOTE_USER}/.claude.json:/home/${REMOTE_USER}/.claude.json" \
    -v "/home/${REMOTE_USER}/.cache/claude:/home/${REMOTE_USER}/.cache/claude" \
    -v "/home/${REMOTE_USER}/.local/share/claude:/home/${REMOTE_USER}/.local/share/claude" \
    -v "/home/${REMOTE_USER}/.config/claude:/home/${REMOTE_USER}/.config/claude" \
    -v "${WORKSPACE}:/workspace" \
    -e HOME="/home/${REMOTE_USER}" \
    -e "QUICBAAS_ADDRESS=https://${LOCAL_IP}:${QUICBAAS_PORT}" \
    -e "QUICBAAS_MCP_ADDRESS=http://${LOCAL_IP}:11443/mcp" \
    -e "CLAUDE_E2E_MODE=true" \
    -w /workspace \
    "$AGENT_IMAGE" \
    --print "$AGENT_PROMPT"

AGENT_EXIT=$?
echo ""
echo -e "${BLUE}=== End Agent Output ===${NC}"

if [ $AGENT_EXIT -eq 0 ]; then
    echo -e "${GREEN}Agent completed successfully (exit code 0)${NC}"
else
    echo -e "${RED}Agent failed (exit code ${AGENT_EXIT})${NC}"
fi

# Step 8: Show QuicBaaS logs
echo ""
echo -e "${BLUE}QuicBaaS server logs (last 20 lines):${NC}"
docker logs "$QUICBAAS_CONTAINER" 2>&1 | tail -20

exit $AGENT_EXIT

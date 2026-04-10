#!/bin/bash
# Tic-Tac-Toe Orchestration Example
#
# Fully automated: starts the server, runs the workflow, monitors progress,
# and cleans up on exit. Supports GPU profile and model family selection.
#
# Prerequisites:
#   - make build (from repo root)
#   - ollama running (model auto-pulled if missing)
#   - opencode installed (for the coder agent)
#
# Usage:
#   ./run.sh                                              # defaults: gpu-lite + llama
#   ./run.sh --gpu-profile cpu                            # CPU only, tinyllama
#   ./run.sh --gpu-profile gpu-power --model-family qwen  # RTX 4090 / A6000
#   ./run.sh --gpu-profile gpu-max --model-family deepseek # RTX 6000 Pro

set -euo pipefail

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
GPU_PROFILE="gpu-lite"
MODEL_FAMILY="llama"

# ---------------------------------------------------------------------------
# Usage
# ---------------------------------------------------------------------------
usage() {
  cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Run the tic-tac-toe orchestration example with configurable GPU profiles.

Options:
  --gpu-profile <cpu|gpu-lite|gpu-power|gpu-max>  GPU tier (default: gpu-lite)
  --model-family <llama|qwen|deepseek>            Model family (default: llama)
  --help                                          Show this help message

GPU Profiles:
  cpu        0 GB VRAM  - tinyllama (all families)
  gpu-lite   ~8 GB VRAM - llama3 / qwen2.5-coder:7b / deepseek-coder-v2:16b
  gpu-power  ~48 GB VRAM - llama3.3:70b / qwen2.5-coder:72b / deepseek-coder-v2:236b
  gpu-max    ~90 GB VRAM - same models as gpu-power, higher limits

Examples:
  $(basename "$0")                                              # gpu-lite + llama
  $(basename "$0") --gpu-profile cpu                            # CPU only
  $(basename "$0") --gpu-profile gpu-power --model-family qwen  # RTX 4090 / A6000
  $(basename "$0") --gpu-profile gpu-max --model-family deepseek # RTX 6000 Pro
EOF
  exit 0
}

# ---------------------------------------------------------------------------
# Parse flags
# ---------------------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --gpu-profile)
      GPU_PROFILE="$2"
      shift 2
      ;;
    --model-family)
      MODEL_FAMILY="$2"
      shift 2
      ;;
    --help)
      usage
      ;;
    *)
      echo "ERROR: Unknown option: $1"
      echo "Run '$(basename "$0") --help' for usage."
      exit 1
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Model matrix: tier:family -> ollama model
# ---------------------------------------------------------------------------
declare -A MODEL_MATRIX
MODEL_MATRIX["cpu:llama"]="tinyllama"
MODEL_MATRIX["cpu:qwen"]="tinyllama"
MODEL_MATRIX["cpu:deepseek"]="tinyllama"
MODEL_MATRIX["gpu-lite:llama"]="llama3"
MODEL_MATRIX["gpu-lite:qwen"]="qwen2.5-coder:7b"
MODEL_MATRIX["gpu-lite:deepseek"]="deepseek-coder-v2:16b"
MODEL_MATRIX["gpu-power:llama"]="llama3.3:70b"
MODEL_MATRIX["gpu-power:qwen"]="qwen2.5-coder:72b"
MODEL_MATRIX["gpu-power:deepseek"]="deepseek-r1:70b"
MODEL_MATRIX["gpu-max:llama"]="llama3.3:70b"
MODEL_MATRIX["gpu-max:qwen"]="qwen2.5-coder:72b"
MODEL_MATRIX["gpu-max:deepseek"]="deepseek-r1:70b"

# ---------------------------------------------------------------------------
# VRAM estimates per tier
# ---------------------------------------------------------------------------
declare -A VRAM_ESTIMATE
VRAM_ESTIMATE["cpu"]="0 GB (CPU only)"
VRAM_ESTIMATE["gpu-lite"]="~8 GB"
VRAM_ESTIMATE["gpu-power"]="~48 GB"
VRAM_ESTIMATE["gpu-max"]="~90 GB"

# ---------------------------------------------------------------------------
# Settings per tier: max_iterations and total_timeout
# ---------------------------------------------------------------------------
declare -A TIER_ITERATIONS
TIER_ITERATIONS["cpu"]=5
TIER_ITERATIONS["gpu-lite"]=5
TIER_ITERATIONS["gpu-power"]=5
TIER_ITERATIONS["gpu-max"]=10

declare -A TIER_TIMEOUT
TIER_TIMEOUT["cpu"]="30m"
TIER_TIMEOUT["gpu-lite"]="30m"
TIER_TIMEOUT["gpu-power"]="45m"
TIER_TIMEOUT["gpu-max"]="60m"

# ---------------------------------------------------------------------------
# Profile descriptions per tier
# ---------------------------------------------------------------------------
declare -A PROFILE_DESC
PROFILE_DESC["cpu"]="CPU-only inference"
PROFILE_DESC["gpu-lite"]="lightweight GPU (~8 GB VRAM)"
PROFILE_DESC["gpu-power"]="high-performance GPU (~48 GB VRAM)"
PROFILE_DESC["gpu-max"]="maximum GPU (~90 GB VRAM, extended limits)"

# ---------------------------------------------------------------------------
# Validate inputs
# ---------------------------------------------------------------------------
MATRIX_KEY="${GPU_PROFILE}:${MODEL_FAMILY}"

if [ -z "${MODEL_MATRIX[$MATRIX_KEY]+x}" ]; then
  echo "ERROR: Invalid combination: --gpu-profile '$GPU_PROFILE' --model-family '$MODEL_FAMILY'"
  echo "Valid profiles: cpu, gpu-lite, gpu-power, gpu-max"
  echo "Valid families: llama, qwen, deepseek"
  exit 1
fi

OLLAMA_MODEL="${MODEL_MATRIX[$MATRIX_KEY]}"
MAX_ITERATIONS="${TIER_ITERATIONS[$GPU_PROFILE]}"
TOTAL_TIMEOUT="${TIER_TIMEOUT[$GPU_PROFILE]}"
VRAM="${VRAM_ESTIMATE[$GPU_PROFILE]}"
DESCRIPTION="${PROFILE_DESC[$GPU_PROFILE]}"

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
QUICBAAS="$REPO_ROOT/bin/quicbaas"
QUICBAASD="$REPO_ROOT/bin/quicbaasd"
DATA_DIR="$SCRIPT_DIR/data"
SOCKET="$SCRIPT_DIR/quicbaas.sock"
TEMPLATE="$SCRIPT_DIR/workflow-template.yaml"
GENERATED_WORKFLOW="$REPO_ROOT/workflows/tictactoe-${GPU_PROFILE}.yaml"
WORKSPACE_PATH="$SCRIPT_DIR/output"
SERVER_PID=""

# ---------------------------------------------------------------------------
# Verify binaries exist
# ---------------------------------------------------------------------------
for bin in "$QUICBAAS" "$QUICBAASD"; do
  if [ ! -x "$bin" ]; then
    echo "ERROR: $(basename "$bin") not found at $bin"
    echo "Run 'make build' from the repo root first."
    exit 1
  fi
done

if [ ! -f "$TEMPLATE" ]; then
  echo "ERROR: Workflow template not found at $TEMPLATE"
  exit 1
fi

# ---------------------------------------------------------------------------
# Cleanup: kill server, remove generated files on exit
# ---------------------------------------------------------------------------
cleanup() {
  echo ""
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "Stopping server (PID $SERVER_PID)..."
    kill "$SERVER_PID" 2>/dev/null
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  # Kill any orphaned claude/codex processes spawned by the server
  pkill -P "$SERVER_PID" 2>/dev/null || true
  rm -f "$SOCKET" "$SCRIPT_DIR/config.yaml" "$GENERATED_WORKFLOW"
  echo "Cleanup complete."
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Helper: all client calls go through the local unix socket
# ---------------------------------------------------------------------------
qb() {
  $QUICBAAS --address "$SOCKET" --transport grpc --data-dir "$DATA_DIR" "$@"
}

# ---------------------------------------------------------------------------
# Profile summary
# ---------------------------------------------------------------------------
echo "=== Tic-Tac-Toe Orchestration Example ==="
echo ""
echo "Profile:      $GPU_PROFILE"
echo "Model family: $MODEL_FAMILY"
echo "Ollama model: $OLLAMA_MODEL"
echo "VRAM:         $VRAM"
echo "Coder:        claude (persistent session)"
echo "Reviewer:     codex (one-shot)"
echo "Workspace:    $WORKSPACE_PATH"
echo "Iterations:   $MAX_ITERATIONS"
echo "Timeout:      $TOTAL_TIMEOUT"
echo ""

# ---------------------------------------------------------------------------
# GPU detection
# ---------------------------------------------------------------------------
if [ "$GPU_PROFILE" != "cpu" ]; then
  if command -v nvidia-smi > /dev/null 2>&1 && nvidia-smi > /dev/null 2>&1; then
    echo "GPU detected:"
    nvidia-smi --query-gpu=name,memory.total --format=csv,noheader 2>/dev/null || true
    echo ""
  else
    echo "WARNING: nvidia-smi not available. GPU tier '$GPU_PROFILE' selected but no GPU detected."
    echo "         Ollama will fall back to CPU inference (significantly slower)."
    echo ""
  fi
fi

# ---------------------------------------------------------------------------
# Model auto-pull: ensure the ollama model is available
# ---------------------------------------------------------------------------
echo "Checking for ollama model '$OLLAMA_MODEL'..."
if ollama list 2>/dev/null | grep -q "^${OLLAMA_MODEL}"; then
  echo "Model '$OLLAMA_MODEL' is available."
else
  echo "Model '$OLLAMA_MODEL' not found locally. Pulling..."
  PULL_RETRIES=3
  PULL_OK=0
  for attempt in $(seq 1 $PULL_RETRIES); do
    if ollama pull "$OLLAMA_MODEL"; then
      PULL_OK=1
      break
    fi
    echo "Pull attempt $attempt/$PULL_RETRIES failed. Retrying in 5s..."
    sleep 5
  done
  if [ "$PULL_OK" -ne 1 ]; then
    echo "ERROR: Failed to pull '$OLLAMA_MODEL' after $PULL_RETRIES attempts."
    exit 1
  fi
  echo "Model '$OLLAMA_MODEL' pulled successfully."
fi
echo ""

# ---------------------------------------------------------------------------
# Generate workflow from template
# ---------------------------------------------------------------------------
echo "Generating workflow from template..."
mkdir -p "$REPO_ROOT/workflows" "$WORKSPACE_PATH"
sed \
  -e "s|{{OLLAMA_MODEL}}|${OLLAMA_MODEL}|g" \
  -e "s|{{PROFILE}}|${GPU_PROFILE}|g" \
  -e "s|{{PROFILE_DESC}}|${DESCRIPTION}|g" \
  -e "s|{{MAX_ITERATIONS}}|${MAX_ITERATIONS}|g" \
  -e "s|{{TOTAL_TIMEOUT}}|${TOTAL_TIMEOUT}|g" \
  -e "s|{{WORKSPACE_PATH}}|${WORKSPACE_PATH}|g" \
  "$TEMPLATE" > "$GENERATED_WORKFLOW"

echo "Generated workflow: $GENERATED_WORKFLOW"
echo ""
echo "--- Workflow contents ---"
cat "$GENERATED_WORKFLOW"
echo "--- End workflow ---"
echo ""

# ---------------------------------------------------------------------------
# Start the server
# ---------------------------------------------------------------------------
echo "Starting quicbaasd..."
mkdir -p "$DATA_DIR"
rm -f "$SOCKET"

# Create minimal config that disables network listeners (Unix socket only)
cat > "$SCRIPT_DIR/config.yaml" << EOF
server:
  quic:
    enabled: false
  mcp:
    enabled: false
  mqtt:
    enabled: false
  amqp:
    enabled: false
  stomp:
    enabled: false
EOF

# Start server from repo root so it finds the workflows/ directory.
cd "$REPO_ROOT"
$QUICBAASD start \
  --data-dir "$DATA_DIR" \
  --unix-socket "$SOCKET" \
  --grpc-port 0 \
  --http-port 0 \
  --config "$SCRIPT_DIR/config.yaml" \
  > "$SCRIPT_DIR/server.log" 2>&1 &
SERVER_PID=$!

echo "Server PID: $SERVER_PID"
echo "Server log: $SCRIPT_DIR/server.log"

# Wait for server to be ready
echo "Waiting for server to be ready..."
WAIT=0
MAX_WAIT=30
while [ $WAIT -lt $MAX_WAIT ]; do
  if qb orchestrator health > /dev/null 2>&1; then
    echo "Server ready after ${WAIT}s"
    break
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "ERROR: Server exited unexpectedly. Check $SCRIPT_DIR/server.log"
    tail -20 "$SCRIPT_DIR/server.log"
    exit 1
  fi
  sleep 1
  WAIT=$((WAIT + 1))
done

if [ $WAIT -ge $MAX_WAIT ]; then
  echo "ERROR: Server failed to start within ${MAX_WAIT}s"
  echo "Last 20 lines of server log:"
  tail -20 "$SCRIPT_DIR/server.log"
  exit 1
fi

echo ""

# Show available backends
echo "Available backends:"
qb orchestrator backends
echo ""

# Load workflow
echo "Loading workflow..."
WORKFLOW_NAME="tictactoe-${GPU_PROFILE}"
cd "$REPO_ROOT"
qb orchestrator workflow show "$WORKFLOW_NAME"
echo ""

# ---------------------------------------------------------------------------
# Start the orchestration job
# ---------------------------------------------------------------------------
echo "Starting orchestration job..."
echo "Press Ctrl+C to cancel and stop server"
echo ""

# The CLI handles event streaming and display via --verbosity.
qb --timeout 45m orchestrator run \
  "Create a tic-tac-toe game in JavaScript with HTML/CSS frontend, game logic, win detection, and a simple AI opponent" \
  --workflow "$WORKFLOW_NAME" \
  --workflow-file "$GENERATED_WORKFLOW" \
  --backend ollama \
  --model "$OLLAMA_MODEL" \
  --project tictactoe \
  --poll-interval 5s \
  --verbosity debug || true

echo ""
echo "=== Job finished ==="
echo ""

# Show full audit trail
echo "=== Orchestration Audit Trail ==="
grep "orchestration: \[" "$SCRIPT_DIR/server.log" 2>/dev/null | sed 's/.*orchestration: \[/  [/' | sed 's/","component.*//'
echo ""

# Show errors/warnings
ERRORS=$(grep "WARN" "$SCRIPT_DIR/server.log" 2>/dev/null | grep -v "TLS.*SNI" | sed 's/.*"msg":"/  /' | sed 's/","component.*//')
if [ -n "$ERRORS" ]; then
  echo "=== Warnings ==="
  echo "$ERRORS"
  echo ""
fi

# Show generated files
if [ -d "$WORKSPACE_PATH" ] && [ "$(ls -A "$WORKSPACE_PATH" 2>/dev/null)" ]; then
  echo "=== Generated files in $WORKSPACE_PATH ==="
  find "$WORKSPACE_PATH" -type f -printf "  %p (%s bytes)\n" | head -20
else
  echo "No output files generated in $WORKSPACE_PATH"
fi

#!/bin/bash
# Calculator Orchestration Example
#
# Fully local: uses Ollama for all LLM inference (lead decomposition + agent coding/review)
# via OpenCode agents pointing at the local Ollama server. Zero API costs.
#
# Prerequisites:
#   - make build (from repo root)
#   - ollama running (model auto-pulled if missing)
#   - opencode installed (go install github.com/opencode-ai/opencode@latest)
#
# Usage:
#   ./run.sh                                              # defaults: gpu-lite + llama
#   ./run.sh --gpu-profile cpu                            # CPU only, tinyllama
#   ./run.sh --gpu-profile gpu-power --model-family qwen  # RTX 4090 / A6000
#   ./run.sh --gpu-profile gpu-max --model-family deepseek # RTX 6000 Pro

set -euo pipefail

# ---------------------------------------------------------------------------
# Directories and paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
QUICBAAS="$REPO_ROOT/bin/quicbaas"
QUICBAASD="$REPO_ROOT/bin/quicbaasd"
DATA_DIR="$SCRIPT_DIR/data"
SOCKET="$SCRIPT_DIR/quicbaas.sock"
TEMPLATE="$SCRIPT_DIR/workflow-template.yaml"
WORKSPACE_PATH="$SCRIPT_DIR/output"
SERVER_PID=""

# ---------------------------------------------------------------------------
# Default flags
# ---------------------------------------------------------------------------
GPU_PROFILE="gpu-lite"
MODEL_FAMILY="llama"

# ---------------------------------------------------------------------------
# Parse command-line flags
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
    --help|-h)
      echo "Usage: $0 [--gpu-profile <cpu|gpu-lite|gpu-power|gpu-max>] [--model-family <llama|qwen|deepseek>]"
      echo ""
      echo "GPU Profiles:"
      echo "  cpu        - CPU-only inference (tinyllama, ~0 GB VRAM)"
      echo "  gpu-lite   - Lightweight GPU models (~8 GB VRAM)"
      echo "  gpu-power  - High-performance GPU models (~48 GB VRAM)"
      echo "  gpu-max    - Maximum GPU models with extended limits (~90 GB VRAM)"
      echo ""
      echo "Model Families: llama, qwen, deepseek"
      exit 0
      ;;
    *)
      echo "Unknown flag: $1 (use --help for usage)"
      exit 1
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Model matrix: tier:family -> ollama model
# ---------------------------------------------------------------------------
declare -A MODEL_MATRIX
# IMPORTANT: All models must support tool calling.
# Model selection per GPU tier:
#   cpu       — qwen3:1.7b (smallest reliable tool-calling model, ~2 GB)
#   gpu-lite  — qwen3:1.7b (fast iteration, ~2 GB VRAM)
#   gpu-power — llama3.3:70b (high quality, ~48 GB VRAM)
#   gpu-max   — llama3.3:70b (same model, extended limits)
MODEL_MATRIX["cpu:llama"]="qwen3:1.7b"
MODEL_MATRIX["cpu:qwen"]="qwen3:1.7b"
MODEL_MATRIX["cpu:deepseek"]="qwen3:1.7b"
MODEL_MATRIX["gpu-lite:llama"]="qwen3:1.7b"
MODEL_MATRIX["gpu-lite:qwen"]="qwen3:1.7b"
MODEL_MATRIX["gpu-lite:deepseek"]="qwen3:1.7b"
MODEL_MATRIX["gpu-power:llama"]="llama3.3:70b"
MODEL_MATRIX["gpu-power:qwen"]="qwen2.5:32b"
MODEL_MATRIX["gpu-power:deepseek"]="llama3.3:70b"
MODEL_MATRIX["gpu-max:llama"]="llama3.3:70b"
MODEL_MATRIX["gpu-max:qwen"]="qwen2.5:72b"
MODEL_MATRIX["gpu-max:deepseek"]="llama3.3:70b"

declare -A VRAM_ESTIMATE
VRAM_ESTIMATE["cpu"]="~2 GB"
VRAM_ESTIMATE["gpu-lite"]="~2 GB"
VRAM_ESTIMATE["gpu-power"]="~48 GB"
VRAM_ESTIMATE["gpu-max"]="~90 GB"

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
  exit 1
fi

OLLAMA_MODEL="${MODEL_MATRIX[$MATRIX_KEY]}"
MAX_ITERATIONS="${TIER_ITERATIONS[$GPU_PROFILE]}"
TOTAL_TIMEOUT="${TIER_TIMEOUT[$GPU_PROFILE]}"
DESCRIPTION="${PROFILE_DESC[$GPU_PROFILE]}"
GENERATED_WORKFLOW="$REPO_ROOT/workflows/calculator-${GPU_PROFILE}.yaml"

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

# Ensure opencode is in PATH (installed via curl -fsSL https://opencode.ai/install | bash)
if [ -d "$HOME/.opencode/bin" ]; then
  export PATH="$HOME/.opencode/bin:$PATH"
fi
if ! command -v opencode > /dev/null 2>&1; then
  echo "ERROR: opencode not found. Install with: curl -fsSL https://opencode.ai/install | bash"
  exit 1
fi

# ---------------------------------------------------------------------------
# Cleanup handler
# ---------------------------------------------------------------------------
cleanup() {
  echo ""
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "Stopping server (PID $SERVER_PID)..."
    kill "$SERVER_PID" 2>/dev/null
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  pkill -P "$SERVER_PID" 2>/dev/null || true
  rm -f "$SOCKET" "$SCRIPT_DIR/config.yaml" "$GENERATED_WORKFLOW"
  echo "Cleanup complete."
}
trap cleanup EXIT INT TERM

# Helper: all client calls go through the local unix socket
qb() {
  $QUICBAAS --address "$SOCKET" --transport grpc --data-dir "$DATA_DIR" "$@"
}

# ---------------------------------------------------------------------------
# Print profile summary
# ---------------------------------------------------------------------------
echo "=== Calculator Orchestration Example ==="
echo ""
echo "Profile:      $GPU_PROFILE"
echo "Model family: $MODEL_FAMILY"
echo "Ollama model: $OLLAMA_MODEL"
echo "VRAM:         ${VRAM_ESTIMATE[$GPU_PROFILE]}"
echo "Lead:         opencode/ollama/$OLLAMA_MODEL (persistent session)"
echo "Coder:        opencode/ollama/$OLLAMA_MODEL (persistent session)"
echo "Reviewer:     opencode/ollama/$OLLAMA_MODEL (persistent session)"
echo "Workspace:    $WORKSPACE_PATH"
echo "Iterations:   $MAX_ITERATIONS"
echo "Timeout:      $TOTAL_TIMEOUT"
echo ""

# ---------------------------------------------------------------------------
# GPU detection
# ---------------------------------------------------------------------------
if [ "$GPU_PROFILE" != "cpu" ]; then
  if command -v nvidia-smi > /dev/null 2>&1; then
    echo "GPU detected:"
    nvidia-smi --query-gpu=name,memory.total --format=csv,noheader
    echo ""
  else
    echo "WARNING: No GPU detected. Inference will run on CPU (slow for large models)."
    echo ""
  fi
fi

# ---------------------------------------------------------------------------
# Model auto-pull
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

# Create a custom model variant with 32k context for proper tool calling.
# Ollama defaults to 4096 context which prevents tool execution.
AGENT_MODEL="${OLLAMA_MODEL}-32k"
if ! ollama list 2>/dev/null | grep -q "^${AGENT_MODEL}"; then
  echo "Creating custom model '$AGENT_MODEL' with 32k context..."
  MODELFILE=$(mktemp)
  echo "FROM ${OLLAMA_MODEL}" > "$MODELFILE"
  echo "PARAMETER num_ctx 32768" >> "$MODELFILE"
  ollama create "$AGENT_MODEL" -f "$MODELFILE" 2>&1 | tail -1
  rm -f "$MODELFILE"
  echo "Custom model '$AGENT_MODEL' created."
else
  echo "Custom model '$AGENT_MODEL' already exists."
fi
echo ""

# ---------------------------------------------------------------------------
# Generate workflow from template
# ---------------------------------------------------------------------------
echo "Generating workflow from template..."
mkdir -p "$REPO_ROOT/workflows" "$WORKSPACE_PATH"

# Create opencode.json in workspace so OpenCode uses Ollama as its LLM provider.
cat > "$WORKSPACE_PATH/opencode.json" << OCEOF
{
  "\$schema": "https://opencode.ai/config.json",
  "permission": "allow",
  "model": "ollama/${AGENT_MODEL}",
  "provider": {
    "ollama": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Ollama (local)",
      "options": {
        "baseURL": "http://localhost:11434/v1"
      },
      "models": {
        "${AGENT_MODEL}": {
          "name": "${AGENT_MODEL}",
          "options": {
            "num_ctx": 32768
          }
        }
      }
    }
  }
}
OCEOF
echo "Created opencode.json in workspace for Ollama provider"

# Install the AI SDK package in the workspace so containers can find it.
# OpenCode dynamically loads @ai-sdk/openai-compatible from the project's node_modules.
if [ ! -d "$WORKSPACE_PATH/node_modules/@ai-sdk" ]; then
  echo "Installing @ai-sdk/openai-compatible in workspace..."
  (cd "$WORKSPACE_PATH" && npm init -y > /dev/null 2>&1 && npm install @ai-sdk/openai-compatible > /dev/null 2>&1)
  echo "AI SDK installed."
fi
sed \
  -e "s|{{OLLAMA_MODEL}}|${AGENT_MODEL}|g" \
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
WORKFLOW_NAME="calculator-${GPU_PROFILE}"
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
# No bash log tailing needed — the Go program does it all.
qb --timeout 45m orchestrator run \
  "Create a simple calculator application in vanilla HTML5/JavaScript with add, subtract, multiply, divide operations, proper error handling for division by zero, etc. Include comprehensive unit tests with code coverage that exceeds 75%." \
  --workflow "$WORKFLOW_NAME" \
  --workflow-file "$GENERATED_WORKFLOW" \
  --backend opencode \
  --model "ollama/$AGENT_MODEL" \
  --project calculator \
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

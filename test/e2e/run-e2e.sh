#!/usr/bin/env bash
# Dockerized Playwright e2e entrypoint.
#
# Boots quicbaasd in the background, waits for first-boot readiness, then runs
# the requested Playwright project against it — the same flow as the host
# `make test-e2e`, but self-contained in one container so the CA cert and the
# first-boot invite log are local to both the daemon and the test runner.
#
# Environment (set by the image / compose / Make):
#   E2E_PROJECT     Playwright project: chromium-no-backend | chromium-acp
#   E2E_BACKEND     embedded-agent backend: ollama | opencode
#   E2E_OLLAMA_URL  ollama endpoint for the backend
#   E2E_EXTRA_ARGS  optional extra args appended to the daemon start command
set -uo pipefail

DATA_DIR=/tmp/quicbaas-e2e-data
SOCK=/tmp/quicbaas-e2e.sock
LOG=/tmp/quicbaasd-e2e.log
PORT=18080
PROJECT="${E2E_PROJECT:-chromium-no-backend}"
BACKEND="${E2E_BACKEND:-ollama}"
OLLAMA_URL="${E2E_OLLAMA_URL:-http://ollama:11434}"

rm -rf "$DATA_DIR" && mkdir -p "$DATA_DIR"
rm -f "$SOCK" "$LOG"

# For the opencode backend, write an opencode config that routes the model
# through the remote ollama endpoint (OpenAI-compatible API at /v1). The model
# the session requests (QUICBAAS_E2E_ACP_MODEL, e.g. ollama/qwen3:1.7b) selects
# this provider. Written at runtime so the ollama URL is dynamic.
if [ "$BACKEND" = "opencode" ]; then
  OLLAMA_MODEL="${QUICBAAS_E2E_ACP_MODEL_BARE:-qwen3:1.7b}"
  mkdir -p /root/.config/opencode
  cat > /root/.config/opencode/opencode.json <<JSON
{
  "\$schema": "https://opencode.ai/config.json",
  "provider": {
    "ollama": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Ollama (remote GPU)",
      "options": { "baseURL": "${OLLAMA_URL%/}/v1" },
      "models": { "${OLLAMA_MODEL}": { "name": "${OLLAMA_MODEL}" } }
    }
  }
}
JSON
  echo "[run-e2e] wrote opencode ollama config (model=${OLLAMA_MODEL} baseURL=${OLLAMA_URL%/}/v1)"
fi

# When QUICBAAS_E2E_SANDBOX=1, run the embedded agent in container-sandbox mode:
# each session is spawned as a SIBLING container (via the mounted host docker
# socket) running the prebuilt sandbox agent image. The daemon resolves the
# image from --embedded-agent-sandbox-image (QUICBAAS_E2E_SANDBOX_IMAGE) and the
# policy to "required" so an explicit sandbox_mode=1 is honored. The image is
# built locally on the host docker (never pushed) so auto-pull stays OFF.
SANDBOX_ARGS=""
if [ "${QUICBAAS_E2E_SANDBOX:-0}" = "1" ]; then
  SANDBOX_IMAGE="${QUICBAAS_E2E_SANDBOX_IMAGE:-quicbaas-e2e-sandbox-agent:latest}"
  SANDBOX_ARGS="--embedded-agent-sandbox-policy required --embedded-agent-sandbox-image ${SANDBOX_IMAGE}"
  echo "[run-e2e] container-sandbox mode ON (image=${SANDBOX_IMAGE})"
  if ! command -v docker >/dev/null 2>&1; then
    echo "[run-e2e] ERROR: QUICBAAS_E2E_SANDBOX=1 but the docker CLI is missing in the runner image" >&2
    exit 1
  fi
fi

echo "[run-e2e] starting quicbaasd (backend=$BACKEND ollama=$OLLAMA_URL project=$PROJECT sandbox=${QUICBAAS_E2E_SANDBOX:-0})"
quicbaasd start \
  --http-port "$PORT" --grpc-port 0 \
  --unix-socket "$SOCK" \
  --data-dir "$DATA_DIR" \
  --log-level warn \
  --embedded-agent --embedded-agent-backend "$BACKEND" \
  --embedded-agent-ollama-url "$OLLAMA_URL" \
  ${SANDBOX_ARGS} \
  ${E2E_EXTRA_ARGS:-} \
  > "$LOG" 2>&1 &
DAEMON_PID=$!

cleanup() {
  kill -9 "$DAEMON_PID" 2>/dev/null || true
}
trap cleanup EXIT

echo "[run-e2e] waiting for daemon readiness (up to 90s)…"
ready=0
for _ in $(seq 1 90); do
  if curl -sk -o /dev/null -w '%{http_code}' "https://localhost:${PORT}/api/v1/health/ready" 2>/dev/null | grep -q 200; then
    ready=1; break
  fi
  if ! kill -0 "$DAEMON_PID" 2>/dev/null; then
    echo "[run-e2e] daemon exited during startup; log tail:"; tail -40 "$LOG"; exit 1
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "[run-e2e] daemon never became ready; log tail:"; tail -40 "$LOG"; exit 1
fi
echo "[run-e2e] daemon ready"

EXTRA_PW=""
if [ "$PROJECT" = "chromium-acp" ]; then
  export QUICBAAS_E2E_BACKEND=acp
fi

cd /app/cmd/quicbaas/frontend
NODE_EXTRA_CA_CERTS="${DATA_DIR}/certs/ca-chain.crt" \
QUICBAAS_UI_URL="https://localhost:${PORT}/ui/" \
QUICBAAS_DAEMON_LOG="$LOG" \
QUICBAAS_E2E_NO_SANDBOX=1 \
  npx playwright test --project="$PROJECT" $EXTRA_PW
RESULT=$?

if [ "$RESULT" -ne 0 ]; then
  echo "[run-e2e] playwright FAILED — daemon log tail (last 120 lines):"
  echo "===================== quicbaasd log ====================="
  tail -120 "$LOG" || true
  echo "========================================================="
fi

echo "[run-e2e] playwright exit=$RESULT"
exit $RESULT

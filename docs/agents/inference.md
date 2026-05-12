# Fleet-Routed Stateless Inference

## Overview

Fleet-routed stateless inference lets any QuicBaaS client (CLI, REST, gRPC, SDK) submit a single-turn prompt to a named backend (e.g., Ollama) running on a fleet-resident agent daemon without first creating a session. The orchestrator resolves the target daemon via the existing `RemoteBackendResolver`, forwards the prompt over the mTLS-authenticated gRPC connection, and streams the response back.

## Architecture

```
operator CLI
  → REST POST /api/v1/orchestrator/inference
      → agentServiceImpl.Infer (pkg/orchestration)
          → RemoteBackendResolver → RemoteACPBackend.Infer
              → proto.AgentRuntime/Infer (gRPC server-streaming, :9091 mTLS)
                  → Runtime.Infer (pkg/agent)
                      → OllamaBackend.Prompt → local ollama 127.0.0.1:11434
```

Session-based backends (ACP, PersistentSessions=true) are rejected with HTTP 422 / gRPC Unimplemented. Use `SpawnSession` + `PromptSession` for those backends.

## gRPC RPC

`proto.AgentRuntime` service, defined in `api/proto/agent_runtime.proto`:

```protobuf
rpc Infer(InferRequest) returns (stream InferResponse);

message InferRequest {
  string backend       = 1;
  string model         = 2;
  string prompt        = 3;
  string system_prompt = 4;
  string tenant_id     = 5;
  map<string, string> metadata = 6;
}

message InferResponse {
  repeated ContentBlock delta = 1;
  string model                = 2;
  string stop_reason          = 3;
  bool   final                = 4;
}
```

## REST Endpoint

`POST /api/v1/orchestrator/inference`

Query parameters:
- `tenant_id` — target tenant; defaults to `"default"`.

Request body (JSON):

| Field          | Type              | Required | Description                                     |
|----------------|-------------------|----------|-------------------------------------------------|
| `backend`      | string            | yes      | Named backend (e.g., `"ollama"`)                |
| `prompt`       | string            | yes      | User prompt text                                |
| `model`        | string            | no       | Model identifier; backend default if omitted    |
| `system_prompt`| string            | no       | System message prepended to the conversation    |
| `host_affinity`| string            | no       | Pin to a daemon by Name or Host                 |
| `stream`       | bool              | no       | `true` for NDJSON streaming; default `false`    |
| `metadata`     | map[string]string | no       | Opaque key-value pairs forwarded to the backend |

### Aggregated response (`stream=false`, default)

```json
{
  "content": [{"type": "text", "text": "..."}],
  "model": "qwen3:1.7b",
  "stop_reason": "end_turn"
}
```

### Streaming response (`stream=true`)

Content-Type: `application/x-ndjson`. One JSON object per line; the final line carries `"final": true`.

```json
{"delta":[{"type":"text","text":"hello "}],"final":false}
{"delta":[{"type":"text","text":"world"}],"model":"qwen3:1.7b","stop_reason":"end_turn","final":true}
```

### Error responses

| Status | Condition                                   |
|--------|---------------------------------------------|
| 400    | Missing or invalid JSON; missing backend/prompt |
| 422    | Backend requires a session (ACP/persistent)  |
| 5xx    | Daemon unreachable or internal error         |

## CLI

```bash
# Aggregated response (default)
quicbaas orchestrator inference \
  --backend ollama \
  --model qwen3:1.7b \
  --prompt "write a haiku about mTLS"

# Streaming response
quicbaas orchestrator inference \
  --backend ollama \
  --model qwen3:1.7b \
  --prompt "explain QUIC in three sentences" \
  --stream

# Pin to a specific daemon
quicbaas orchestrator inference \
  --backend ollama \
  --host-affinity gpu-host \
  --model qwen3:1.7b \
  --prompt "summarise the NIST CSF"

# JSON output
quicbaas orchestrator inference \
  --backend ollama \
  --prompt "hello" \
  --output json
```

## Go SDK

```go
orch := sdk.Orchestrator()

// Aggregated
resp, err := orch.Inference(ctx, &orchestrator.InferenceRequest{
    Backend: "ollama",
    Model:   "qwen3:1.7b",
    Prompt:  "write a haiku about mTLS",
})

// Streaming
chunks, err := orch.InferenceStream(ctx, &orchestrator.InferenceRequest{
    Backend: "ollama",
    Prompt:  "explain QUIC",
})
for chunk := range chunks {
    fmt.Print(chunk.Delta[0].Text)
}
```

## Key Types

| Package                                | Type                   | Role                                          |
|----------------------------------------|------------------------|-----------------------------------------------|
| `pkg/orchestration`                    | `InferRequest`         | Domain request (backend, prompt, tenant, etc.)|
| `pkg/orchestration`                    | `InferChunk`           | One streaming delta from any backend          |
| `pkg/orchestration`                    | `InferResponse`        | Aggregated result of `AccumulateChunks`       |
| `pkg/orchestration`                    | `InferUnsupportedError`| Returned for session-required backends        |
| `pkg/orchestration`                    | `InferBackendError`    | Wraps a backend Prompt failure                |
| `pkg/orchestration`                    | `AccumulateChunks`     | Drains `<-chan *InferChunk` to `*InferResponse`|
| `pkg/orchestration`                    | `AgentService.Infer`   | Service interface method                      |
| `pkg/agent`                            | `Runtime.Infer`        | Agent-side: delegates to OllamaBackend.Prompt |
| `pkg/agent`                            | `GRPCSpawnServer.Infer`| gRPC streaming handler on the daemon          |
| `sdk/go/client/orchestrator`           | `InferenceRequest`     | SDK wire type                                 |
| `sdk/go/client/orchestrator`           | `InferenceChunk`       | SDK streaming chunk                           |

## Scope

The `AgentScopeRuntimeInfer = "agent:runtime:infer"` scope authorises daemon tokens for the Infer RPC. The handler also accepts the existing `AgentScopeRuntimePrompt` scope for backward compatibility with tokens issued before this feature was deployed.

## Session Events

Two session-event kinds are emitted when the runtime Infer path is active:

| Kind                    | Payload                                        |
|-------------------------|------------------------------------------------|
| `inference_submitted`   | `InferenceSubmittedPayload` (backend, model, prompt) |
| `inference_completed`   | `InferenceCompletedPayload` (backend, model, stop_reason) |

Sessionless inference does not have an operator-visible session, so the
agent runtime synthesises a per-call SessionID of the form
`inference-<uuid>` and emits both lifecycle events under that key. This
satisfies the control plane event-log's session-id validation while still
letting operators correlate the two halves of the same call. Query
back via `SubscribeSessionEvents(tenant, "inference-<uuid>")`.

These events appear in the session event stream and can be consumed via `SubscribeSessionEvents`.

## Using the GUI

The QuicBaaS web UI and Wails desktop app expose inference through a dedicated
**Inference** view reachable at `#/inference` (sidebar: ORCHESTRATION > Inference).

### Layout

The view is structured as four daisyUI cards stacked vertically:

**Card 1 — Target**

Select the backend, host affinity, and model:

- **Backend** — populated from `GET /api/v1/orchestrator/backends`. Session-only
  backends (`claude`, `codex`, `opencode`) are visible but disabled with a tooltip
  explaining that they require a session. Use the Sessions view for those.
- **Host affinity** — populated from `GET /api/v1/orchestrator/agents` filtered to
  agents whose `backend` field or `capabilities` list includes the chosen backend.
  Select "Any" to let the orchestrator pick the best available daemon.
- **Model** — free-text input. Defaults to `qwen3:1.7b` for the `ollama` backend.
  The model must be installed on the chosen agent's local Ollama instance.

**Card 2 — Prompt**

- **System prompt** — collapsible; defaults to collapsed. Provides an optional system
  message prepended to the conversation.
- **Prompt** — required. Press Enter inside the form or click **Send**.
- **Stream** toggle — enabled by default. When on, the UI appends delta chunks as they
  arrive from the NDJSON stream. When off, waits for the full aggregated response.
- **Send** button — disabled while a request is in-flight.
- **Cancel** button — visible only when a request is in-flight; aborts the stream
  immediately while preserving any partial output already displayed.

**Card 3 — Response**

- Live output area with `aria-live="polite"` so screen readers announce streaming
  chunks progressively.
- Footer shows model name, stop reason, and wall-clock elapsed time once the
  response is complete.
- **Copy** button — copies the full response text to the clipboard.
- **Clear** button — resets the response area for a fresh prompt (does not clear the
  prompt field itself).

**Card 4 — Session History**

Displays the last 10 inference calls from the current browser session as collapsible
rows. Each row shows the timestamp, backend, model, host, prompt summary, and elapsed
time. Expanding a row reveals the full prompt and response text. History survives view
re-mounts (it lives in module-level Svelte 5 `$state`) but is cleared on page reload.

### Error handling

| Scenario | Behaviour |
|----------|-----------|
| No backends registered | Empty-state message with link to Agent Fleet |
| No agents for chosen backend | Empty-state below host selector with link to Agent Fleet |
| 4xx from server | Field-level error below the prompt + error toast |
| 5xx from server | Error toast; prompt text preserved |
| Stream interrupted mid-response | Partial output retained; warning toast |
| >30 s generating | Spinner with "Generating..." label on the Send button |
| User cancels | Partial output retained; info toast |

### Wails desktop mode

In the Wails desktop app the streaming path falls back to a single aggregate
`OrchestratorBinding.Inference` call because Wails IPC does not support chunked
HTTP. The response is yielded as a single `InferenceChunk` with `final: true`,
so the Svelte streaming loop terminates correctly and the UI behaves identically
to the streaming REST path from the user's perspective.

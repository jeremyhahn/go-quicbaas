# quicbaas orchestrator

Operate against the orchestration layer: backends, agents, sessions,
prompts, jobs, workflows, and stateless inference. Every subcommand
goes through the SDK abstraction and works on every transport (REST,
gRPC, QUIC, MCP, Unix socket).

## Subcommands

| Command | Description |
|---------|-------------|
| `backends list` | List backends registered with the orchestrator |
| `agents list` | List agent daemons currently in the fleet registry |
| `agents register` | Register an agent (mTLS-bootstrapped, normally via `agent deploy`) |
| `session spawn` | Open a persistent ACP session against a session-based backend |
| `session prompt` | Send a prompt into an existing session |
| `session attach` | Replay history + tail live events for a session |
| `session list` | List sessions filterable by tenant / backend / status |
| `inference` | Send a stateless prompt — no session needed |
| `jobs list` / `jobs start` | Queue and inspect orchestration jobs |
| `workflows list` / `workflows run` | Named multi-step workflow execution |

## quicbaas orchestrator inference

Stateless prompt routed to a fleet-resident backend (e.g., the Ollama
instance bundled with each agent daemon). The orchestrator picks an
online agent serving the chosen backend, forwards the prompt over an
mTLS-authenticated gRPC channel, and streams the response back.

### Flags

| Flag | Type | Description |
|------|------|-------------|
| `--backend` | string (required) | Backend name (`ollama`, etc.). Session-only backends (`claude`, `codex`, `opencode`) are rejected with `InferUnsupportedError`. |
| `--prompt` | string (required) | The user prompt. |
| `--model` | string | Backend model identifier. Defaults to the backend's `default_model`. |
| `--system-prompt` | string | Optional system message prepended to the prompt. |
| `--host-affinity` | string | Pin to an agent by name or hostname. |
| `--metadata` | `key=value` (repeatable) | Forwarded verbatim to the backend. |
| `--stream` | bool | Emit NDJSON chunks as they arrive instead of buffering. |
| `--tenant` | string | Defaults to the global `--tenant` flag (`default`). |
| `--timeout` | duration | Per-call timeout. Defaults to the global `--timeout`. |
| `--output` | `table\|json\|yaml` | Output format. Default `table`. |

### Examples

```bash
# Default (REST) — auto-selected from the URL scheme.
quicbaas orchestrator inference \
  --backend ollama --model qwen3:1.7b \
  --prompt "explain QUIC in three sentences"

# Streaming.
quicbaas orchestrator inference \
  --backend ollama --model qwen3:1.7b \
  --prompt "tell me a haiku about mTLS" \
  --stream

# Pin to a specific agent.
quicbaas orchestrator inference \
  --backend ollama --host-affinity gpu-host \
  --prompt "summarise the NIST CSF"

# Same command over each transport.
quicbaas --transport grpc --address localhost:19090 --tls-skip-verify \
  orchestrator inference --backend ollama --prompt "ping"

quicbaas --transport quic --address localhost:10443 --tls-skip-verify \
  orchestrator inference --backend ollama --prompt "ping"

quicbaas --transport mcp --address localhost:11443 --tls-skip-verify \
  orchestrator inference --backend ollama --prompt "ping"

quicbaas --transport unix --address /tmp/quicbaas.sock \
  orchestrator inference --backend ollama --prompt "ping"
```

### Output (`--output json`)

```json
{
  "content": [{"type": "text", "text": "QUIC is..."}],
  "model": "qwen3:1.7b",
  "stop_reason": "end_turn"
}
```

### Streaming output (`--stream`)

NDJSON, one chunk per line:

```json
{"delta":[{"type":"text","text":"QUIC "}],"final":false}
{"delta":[{"type":"text","text":"is..."}],"final":false}
{"delta":[{"type":"text","text":""}],"model":"qwen3:1.7b","stop_reason":"end_turn","final":true}
```

### Errors

| Exit | Cause |
|------|-------|
| 0 | Successful response. |
| 1 | Invalid flag combination, missing required flags. |
| 2 | Backend rejects sessionless prompts (use `session spawn` + `session prompt`). |
| 3 | No online agent serves the requested backend in this tenant. |
| 4 | Network / mTLS handshake failure. |
| 5 | Backend returned an error mid-stream. |

### See also

- [Fleet quick start](../agents/fleet-quickstart.md) — full deploy + inference walkthrough.
- [agents/inference.md](../agents/inference.md) — RPC, REST, SDK, and GUI reference.
- [agents/deploy.md](../agents/deploy.md) — how to put an agent on a remote host.

## quicbaas orchestrator backends list

List all backends the orchestrator knows about, including their
capabilities (session-based vs. sessionless, tool use, file access,
streaming).

```bash
quicbaas orchestrator backends list --output json
```

```json
[
  {"name":"ollama","type":"ollama","capabilities":{"persistent_sessions":false,"streaming":true}},
  {"name":"claude","type":"acp","capabilities":{"persistent_sessions":true,"tool_use":true,"file_access":true,"acp":true}},
  ...
]
```

## quicbaas orchestrator agents list

List the agent daemons currently registered in the fleet:

```
ID                  NAME       HOST              TYPE   STATUS   LAST SEEN
agt_abc123def456    gpu-host    198.51.100.91    acp    running  2026-04-25T17:12:59Z
```

The same data is rendered live by the GUI under **Orchestration →
Agent Fleet**.

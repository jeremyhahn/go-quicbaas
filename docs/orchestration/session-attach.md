# Session Attach & Event Log

Attach to any running orchestrator session to scroll back through its history and live-tail new events as they arrive. Sessions are tenant-scoped; operators see only their tenant's events.

## CLI

```bash
# Attach + follow (default pretty formatter).
quicbaas orchestrator attach <session-id>

# Replay from a specific sequence.
quicbaas orchestrator attach <session-id> --from 120

# Fetch scrollback and exit (no live tail).
quicbaas orchestrator attach <session-id> --no-follow

# Emit raw JSON events (for piping into jq/scripts).
quicbaas orchestrator attach <session-id> --format json
```

The CLI uses the SDK, so `--protocol` selects transport:
`embedded`, `rest`, `grpc`, `quic`, `mcp`, or `unix`.

## REST

```
GET /api/v1/orchestrator/sessions/{id}/events
  ?tenant_id=<tenant>      (defaults to "default")
  &from=<seq>              (replay offset; 0 = from start)
  &follow=0|1              (1 = upgrade to SSE live tail after replay)
  &limit=<N>               (JSON mode only; default 256)
```

Content negotiation:
- `Accept: text/event-stream` → SSE (default)
- `Accept: application/json` with `follow=0` → paginated JSON array + `X-Next-Sequence` response header

SSE frames carry `id: <sequence>`; browsers resume via `Last-Event-ID` on reconnect. Server honors `Last-Event-ID` over the `from` query param so reconnects never miss or duplicate events.

## gRPC

```proto
service OrchestratorService {
  rpc SubscribeSessionEvents(SubscribeSessionRequest)
    returns (stream SessionEventEnvelope);
}
```

Server-streaming RPC mirrors the SSE semantics. The `from_sequence` field drives replay; omit to start from the first event.

## Event Kinds

Each event is one of:

| Kind | Payload |
|------|---------|
| `prompt_submitted` | The prompt text sent to the agent |
| `assistant_chunk` | One fragment of assistant output (+ `stop_reason` on final chunk) |
| `tool_invocation` | Tool name + args for a Read/Write/Edit/Bash/etc. call |
| `tool_result` | Tool output, paired to the invocation via `invocation_id` |
| `file_op` | File read/write/edit with optional unified diff |
| `system_message` | Orchestrator-internal notices (info/warn/error) |
| `operator_joined` | An operator attached via CLI/web/signal |
| `operator_left` | An operator detached |
| `session_closed` | Terminal event; carries reason |
| `session_resumed` | Daemon reattach after central or daemon restart |

## At-Least-Once Delivery

Every event carries a monotonic per-session `sequence` starting at 1. The log rejects any append whose sequence is not exactly `latest+1` (`ErrSessionLogGap`). Consumers validate monotonicity; if a gap appears they can `Range()` the missing window to catch up.

## Tenant Isolation

The log keys events on `(tenant_id, session_id)` and never leaks across tenants. Cross-tenant attach attempts return an empty stream rather than another tenant's data. At rest, event payloads are encrypted with the tenant's DEK; the session ID is mixed into the AEAD AAD so ciphertexts cannot be relocated across sessions.

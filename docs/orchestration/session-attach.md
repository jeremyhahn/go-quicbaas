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

### Long-Lived Streams

A follow stream lasts as long as the turns it reports on, which is unbounded.
Three mechanisms keep it alive:

- **No write deadline.** `http.Server.WriteTimeout` arms an absolute write
  deadline when the request is read and never extends it, so a streaming
  response would be severed exactly `WriteTimeout` (default 30s) after it
  began. The handler clears the deadline per-connection with
  `http.ResponseController.SetWriteDeadline(time.Time{})`; the global
  `WriteTimeout` still protects every unary route.
- **Heartbeats.** The live tail emits a `: ping <unix>` comment frame every
  15 seconds. Conforming readers ignore comments, but the bytes stop an idle
  connection from looking dead to a proxy (nginx `proxy_read_timeout`, HAProxy
  `timeout tunnel` and AWS ALB idle timeout all default to 60s, so three
  heartbeats can be lost before any of them reaps the connection) and let the
  server notice a departed client during a silent turn.
- **Advertised retry.** The stream opens with `retry: 2000`, so an EventSource
  client reconnects within the presence linger window (below).

The middleware exempts streaming requests — `Accept: text/event-stream` or
`follow=1` — from `TimeoutMiddleware`, which would otherwise buffer the whole
response and cut it at the request deadline.

### Client Resumption

The SDK REST reader (`sdk/go/transport/rest`) resumes a `follow` stream that
ends without an explicit server error. It re-opens at `from=<last sequence+1>`
with a matching `Last-Event-ID` header, so a resumed stream has no gap and no
duplicate. Retries are bounded at 5 attempts with exponential backoff from
250ms (about 8 seconds total); exhausting them yields a typed
`SessionStreamReconnectError` carrying the session, the last delivered
sequence, and the final failure. A server-sent `event: error` frame is
terminal and is never retried.

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
| `agent_thought` | One fragment of the agent's chain-of-thought reasoning (`thought.text`) |
| `tool_invocation` | Tool name + args for a Read/Write/Edit/Bash/etc. call |
| `tool_result` | Tool output, paired to the invocation via `invocation_id` |
| `file_op` | File read/write/edit with optional unified diff |
| `system_message` | Orchestrator-internal notices (info/warn/error) |
| `operator_joined` | An operator started watching via CLI/web/signal |
| `operator_left` | An operator stopped watching |
| `session_closed` | Terminal event; carries reason |
| `session_resumed` | Daemon reattach after central or daemon restart |
| `turn_started` | An agent turn was accepted and dispatched; carries `turn_id`, `backend`, `model` |
| `turn_completed` | The turn finished; carries `turn_id`, `stop_reason`, `model`, `duration_ms` |
| `turn_failed` | The turn ended in an error; carries `turn_id`, `error`, `duration_ms` |

`turn_started` / `turn_completed` / `turn_failed` bracket every prompt, so a
consumer can tell "the agent is working" from "the agent is idle" without
inferring it from the absence of a `stop_reason`. See
[Asynchronous Agent Turns](async-turns.md).

Between those brackets, backends stream `assistant_chunk`, `agent_thought`,
`tool_invocation`, and `tool_result` events into the transcript live as the
turn runs — an attached consumer watches the agent work rather than receiving
only the final response.

## At-Least-Once Delivery

Every event carries a monotonic per-session `sequence` starting at 1. The log rejects any append whose sequence is not exactly `latest+1` (`ErrSessionLogGap`). Consumers validate monotonicity; if a gap appears they can `Range()` the missing window to catch up.

## Operator Presence

Presence describes a person watching a session, not the connection carrying the
stream. `operator_joined` is written only when an operator's live-attachment
count for a session rises from zero, and `operator_left` only after it has
stayed at zero for the linger window (15s, `orchestration.DefaultPresenceLinger`).
A dropped-and-resumed stream, a browser tab reload, or a second tab therefore
leave the transcript untouched, while a genuine departure is still recorded.
Server shutdown commits every outstanding departure immediately so a restart
never leaves phantom operators in a transcript.

## Tenant Isolation

The log keys events on `(tenant_id, session_id)` and never leaks across tenants. Cross-tenant attach attempts return an empty stream rather than another tenant's data. At rest, event payloads are encrypted with the tenant's DEK; the session ID is mixed into the AEAD AAD so ciphertexts cannot be relocated across sessions.

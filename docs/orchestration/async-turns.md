# Asynchronous Agent Turns

Prompting an agent session is asynchronous on every transport. The server
records the turn, dispatches it in the background, and answers immediately with
an acknowledgement. The agent's output is delivered on the durable session event
log, which is the same stream `quicbaas orchestrator attach` and the web UI
already consume — see [Session Attach & Event Log](session-attach.md).

## Why

A real agent turn runs for minutes. No request/response transport can hold a
connection open that long, and QuicBaaS does not try: the REST server caps a
request at 25 seconds (`pkg/server/lifecycle_rest.go`). A synchronous prompt
contract therefore made real agent work structurally impossible — a prompt
asking an agent to read a directory and summarise it died with
`request timeout after 25s` before the agent had finished thinking.

Raising the cap only moves the failure. The turn is decoupled from the request
instead.

## Contract

`POST /api/v1/orchestrator/sessions/{id}/prompt` (and its equivalent on every
other transport):

1. Validates the request — tenant, prompt text, session, backend resolution.
   Every rejection is synchronous, so a caller never gets an acknowledgement for
   a turn that will not happen.
2. Appends `prompt_submitted` and `turn_started` to the session event log.
3. Dispatches the turn on a context detached from the request
   (`context.WithoutCancel`), so a client disconnect never cancels the agent.
4. Returns the acknowledgement.

When the turn ends, the background dispatch appends `turn_completed` or
`turn_failed` carrying the same `turn_id`.

### Acknowledgement

```json
{
  "session_id": "sess-abc123",
  "turn_id":    "9f3c…",
  "status":     "accepted",
  "sequence":   42,
  "backend":    "claude",
  "model":      "claude-sonnet",
  "timestamp":  1786207030175886792
}
```

`sequence` is the session-log sequence of this turn's `turn_started` event.
Attach from it to receive the whole turn with no gap and no duplicates, even if
the agent finished before you subscribed.

### Stateless prompts stay synchronous

A prompt that names a `backend` but no `session_id` is a stateless one-shot. It
has no event log to publish to, so the completed turn is returned inline
(`content` populated, no `status`). This is a different operation, not a
fallback: the two shapes travel on one wire type and are told apart by `status`.

## Per-transport surface

| Transport | Dispatch | Turn output |
|-----------|----------|-------------|
| REST | `POST .../sessions/{id}/prompt` → `202 Accepted` + ack | SSE at `GET .../sessions/{id}/events?follow=1&from={sequence}` |
| gRPC | `OrchestratorService/PromptSession` → ack in `PromptSessionResponse` (`status`, `turn_id`, `sequence`) | `OrchestratorService/SubscribeSessionEvents` server stream |
| QUIC | orchestrator prompt sub-op → ack payload | session event stream |
| Unix | gRPC over a Unix socket; identical to gRPC | identical to gRPC |
| Embedded | `svc.Agent().PromptSessionAsync` → ack | in-process `SessionEventLog.Subscribe` |
| MCP | `quicbaas_orchestrator_prompt` → ack | **poll** `quicbaas_orchestrator_session_history` |

### MCP has no stream

MCP is request/response only — there is no server-push channel. The prompt tool
returns the acknowledgement and nothing else; it never carries `content`.
An MCP client observes the turn by paging the transcript:

```
quicbaas_orchestrator_prompt          → {session_id, turn_id, status, sequence}
quicbaas_orchestrator_session_history → {events, next_seq}   (from_sequence = sequence)
```

Page until an event of kind `turn_completed` or `turn_failed` carrying the same
`turn_id` appears. `quicbaas_orchestrator_status` reports the *session's* state
(active, closed); it cannot tell you whether a particular turn has finished, so
the transcript is the authoritative signal.

## SDK

`PromptSessionResponse` reports which shape it holds:

| Field | Accepted turn | Stateless turn |
|-------|---------------|----------------|
| `Accepted` | `true` | `false` |
| `Done` | `false` | `true` |
| `TurnID` | set | empty |
| `Sequence` | `turn_started` sequence | `0` |
| `Response` | empty | the agent's text |

`Done` means "this response describes a finished turn". An accepted turn has
only started, so `Done` is false and the terminal state is observed on the event
stream. Both shapes are built in `sdk/go/client/orchestrator/session_wire.go`,
the single conversion point every transport routes through.

Follow a turn with the existing stream API:

```go
ack, err := client.PromptSession(ctx, &orchestrator.PromptSessionRequest{
    SessionID: sessionID,
    Prompt:    "audit this directory",
})
events, err := client.SubscribeSessionEvents(ctx, &orchestrator.SubscribeSessionEventsRequest{
    SessionID:    ack.SessionID,
    FromSequence: ack.Sequence,
    Follow:       true,
})
```

## CLI

`quicbaas orchestrator prompt` follows the turn by default, so the command still
reads as a synchronous conversation even though the transport is not:

```bash
# Send a prompt and stream the turn until it completes (default).
quicbaas orchestrator prompt sess_abc123 "Write unit tests for the auth handler"

# Print the acknowledgement and exit — for scripts and CI.
quicbaas orchestrator prompt sess_abc123 "Refactor the database layer" --follow=false

# Reattach to a turn already in flight.
quicbaas orchestrator attach sess_abc123 --from 42
```

`--follow` defaults to true because the affirmative behaviour an operator asks
for when typing a prompt is "show me the answer". Interrupting the follow
detaches the CLI only; the turn keeps running on the server. A turn that ends in
`turn_failed` exits non-zero with the agent's own reason, so scripts can branch
on it.

## Ordering and concurrency

Every session event carries a monotonic per-session sequence, and the log
rejects an append that is not exactly `latest+1`. A session has several genuine
concurrent writers — background turn goroutines, other transports, and every
live attacher recording its presence — so sequence assignment goes through one
shared, retrying write path, `orchestration.AppendNextSessionEvent`. A turn's
events therefore appear in the transcript in this order:

```
prompt_submitted → turn_started → (assistant_chunk | agent_thought | tool_invocation | tool_result | file_op)* → turn_completed | turn_failed
```

## Shutdown

`WaitForTurns` blocks until every accepted turn has written its terminal event.
It is deliberately *not* wired into daemon shutdown: an agent turn runs for
minutes, and a shutdown that stalls that long is worse than a transcript that
resumes on restart. It exists so callers that need a quiescent log — tests above
all — have a synchronization point instead of a sleep.

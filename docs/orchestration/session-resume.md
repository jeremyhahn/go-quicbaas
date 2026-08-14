# Resumable Sessions

An agent session outlives the daemon that hosted it. The agent process does not.

A persistent session holds a long-running agent subprocess as a child of the
daemon (`pkg/orchestration/acp_backend.go`). When the daemon restarts, that
subprocess dies with it. Before this feature the session vanished with the
process: a prompt against its ID failed with
`agent: backend is required: either backend or session_id must be provided`,
because the in-process registry no longer knew the session existed — even
though its transcript was safely on disk.

Resumable sessions close that gap. The description of a session is durable, so
after a restart the session is addressable again, and the first prompt that
needs an agent process creates one and restores the conversation into it.

Source: `pkg/orchestration/session_store.go`, `session_resume.go`,
`orchestrator_resume.go`, `acp_backend_resume.go`.

## Session states

| State | Meaning |
| --- | --- |
| `active` | An agent process is serving the session. |
| `idle` | The session exists and is waiting for input. |
| `resumable` | The durable record survived a restart; there is no agent process yet. The next prompt creates one. |
| `closed` | The session is over. |
| `error` | A resume was attempted and failed; `resume_error` carries the reason. |

`resumable` is reported by `orchestrator status`, `orchestrator sessions`, and
every transport (REST/QUIC/MCP/Unix carry the string; gRPC carries
`SESSION_STATE_RESUMABLE`). No new verb was added: resuming is transparent on
prompt.

```
$ quicbaas orchestrator sessions
SESSION ID          BACKEND  STATUS     LAST ACTIVE
ses-8f2a...         claude   resumable  2026-08-08T09:14:22Z
```

## The durable record

`SessionRecord` holds everything needed to address a session and to rebuild the
agent that served it: identity and tenant, backend and model, system prompt,
work directory, environment, extra CLI arguments, MCP server configuration,
sandbox mode and image, host affinity, the persistent flag, the pooled provider
account it authenticates as, and the lifecycle status.

It lives under the standard orchestration keyspace, so it inherits the same
durability, replication, tenant isolation and at-rest encryption as projects,
agents and tasks (see [persistence.md](persistence.md)):

```
orch/session/{tenant}/{sessionID}   -> SessionRecord (JSON)
```

Two things are deliberately **not** in the record:

- **The transcript.** Conversation content lives in the `SessionEventLog`,
  which is the single source of truth for it. Copying it here would create a
  second copy to keep in sync.
- **The derived MCP payload.** `QUICBAAS_MCP_SERVERS` encodes the paths of
  certificates minted for one spawn and deleted when the session closes.
  Persisting it would resume the session against material that no longer
  exists, so it is stripped and re-derived from the retained `MCPServers`
  against freshly minted certificates on each resume.

### The account pin

`AccountID` records the pooled provider account the spawn **resolved to**, not
the one it asked for. A spawn that used strategy-based selection
(`AccountAuto`) named no account at all, so only the resolved value is
meaningful.

`spawnRequestFromRecord` replays it as a **pin** — `AccountID` set,
`AccountAuto` deliberately false — so no selection strategy runs a second time.
Re-running selection would be a silent correctness bug: a `least_usage` pool
moves between the spawn and the resume, so the reconstituted process would
authenticate as a different provider account than the conversation it is
continuing has been billed to. That breaks a static pin outright and corrupts
per-account cost attribution (see [accounts.md](accounts.md)).

`AccountStrategy` is retained as audit history — it is what lets a cost anomaly
be traced back to the policy that produced it — and is deliberately *not*
replayed as a selection input.

A record with no account produces a request with no account, so a session that
never used the pool is unaffected.

## Respawn policy: lazy

At boot the daemon calls `LoadPersistedSessions`, which restores the registry
from the durable store. It does **not** start agent processes.

| Record | Restored as |
| --- | --- |
| Persistent, local backend, not closed | `resumable` — process recreated on the next prompt |
| Any backend that is no longer registered | not restored (it cannot be addressed) |
| Non-persistent | marked `closed` — it ran a fresh process per prompt, so there was never a process to miss |
| Remote (dispatched to another daemon) | `active` — its process belongs to another host, which this restart did not touch |
| Already `closed` | not restored |

Lazy rather than eager, for three reasons:

1. **Resource.** Eager respawn forks one agent subprocess per stored session at
   boot. A daemon with fifty sessions would fork fifty `claude` processes, each
   taking tens of seconds to initialise, before it could serve a request.
2. **Cost.** Restoring a conversation costs a context window — replayed
   transcript tokens, or a native resume's reload. Doing that for a session
   nobody prompts again is money spent on nothing.
3. **Correctness.** Boot has no request context: no tenant, no operator
   identity, no authenticated caller to mint MCP certificates against.
   Reconstituting on the prompt path means the transcript is read from the
   caller's own tenant barrier and any credential material is minted against
   the caller's identity.

The cost of lazy is one-time latency on the first prompt after a restart — the
same latency the original spawn paid.

## Resume mechanisms

`ACPBackendConfig.NativeSessionResume` (operator config:
`orchestration.claude_native_session_resume`) selects the mechanism.

### Replay (default)

The CLI runs with `--no-session-persistence`, so it keeps no transcript on the
host. On resume, the orchestrator reads the durable, barrier-encrypted
transcript back from the `SessionEventLog`, distils it into conversation turns,
bounds it, and hands it to the backend. The backend primes the fresh process
with it.

The priming message **replaces the spawn handshake**. The persistent CLI only
emits its init record after receiving input, so every spawn already pays for one
round trip; a replay resume costs no extra turn.

What is replayed: the system prompt (always, in full — it is configuration, not
conversation), operator prompts, and agent output. Consecutive streamed
assistant chunks are merged into the single message the operator saw. Tool
invocations and their results are **not** replayed: they would consume most of
the budget on mechanics the agent will redo anyway, and a tool result restated
as prose is not something the agent can act on as if it had run the tool.

### Native

The CLI keeps its own transcript under the daemon's home directory and the
resume runs `claude --resume <session-id>`. Fidelity is complete — every tool
call and result is restored — and QuicBaaS re-sends nothing.

The trade is a plaintext copy of every conversation on the host filesystem,
**outside the storage barrier**. Enable it only where that filesystem is
already inside the trust boundary. Because of that, it is off by default.

```yaml
orchestration:
  claude_native_session_resume: false   # default: replay from the barrier
  transcript_replay_budget: 49152       # characters; 0 selects the default
```

## Replay bound

`DefaultReplayBudget` is **48 KiB of transcript text** — roughly 12k tokens at
the conventional four-characters-per-token estimate. It is a cost control as
much as a context-window control: a resumed agent is charged for every
character of history it is given.

Truncation is **oldest-first** and drops whole turns: the turn about to be
answered follows the newest turns, not the oldest. A single turn larger than the
whole budget is kept whole rather than cut mid-sentence — a truncated turn reads
as a different statement from the one that was made.

When turns are dropped the agent is told so, in the priming message itself:

```
[4 earlier turn(s) omitted: they exceeded the replay budget and are NOT
available to you]
```

An agent that knows its view is partial can say so. One handed a silently
truncated history cannot.

## No silent fresh starts

A resume that cannot restore context fails; it never produces a context-free
agent wearing the old session ID.

| Situation | Result |
| --- | --- |
| Backend implements no resume mechanism | `ErrSessionResumeUnsupported` |
| Replay mode with an unreadable transcript | `ErrSessionTranscriptUnavailable` |
| Backend fails to start the replacement process | `ErrSessionResumeFailed` |
| Session is not persistent | `ErrSessionNotResumable` |
| Session's process is still live | `SessionAlreadyLiveError` — replacing it would discard the context it holds |

A failure moves the durable record to `error` with `resume_error` set, appends a
`session_resume_failed` system message to the transcript, and returns the error
to the caller. The prompt is never delivered to the replacement agent.

The one case that is *not* a failure: a persistent session that was spawned but
never prompted has an empty transcript. A fresh process is then a faithful
restoration of an empty conversation, so it starts on the ordinary handshake.

## What a successful resume records

- Durable record: `status=active`, `resume_mode=native|replay`,
  `resume_error` cleared, `last_activity` stamped.
- Transcript: a `session_resumed` event naming the mode and, for a replay, how
  many turns were replayed and how many omitted.
- Session metadata (the attach surface): status returned to `active`.

Concurrent prompts against one resumable session are serialised per session, so
two callers arriving together produce exactly one replacement process.

## Scope: local sessions only

A session dispatched to a remote agent daemon is owned by that daemon's
process, not by the control plane's. When the control plane restarts, the
session record is restored as `active` and follow-up prompts route to the same
daemon by host affinity — which is correct, because the agent there is still
running.

If the **remote daemon** restarts, its own in-process registry is lost and the
prompt fails with `acp: session not found` from that host. Reconstituting a
session on a remote daemon needs the same durable record and resume path inside
`pkg/agent`'s runtime; that is not implemented. Until it is, a remote daemon
restart still ends its sessions.

## Related

- [persistence.md](persistence.md) — the KV keyspace and tenant isolation rules
  session records inherit.
- [async-turns.md](async-turns.md) — the turn contract a resumed session serves.
- [recovery.md](recovery.md) — crash recovery and auto-restart for workflows.

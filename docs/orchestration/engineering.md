# Orchestration — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Implementation contract for the orchestration engine (`pkg/orchestration`):
> exported surface, invariants, errors, configuration, persistence, tests and
> observability. Feature folder: `docs/orchestration/`. Conflicts with this
> document are defects.

Agent backends are the execution layer for AI agent orchestration. Each backend implements the `AgentBackend` interface, providing a uniform API for spawning sessions, sending prompts, and managing lifecycle across different AI runtimes.

## AgentBackend Interface

```go
type AgentBackend interface {
    Spawn(ctx context.Context, req *SpawnRequest) (*Session, error)
    Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error)
    Status(ctx context.Context, sessionID string) (*SessionStatus, error)
    Close(ctx context.Context, sessionID string) error
    Capabilities() BackendCapabilities
    Type() string
}
```

| Method | Description |
|--------|-------------|
| `Spawn` | Creates a persistent session. Returns `UnsupportedOperationError` for stateless backends. |
| `Prompt` | Sends a prompt. Uses an existing session if `SessionID` is set, creates an ephemeral session if empty (ACP), or executes directly (stateless). |
| `Status` | Returns session health: state, last activity, uptime. |
| `Close` | Terminates a session and cleans up resources. |
| `Capabilities` | Reports backend feature support. |
| `Type` | Returns the backend type identifier string (e.g., `"acp"`, `"ollama"`). |

## BackendCapabilities

```go
type BackendCapabilities struct {
    Kind               BackendKind `json:"kind"`
    PersistentSessions bool        `json:"persistent_sessions"`
    ToolUse            bool        `json:"tool_use"`
    FileAccess         bool        `json:"file_access"`
    Streaming          bool        `json:"streaming"`
    ACP                bool        `json:"acp"`
}
```

| Field | Description |
|-------|-------------|
| `Kind` | Backend classification: `agent` or `model_server`. |
| `PersistentSessions` | Backend supports long-lived sessions via `Spawn`/`Close`. |
| `ToolUse` | Backend agent can invoke tools during execution. |
| `FileAccess` | Backend agent can read/write files. |
| `Streaming` | Backend supports streaming responses. |
| `ACP` | Backend speaks the Agent Client Protocol. |

## Backend Classification

Each backend is classified by its `BackendKind`, which determines its fundamental operational model:

```go
type BackendKind string

const (
    BackendKindAgent       BackendKind = "agent"
    BackendKindModelServer BackendKind = "model_server"
)
```

| Kind | Backends | Description |
|------|----------|-------------|
| `agent` | claude, codex, opencode | ACP-compatible. Persistent sessions, tool use, file access. |
| `model_server` | ollama | Stateless prompt/response only. No sessions or tool use. |

The `Kind` field is included in `BackendCapabilities` and can be used to branch behavior based on whether a backend supports the full agent lifecycle or only stateless inference.

## Built-in Backends

### Capabilities Summary

| Backend | Kind | PersistentSessions | ToolUse | FileAccess | Streaming | ACP |
|---------|------|--------------------|---------|------------|-----------|-----|
| claude | `agent` | Yes | Yes | Yes | No | Yes |
| codex | `agent` | Yes | Yes | Yes | No | Yes |
| opencode | `agent` | Yes | Yes | Yes | No | Yes |
| ollama | `model_server` | No | No | No | No | No |

### ACPBackend

Type identifier: `"acp"`

For ACP-compatible agents such as Claude Code, Codex, and OpenCode. Communicates via JSON-RPC over stdin/stdout using `StdioTransport`.

**Capabilities:**

| Capability | Supported |
|------------|-----------|
| PersistentSessions | Yes |
| ToolUse | Yes |
| FileAccess | Yes |
| Streaming | No |
| ACP | Yes |

**Behavior:**

- `Spawn` starts a subprocess, sends `initialize`, then `session/new`. Returns a `Session` with process PID.
- `Prompt` with `SessionID` routes to an existing session. Without `SessionID`, creates an ephemeral session (spawn, prompt, close).
- `Close` sends a `session/close` notification, then shuts down the transport with SIGTERM (5s timeout before SIGKILL).

**Configuration:**

```go
type ACPBackendConfig struct {
    Command      string   `json:"command"`       // e.g., "claude"
    Args         []string `json:"args"`           // e.g., ["--agent"]
    DefaultModel string   `json:"default_model"`  // e.g., "claude-sonnet-4-20250514"
}
```

### OllamaBackend

Type identifier: `"ollama"`

For local Ollama LLMs. Stateless -- every prompt is an independent chat request.

**Capabilities:**

| Capability | Supported |
|------------|-----------|
| PersistentSessions | No |
| ToolUse | No |
| FileAccess | No |
| Streaming | No |
| ACP | No |

**Behavior:**

- `Spawn`, `Status`, `Close` all return `UnsupportedOperationError`.
- `Prompt` sends a chat request via the Ollama `ChatService`. System prompt is prepended as a system message if provided.

**Configuration:**

```go
type OllamaBackendConfig struct {
    ChatService  *ollama.ChatService
    DefaultModel string  // e.g., "llama3"
}
```

## Implementing a Custom Backend

Implement all six methods of the `AgentBackend` interface. Add a compile-time check:

```go
var _ acp.AgentBackend = (*MyBackend)(nil)
```

For stateless backends, return `&UnsupportedOperationError{}` from `Spawn`, `Status`, and `Close`.

For session-based backends, track sessions in a `sync.Map` and implement the full lifecycle.

### Minimal stateless example

```go
type MyBackend struct {
    defaultModel string
}

func (b *MyBackend) Spawn(ctx context.Context, req *acp.SpawnRequest) (*acp.Session, error) {
    return nil, &acp.UnsupportedOperationError{Backend: b.Type(), Operation: "spawn"}
}

func (b *MyBackend) Prompt(ctx context.Context, req *acp.PromptRequest) (*acp.PromptResponse, error) {
    // Call your LLM API here
    return &acp.PromptResponse{
        Content: []acp.ContentBlock{{Type: "text", Text: "response"}},
        Model:   b.defaultModel,
    }, nil
}

func (b *MyBackend) Status(ctx context.Context, sessionID string) (*acp.SessionStatus, error) {
    return nil, &acp.UnsupportedOperationError{Backend: b.Type(), Operation: "status"}
}

func (b *MyBackend) Close(ctx context.Context, sessionID string) error {
    return &acp.UnsupportedOperationError{Backend: b.Type(), Operation: "close"}
}

func (b *MyBackend) Capabilities() acp.BackendCapabilities {
    return acp.BackendCapabilities{} // all false
}

func (b *MyBackend) Type() string {
    return "mybackend"
}
```

## Model Resolution Chain

When a prompt or spawn request is made, the model is resolved in this order:

1. **Request model** -- `req.Model` field on `SpawnRequest` or `PromptRequest`
2. **Project default** -- `default_model` set during project registration
3. **Backend default** -- `DefaultModel` from backend configuration (e.g., `ACPBackendConfig.DefaultModel`)
4. **Global default** -- server-level default model configuration

The first non-empty value wins. If no model is resolved, stateless backends like Ollama return an `InputValidationError`.

## Configuration Example

```yaml
orchestration:
  backends:
    claude:
      type: acp
      command: claude
      args: ["--agent"]
      default_model: "claude-sonnet-4-20250514"

    codex:
      type: acp
      command: codex
      args: ["--agent"]
      default_model: "o3"

    opencode:
      type: acp
      command: opencode
      args: []
      default_model: "anthropic:claude-sonnet-4-5-20250514"

    ollama:
      type: ollama
      endpoint: "http://localhost:11434"
      default_model: "llama3"

  global_default_model: "claude-sonnet-4-20250514"
```

## Session States

Sessions progress through these states:

| State | Description |
|-------|-------------|
| `active` | Session is processing a request. |
| `idle` | Session is waiting for input. |
| `resumable` | The record survived a restart; the process is recreated on the next prompt. |
| `closed` | Session has been terminated. |
| `error` | Session encountered an unrecoverable error. |

## Session Reconciliation

A session RECORD is durable (`orch/session/{tenant}/{session}`); a session
PROCESS is not. Reconciliation is what keeps the two from disagreeing: every
way a session can stop now ends in the same terminal transition, and every
closed record says why.

### Conclusion reasons

`SessionRecord.ConclusionReason` is written with the terminal state, in one
atomic store mutation (`AgentStore.ConcludeSession`), and is never overwritten
— the first ending owns the attribution.

| Reason | Written by |
|--------|-----------|
| `operator` | `CloseSession`, including on a stale record with no process behind it. |
| `process-exit` | The supervisor's exit observer (`RecordSessionExit`). |
| `reconcile` | A process found already gone when its record was persisted. |
| `node_restart` | Startup reconciliation: the record cannot be re-attached. |
| `job_concluded` | The job the session worked for reached a terminal state. |
| `agent_gone` | The daemon the session is pinned to unregistered or fell silent. |
| `session_idle` | The session is bound to no job, is not kept, and stayed quiet past the idle bound. |

### The four routes into it

1. **Startup.** `LoadPersistedSessions` walks every record once and asks
   locality FIRST.

   A **remote** record (`BackendLocal == false`) names a process on another
   host that this node's restart did not touch, and it is reached through the
   remote resolver rather than through the local backend table — so "absent
   from my backends" says nothing about it. It is concluded only on POSITIVE
   evidence that its daemon has left: the resolver cannot reach the backend
   AND the fleet registry has no registration for its `HostAffinity`, in which
   case the reason is `agent_gone`, not `node_restart`. Otherwise it is
   restored live. At boot the registry question is PRESENCE, not heartbeat
   recency: every registration predates the restart, so a live peer
   necessarily looks silent until it checks in, and the sweep asks the
   stronger question later.

   A **local** record's process died with the previous daemon. One whose
   backend is no longer registered, or that is not persistent, has no process
   to come back to and is concluded `node_restart`; a persistent one is
   restored `resumable`.
2. **Job conclusion.** `finishJob` (every terminal path of a run — completed,
   failed, aborted, panicked) and `RecordJobDeath` (the reaper's path for a job
   whose node died) conclude every session still bound to the job. The binding
   is read from the DURABLE records, not from the `JobRun`, so it survives a
   run that never reached its discard hook.
3. **Agent unregister.** `RemoveFleetNode` concludes every session whose
   `HostAffinity` names the removed registration (by ID, name or host).
4. **The maintenance sweep.** `Sweep` runs the reaper, the stall detector and
   then `SweepSessions`, on the same ticker (a third of the lease TTL).

### Which records a pass can see

The durable orchestration KV **routes every read to the tenant on the
context** (`pkg/server/tenant_kvstore.go`). Boot recovery and the maintenance
sweep both run on contexts that carry no principal, so a single cross-tenant
scan would see the system tenant's records and nothing else — every tenant's
session left neither restored nor reconciled.

Both passes therefore enumerate through `TenantLister`, the same seam the task
dispatcher and the task-source poller already use, and visit each record under
a context scoped to its owner (`agentServiceImpl.eachSessionRecord`). The
system tenant is always visited, exactly once. One tenant that cannot be
listed — a sealed barrier — is logged and skipped rather than ending the walk.
A service over a single, unrouted store (embedded, in-memory) needs no lister
and takes one scan.

The transcript beside each record follows the same rule from the other side:
`logTenant` substitutes the system tenant for an absent one INSIDE the log
implementations, so a conclusion closing a log it addresses by the caller's raw
tenant and a spawn that filed it under the storage tenant meet on one key.

### What the sweep will and will not do

The sweep needs **two** signals and never acts on the clock alone:

- the session has been quiet for longer than the LONGEST job idle bound
  (`JobIdleThresholds.longest()`), **and**
- its job is in a terminal state (`job_concluded`), or the daemon it is pinned
  to is unregistered or has not heartbeated within
  `orchestration.heartbeat_timeout` (default 90s) — `agent_gone`, or it is
  bound to no job at all and nothing keeps it — `session_idle`.

Both corroborating reads are fail-closed: a job status that cannot be read is
not a terminal job, and a registry that cannot answer means nothing is gone. A
quiet session WITH a job is left alone until its job ends, because a
long-running agent that has not spoken for an hour is what a long-running agent
looks like.

#### Sessions bound to no job

A job-less session has no job that will ever conclude it, so the sweep ends it
on the idle bound alone (#843). The bound is the same longest job idle bound;
when every job state is switched off there is no bound and nothing is
concluded on silence. The finding reads, for example:

```
session has been quiet for 59h55m0s, past the idle bound of 3h0m0s, and is bound to no job
```

A job-less session is left alone when:

- `SessionRecord.Keep` is set — the operator's explicit keep mark. The
  absence of a job is never read as a wish to keep a session;
- it is this node's lead session (`orchestration.lead_agent`), which the node
  holds by configuration;
- its status is `active`, a turn in flight;
- it is quiet for less than the bound, or already closed.

This applies to `resumable` records too: the resumable refusal below exists to
protect a session an operator is keeping, and a job-less one is kept only by
saying so.

**The stored record decides.** The verdict rests on the record's own fields —
quiet time, keep mark, job binding — so it is asked again of the record the
store holds when the ending is about to be written (`sessionStillDue`), under
the same per-session lock as every other absent-session conclusion. A session
whose record shows a prompt, a keep mark or a job binding written after the
sweep listed it is not concluded and produces no finding.

A prompt in flight is not yet on the record. `PromptSession` refuses a
concluded session before the turn, but the turn writes the record (its
activity stamp) only once the backend has answered, so a sweep that re-reads
the record during a turn still sees it quiet and can conclude it mid-turn.
Closing that window needs the conclusion to be claimed before its teardown,
which the conclusion protocol does not do today: the record is its last write.

**Keep is set at spawn.** `SpawnSessionRequest.keep` (proto field 35, JSON
`keep`) is persisted on the record as `SessionRecord.Keep`. It is wired on
every transport, the MCP spawn tool, the CLI (`orchestrator spawn --keep`) and
the TypeScript SDK. It only changes job-less reconciliation: a session bound
to a job still ends with its job. No verb sets it on an existing session.

The in-repo job-less spawners decide it as follows:

- the Signal bridge's chat spawn (`pkg/signalbridge/bot.go`) is kept: a chat
  session lives between the operator's messages until they close it;
- the worker-image smoke test (`pkg/sdlc/workerimage/smoke.go`) is not: it
  is a probe, and an abandoned probe should be reaped;
- the node's lead session (`pkg/server/lifecycle_agents.go`) needs no mark: it
  is excluded as the lead;
- CLI, MCP, REST, gRPC and SDK spawns are kept only when the caller asks.

### One conclusion, all of it or none

**The closed record is the commit point, and it is written LAST.** A
conclusion owes three durable things — the log's terminal transition, the
job's tidy-up, and the session record's terminal state — and they are written
in that order by `finishDurable`:

1. shut the log's append gate and write the single `session_closed` event;
2. drop the session from `JobStatus.SessionIDs` and append the
   `session_concluded` audit line;
3. `AgentStore.ConcludeSession` — status and reason, together.

Any failure before step 3 leaves the record NOT closed, which is the state
every later pass retries; steps 1 and 2 are idempotent, so the resuming pass
repeats work rather than duplicating it.

**Only a genuine absence is vacuously complete.** Because the closed record is
believed, every step before it must distinguish "there is nothing to do" from
"I could not find out". A session with no log metadata, a job that was never
persisted or has been reaped, a session record that is gone — those are
answers, and the step is done. A read that timed out, a store that is
unavailable, a body that will not unmarshal — those are failures to ANSWER,
and they stop the conclusion before the commit so the next pass retries.
`jobStatusAbsent` draws that line for the job record; `errors.Is(err,
ErrAgentStoreNotFound)` draws it for the session record; `ErrSessionLogNotFound`
draws it for the transcript. That makes `status == closed` mean
"complete" by construction, so the cheap test every reader already performs is
a correct completeness test and no auxiliary scan has to exist to find
half-finished endings.

The order used to be the other way round, and the failure was unrepairable: a
record written closed over a log that then failed to close was skipped by
every subsequent pass, because a pass reads the record, sees closed and
returns. The inverse window — a record still `active` whose log is already
closed — is the harmless one: the process is already gone, the shut gate
refuses further events, and the next pass finishes the record. A false
`active` is retried; a false `closed` is believed.

### One conclusion, never two

Reconciliation never becomes a second owner of an ending:

- A record this process holds LIVE is concluded through
  `session_conclusion.go`'s compare-and-swap, as a `conclusionNudge` — losing
  to an operator's close or an exit observer is the correct outcome.
- A record it does NOT hold live goes through `concludeAbsentSession`: a
  per-session lock, a re-read of the record under it, and a store write that
  refuses to overwrite an existing conclusion.

Each conclusion is also recorded on the job that spawned the session — a
`session_concluded` audit event on `JobStatus.Events`, and the session dropped
from `JobStatus.SessionIDs`, so a job never claims to hold a closed session.
This is part of the transition rather than of the reconciliation wrapper, so
an operator's close and a supervisor's exit observer owe the job the same
tidy-up that a sweep does.

### Filtering the list

`ListSessions` merges live entries with durable records, so an unfiltered call
returns closed sessions too. `ListSessionsRequest.Status` narrows it to one
lifecycle state; empty means every state, which is what a caller that predates
the filter sends. It is honoured identically on REST (`?status=`), gRPC
(`ListSessionsRequest.status`, field 3), MCP (`status`) and the embedded
transport.

The filter runs on the state the caller will SEE — after the durable `closed`
verdict overrides a live entry's assumption — so it never answers for a state
the listing does not report. An unrecognised value is **refused**
(`ErrSessionStateUnknown`) rather than matched against nothing: a filter the
engine silently misapplied reads as "there are no such sessions", which is the
wrong answer to a question that was never understood.

### Times on the wire

`SessionRecord.CreatedAt` and `LastActivity` are **Unix nanoseconds**. The
store stamps `CreatedAt` at write time when a caller supplies none, so no
record that exists can report the epoch, and `ListSessions` falls back to the
durable value when a live entry carries none. The gRPC mapping renders both
with `unixNanoTimestamp`; an unset instant is sent as an absent timestamp
rather than as 1970. Every session RPC uses it — spawn, prompt (synchronous
turn and async ack), status (including `last_activity`), close, and the list
and its entries — because each of those responses stamps `UnixNano` and each
was converted as seconds.

## Error Types

All backend operations return typed errors from the `acp` package:

| Error Type | Condition |
|------------|-----------|
| `SessionNotFoundError` | Session ID does not exist. |
| `TransportError` | Communication failure with subprocess. |
| `ProtocolError` | Invalid JSON-RPC response or ACP protocol violation. |
| `BackendNotFoundError` | Requested backend name is not registered. |
| `UnsupportedOperationError` | Operation not supported by this backend type. |
| `SpawnError` | Failed to start subprocess. |
| `PromptError` | Prompt execution failed. |
| `InputValidationError` | Missing or invalid request parameters. |


---

## Inference Backends

QuicBaaS supports three backend categories for AI inference: sessionless local servers (Ollama), session-based agent platforms (ACP: Claude Code, Codex, OpenCode), and HTTP API inference providers (Anthropic, OpenAI, xAI, etc.). The `APIInferenceBackend` interface abstracts API-based providers, enabling vendor-neutral integrations without importing provider SDKs.

### Backend Categories

| Category | Examples | Session Model | Authentication |
|----------|----------|---------------|-----------------|
| **Sessionless Local** | Ollama | Stateless HTTP requests | Optional (API key) |
| **Session-Based ACP** | Claude Code, Codex, OpenCode | Persistent agent sessions | OAuth2 + mTLS |
| **API Inference** | Anthropic, OpenAI, xAI | Stateless HTTP requests | Secret ref (OAuth, API key) |

**Note:** ACP backends belong in the Sessions and Workflows layer, not here. This package covers stateless HTTP API providers only.

### APIInferenceBackend Interface

All API inference providers must implement this contract, defined in `pkg/orchestration/inference/backend.go`:

```go
type APIInferenceBackend interface {
    // Provider returns the canonical provider identifier.
    // Examples: "anthropic", "openai", "xai"
    Provider() string
    
    // BaseURL returns the configured API endpoint.
    // Used for diagnostics; never used for routing.
    BaseURL() string
    
    // ListModels returns the catalog advertised by the provider instance.
    // An empty slice means "provider reachable but no models advertised".
    ListModels(ctx context.Context) ([]Model, error)
    
    // Generate executes a non-streaming completion and returns the full
    // assistant message plus token usage.
    Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
    
    // Stream executes a streaming completion. Implementations MUST close both
    // returned channels when the stream drains or context cancels.
    // The chunk channel emits GenerateChunk values; the error channel emits
    // at most one error then closes. An empty close indicates clean completion.
    Stream(ctx context.Context, req GenerateRequest) (<-chan GenerateChunk, <-chan error)
    
    // Close releases underlying resources (HTTP clients, goroutines, pools).
    // Safe to call exactly once.
    Close() error
}

type Model struct {
    ID       string  // Canonical identifier (e.g., "claude-opus-4-1")
    Name     string  // Display name
    Provider string  // Provider (echoed from Provider())
}

type GenerateRequest struct {
    Model    string         // Model ID
    Messages []ChatMessage  // Conversation context
    MaxTokens int          // Max completion tokens
    // ... other fields ...
}

type GenerateResponse struct {
    Content  string         // Full assistant message
    StopReason string       // "end_turn", "max_tokens", etc.
    InputTokens int         // Prompt token count
    OutputTokens int        // Completion token count
}

type GenerateChunk struct {
    Delta      string       // Incremental content
    FinishReason string     // Non-empty on stream end
}
```

### Registry & Factory Pattern

Providers are registered at init-time using a map-based dispatcher:

```go
type ProviderFactory func(ctx context.Context, cfg Config) (APIInferenceBackend, error)

type Registry struct {
    mu        sync.RWMutex
    factories map[string]ProviderFactory
}

// Register adds a provider to the registry.
func (r *Registry) Register(provider string, factory ProviderFactory) error { ... }

// New constructs a backend instance for a provider.
func (r *Registry) New(ctx context.Context, provider string, cfg Config) (APIInferenceBackend, error) { ... }
```

See `pkg/orchestration/inference/registry.go` for the package-level singleton.

### Adding a New Provider

Create one adapter file per provider in `pkg/orchestration/inference/`:

**Example: `pkg/orchestration/inference/anthropic.go`** (pseudocode, not shipped)

```go
package inference

import (
    "context"
    "net/http"
    "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func init() {
    // Register the factory with the default registry.
    defaultRegistry.MustRegister("anthropic", newAnthropic)
}

// anthropicBackend implements APIInferenceBackend.
type anthropicBackend struct {
    client  *http.Client
    baseURL string
    keyRef  service.SecretRef // Never embed raw API key
}

// newAnthropic constructs a backend from config.
func newAnthropic(ctx context.Context, cfg Config) (APIInferenceBackend, error) {
    // 1. Validate cfg
    if cfg.BaseURL == "" {
        cfg.BaseURL = "https://api.anthropic.com"
    }
    
    // 2. Resolve API key from secrets engine (never inline).
    keyValue, err := cfg.SecretResolver(ctx, cfg.APIKeyRef)
    if err != nil {
        return nil, err
    }
    
    // 3. Create HTTP client (reusable).
    client := &http.Client{
        Timeout: 30 * time.Second,
    }
    
    // 4. Return backend.
    return &anthropicBackend{
        client:  client,
        baseURL: cfg.BaseURL,
        keyRef:  cfg.APIKeyRef, // Store ref, not key
    }, nil
}

func (b *anthropicBackend) Provider() string { return "anthropic" }

func (b *anthropicBackend) BaseURL() string { return b.baseURL }

func (b *anthropicBackend) ListModels(ctx context.Context) ([]Model, error) {
    // Fetch model list from Anthropic API or return static catalog.
    return []Model{
        {ID: "claude-opus-4-1", Name: "Claude Opus 4.1", Provider: "anthropic"},
        {ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", Provider: "anthropic"},
    }, nil
}

func (b *anthropicBackend) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
    // POST to https://api.anthropic.com/v1/messages
    // Use keyRef to fetch key from secrets engine at call time.
    // Return GenerateResponse with token counts.
    return &GenerateResponse{...}, nil
}

func (b *anthropicBackend) Stream(ctx context.Context, req GenerateRequest) (<-chan GenerateChunk, <-chan error) {
    chunks := make(chan GenerateChunk)
    errs := make(chan error, 1)
    
    go func() {
        // Stream completion, emit GenerateChunk per delta, close on finish.
        defer close(chunks)
        defer close(errs)
        // ... streaming logic ...
    }()
    
    return chunks, errs
}

func (b *anthropicBackend) Close() error {
    b.client.CloseIdleConnections()
    return nil
}
```

### Credential Management

**Critical:** Never embed raw API keys in the backend struct. Always use `SecretRef`:

```go
type SecretRef struct {
    // SecretName is the key in the secrets store (e.g., "anthropic-api-key").
    SecretName string
    // Version is optional; defaults to latest.
    Version string
}

// Resolve at request time (not init time):
keyValue, err := cfg.SecretResolver(ctx, cfg.APIKeyRef)
if err != nil {
    return nil, err
}
```

This ensures:

1. Raw keys never appear in config files or logs
2. Key rotation doesn't require backend restart
3. Audit trail tracks all key access

### Configuration

Provider backends are configured in `pkg/config/inference.go`:

```go
type InferenceConfig struct {
    // Providers is a map of provider_id → provider configuration.
    Providers map[string]ProviderConfig
}

type ProviderConfig struct {
    Provider       string       // "anthropic", "openai", etc.
    BaseURL        string       // Override default endpoint (optional)
    APIKeyRef      SecretRef    // Reference to secrets store
    MaxRetries     int          // HTTP retry policy
    TimeoutSeconds int          // Request timeout
}
```

Example YAML:

```yaml
inference:
  providers:
    anthropic:
      provider: anthropic
      base_url: https://api.anthropic.com
      api_key_ref:
        secret_name: anthropic-api-key
        version: latest
      max_retries: 3
      timeout_seconds: 30
```

### Current Status

**No providers are shipped with QuicBaaS.** The interface and registry exist to enable fast integration. To add a provider:

1. Implement `APIInferenceBackend` in `pkg/orchestration/inference/PROVIDER.go`
2. Call `Register()` from the file's `init()`
3. Add configuration to the YAML
4. Test via `pkg/orchestration/inference/PROVIDER_test.go`

### UI Integration

The QuicBaaS Inference view shows a message when no backends are configured:

> No provider configured for this backend — see Settings → Inference Providers

Once a provider is configured, the view auto-discovers models via `ListModels()` and allows users to select a model for generation tasks.

**Note:** ACP backends (Claude Code, Codex) appear in Sessions/Workflows, not the Inference view, because they have persistent session semantics. They are greyed out in the Inference view if presented.

### Error Handling

Adapters should return typed errors from `pkg/orchestration/inference/errors.go`:

```go
type ProviderUnreachableError struct {
    Provider string
    Err      error
}

type InvalidModelError struct {
    Model string
    Err   error
}

type RateLimitError struct {
    RetryAfter time.Duration
}

// ... others ...
```

This allows callers to distinguish transient (retry) from fatal (give up) errors.

### Performance Considerations

- **HTTP clients**: Reuse across requests (connection pooling)
- **Context cancellation**: Respect context.Done() in Stream()
- **Token counting**: Deferred until response to save computation
- **Model caching**: Cache ListModels() result with TTL (30m default)

### Source Files

- `pkg/orchestration/inference/backend.go:34-74` — APIInferenceBackend interface
- `pkg/orchestration/inference/registry.go` — Registry and factory pattern
- `pkg/config/inference.go` — Configuration types
- `pkg/orchestration/inference/errors.go` — Typed errors
- `pkg/api/rest/handlers_inference.go` — REST endpoints for discovery

### Cross-References

- Session-based backends (ACP): `docs/orchestration/acp.md`
- Local model servers: `docs/orchestration/backends.md`
- Secrets management: `docs/secrets/api-reference.md`
- Configuration: `docs/configuration/workflow-settings.md`


---

## Sub-Job Spawning & Typed Artifacts

Sub-job spawning enables massively parallel agentic fan-out across machines. A lead agent in one job spawns multiple child jobs, each potentially running on different hosts with their own task graphs, and coordinates results via typed artifacts.

### Motivation

Traditional sequential workflows scale poorly when a task requires parallel exploration or distributed work. Sub-jobs let a lead agent:

- Spawn N independent job instances from within a running task
- Assign each to a different agent/host (based on affinity + availability)
- Let each solve a sub-problem in parallel
- Gather results via artifacts (commits, KV entries, files)

Example: "Build a trading system" → lead spawns three child jobs ("API service", "data pipeline", "analytics engine") in parallel, each delegated to a different specialized agent.

### Data Model

Job parent-child relationship is tracked in `pkg/orchestration/orchestrator_types.go`:

```go
type Job struct {
    // ... other fields ...
    
    // ParentJobID is empty for root jobs; set to the spawning job's ID for children.
    ParentJobID string `json:"parent_job_id,omitempty"`
    
    // ChildJobIDs accumulates IDs of jobs spawned by this job.
    // Appended atomically as children are created.
    ChildJobIDs []string `json:"child_job_ids,omitempty"`
}

type SpawnChildJobRequest struct {
    ParentJobID       string   // required: job whose lead agent is spawning
    WorkflowID        string   // required: which workflow to run for the child
    ProjectID         string   // required: project context
    Tasks             []Task   // required: initial task graph for the child
    HostAffinity      string   // optional: preferred host
    GPURequired       bool     // optional: GPU machine only
    HostAffinityStrict bool    // optional: fail if affinity not available
}
```

### Spawning a Child Job

Only the **registered lead agent** of `parent_job_id` may call `SpawnChildJob`. This is enforced via mTLS-bound OAuth2 scope checks. The orchestrator verifies:

1. Caller's certificate matches the lead agent ID
2. Lead agent has valid OAuth2 token scoped to `orchestrator:write`
3. Parent job exists and is in a valid state (not failed, not finished)

#### gRPC RPC

```protobuf
rpc SpawnChildJob(SpawnChildJobRequest) returns (SpawnChildJobResponse) {}

message SpawnChildJobRequest {
    string parent_job_id = 1;           // UUID of the parent job
    string workflow_id = 2;             // workflow template to run
    string project_id = 3;              // project owning the parent job
    repeated Task tasks = 4;            // initial task graph
    string host_affinity = 5;           // preferred agent host (optional)
    bool gpu_required = 6;              // GPU-only constraint (optional)
    bool host_affinity_strict = 7;      // fail if affinity unavailable (optional)
}

message SpawnChildJobResponse {
    string job_id = 1;                  // UUID of newly spawned child job
    string parent_job_id = 2;           // echoed from request
    int64 spawned_at_unix = 3;          // creation timestamp
}
```

#### Client Code Example

From a running task (agent-side), spawn a child job:

```go
// Inside an agent's task handler
resp, err := client.SpawnChildJob(ctx, &pb.SpawnChildJobRequest{
    ParentJobID:       parentJobID,
    WorkflowID:        "data-processing",
    ProjectID:         projectID,
    Tasks: []*pb.Task{
        {
            Subject:        "Process shard 1",
            SubjectDetails: "Process data shard from row 0-1000",
            RequiredBackend: "claude",
        },
    },
    HostAffinity:       "gpu-host-02",
    GPURequired:        true,
    HostAffinityStrict: false,
})
if err != nil {
    return fmt.Errorf("spawn child: %w", err)
}
childJobID := resp.JobID
```

### Artifacts

Artifacts are typed outputs/inputs passed between tasks and jobs. Five artifact kinds support different storage backends:

| Kind | Storage | Use Case |
|------|---------|----------|
| `inline` | Task record | Small blobs (≤ 1 MiB) |
| `file` | Agent filesystem | Large files (path + agent_id) |
| `commit` | Git repository | Code snapshots (SHA + branch) |
| `kv_key` | QuicBaaS KV store | Auto-promoted large inlines |
| `url` | External (S3, HTTP) | Third-party resources |

#### Artifact Data Model

From `pkg/orchestration/artifact.go`:

```go
type ArtifactKind string

const (
    ArtifactKindInline  ArtifactKind = "inline"
    ArtifactKindFile    ArtifactKind = "file"
    ArtifactKindCommit  ArtifactKind = "commit"
    ArtifactKindKVKey   ArtifactKind = "kv_key"
    ArtifactKindURL     ArtifactKind = "url"
)

type Artifact struct {
    Kind     ArtifactKind   // Required: kind classifier
    Name     string         // Required: unique within task
    MIMEType string         // Optional: e.g., "text/plain"
    Size     int64          // Byte count
    Content  []byte         // Inline bytes when Kind=inline
    Ref      ArtifactRef    // For non-inline kinds
}

type ArtifactRef struct {
    AgentID string // For file-kind: agent owning the file
    Path    string // Filesystem path, KV key, or full URL
    SHA     string // Content hash or git commit SHA
    Branch  string // Git branch (commit-kind only)
}
```

#### Inline Limit & Auto-Promotion

By default, artifacts larger than 1 MiB are automatically promoted from `inline` to `kv_key` storage:

```go
const InlineLimit int64 = 1 << 20 // 1 MiB
```

This is configurable at orchestration service setup:

```go
cfg := &orchestration.Config{
    Artifacts: orchestration.ArtifactConfig{
        InlineLimit: 5 << 20, // 5 MiB
    },
}
```

When an inline artifact exceeds the limit, the orchestrator:

1. Stores content in KV under a project-scoped key
2. Updates the artifact record: `Kind=kv_key`, clears `Content`, sets `Ref.Path`
3. Task record size remains bounded

#### Artifact REST API

```
GET /api/v1/tasks/{task_id}/artifacts
200 OK
[
  {
    "name": "analysis-result",
    "kind": "inline",
    "mime_type": "application/json",
    "size": 2048,
    "content": "..."
  },
  {
    "name": "compiled-binary",
    "kind": "file",
    "mime_type": "application/octet-stream",
    "size": 45678901,
    "ref": {
      "agent_id": "agent-prod-01",
      "path": "/tmp/output/binary"
    }
  }
]

GET /api/v1/tasks/{task_id}/artifacts/{name}
200 OK
{
  "name": "analysis-result",
  ...
}
```

#### Artifact MCP Tools

Two tools expose artifact operations:

| Tool | Purpose |
|------|---------|
| `task_list_artifacts` | List all artifacts for a task |
| `task_get_artifact` | Retrieve a single artifact by name |

Both enforce project-level authorization. Inline artifacts return full content; non-inline return references.

### Cross-Machine Handoff Pattern

1. **Parent job (lead)** spawns child job on remote host
2. **Child agent** executes tasks, produces commit artifact (e.g., Git push to `feature/child-work`)
3. **Parent agent** reads commit artifact reference from child's completed task
4. **Parent agent** uses SHA to fetch code context on next phase (post-rebase merge, testing, etc.)

Example sequence:

```
Parent Job (Host A)
  ├─ Spawn Child Job → "Implement API endpoints" (Host B)
  │   └─ Child Job completes, produces Artifact(name="code", kind="commit", sha="abc123...")
  │
  └─ Parent reads Artifact(name="code") from child task
      └─ Gets ref.SHA = "abc123", checks out and integrates code
```

### Service Implementation

The `SpawnChildJob` RPC is implemented in `pkg/orchestration/orchestrator.go`. The service:

1. Validates caller is the registered lead agent (via mTLS OAuth2 scope)
2. Checks parent job state (must be running or waiting)
3. Runs scheduler to pick a matching agent (affinity + GPU filter)
4. Creates child job record with `ParentJobID` set
5. Appends child job ID to parent's `ChildJobIDs` slice
6. Returns child job ID to caller

Error scenarios:

- `NoMatchingFleetNodeError`: scheduler found no available agent matching constraints
- `InvalidParentJobError`: parent job not found or in terminal state
- `UnauthorizedError`: caller is not the lead agent (mTLS/OAuth2 mismatch)

### Source Files

- `pkg/orchestration/orchestrator_types.go:891-894, 969-970` — Job parent-child fields, SpawnChildJobRequest
- `pkg/orchestration/artifact.go` — Artifact types and kinds
- `pkg/orchestration/orchestrator.go` — SpawnChildJob implementation
- `pkg/api/grpc/server_orchestrator.go` — gRPC RPC handler
- `pkg/api/rest/handlers_orchestrator.go` — REST endpoint wrapper
- `pkg/orchestration/spawn_child_job_test.go` — Usage examples and tests


---

## Auto-Spawn Tasks

Auto-spawn is a task mode that immediately dispatches a job to an available agent, bypassing the manual queue. Paired with job-to-task status mirroring, it enables lightweight fire-and-forget work patterns.

### Two Task Modes

Tasks can run in one of two modes:

| Mode | Behavior | Use Case |
|------|----------|----------|
| **Queued** (default) | Task waits in queue until an agent claims it | Interactive workflows, multi-step orchestration |
| **Auto-spawn** | Task immediately dispatches as a spawned job | One-off commands, parallel batch work |

### Auto-Spawn Data Model

From `pkg/orchestration/orchestrator_types.go`:

```go
type Task struct {
    // ... other fields ...
    
    // AutoSpawn, when true, instructs the orchestrator to spawn a job for
    // this task immediately rather than queue it for claim.
    AutoSpawn bool `json:"auto_spawn,omitempty"`
    
    // SpawnedJobID is populated when a task is auto-spawned. Ignored when
    // AutoSpawn is false.
    SpawnedJobID string `json:"spawned_job_id,omitempty"`
    
    // SpawnedAt records when the task's job was dispatched.
    SpawnedAt *time.Time `json:"spawned_at,omitempty"`
}
```

### Dispatch Flow

When the scheduler encounters an auto-spawn task:

1. **Scheduler runs** (interval-based, default 5s)
2. **Filters** matching agents:
   - Host affinity (if set, filter to that host)
   - GPU requirement (if set, filter to GPU-capable agents)
3. **Picks an agent** from the filtered set (round-robin or by load)
4. **Calls `StartJob`** on the selected agent
5. **Populates `SpawnedJobID`** with the spawned job ID
6. **Task status** moves `pending -> in_progress`, in the same claim that
   records the job (`ClaimTaskSpawn`), and back to `completed` / `failed` /
   `blocked` when the job concludes. There is no `dispatched` status: the job
   the engine started IS the claimant (go-quicbaas#966)

#### The project's concurrency quota

An auto-spawn is checked against `Project.Quotas.MaxConcurrency` before the
attempt is claimed (`agentServiceImpl.checkProjectConcurrency`). It used not to
be: the quota was enforced only inside the poller and dispatcher sweeps, so a
caller in a loop could hand a project capped at two twenty concurrent jobs
(go-quicbaas#898).

A refusal is `ProjectCapacityExceededError` — the same type the capacity lease
store raises, so the same retryable `project_capacity_exceeded` fault class and
retry-at apply. The task is still created and still `pending`, carrying the
refusal as its dispatch reason, and the dispatcher's next sweep starts it once a
slot frees. No attempt is charged, which is why the check precedes
`ClaimTaskSpawn`. Details and the full enforcement table:
[autonomy-and-dispatch.md](autonomy-and-dispatch.md).

#### Failure Modes

If no matching agent is available:

- The task is persisted with `SpawnedJobID` empty
- Status remains `queued`
- Scheduler retries on next interval
- No error is logged (graceful degradation)

If all matching agents are unavailable and `HostAffinityStrict=true`:

- Task transitions to `failed` status
- Error reason: `NoMatchingFleetNodeError`

### Status Mirroring

When a spawned job completes or fails, the parent task's status is automatically updated:

| Spawned Job Status | Task Status |
|--------------------|-------------|
| `succeeded` | `succeeded` |
| `failed` | `failed` |
| `cancelled` | `cancelled` |

This mirroring happens via the job event stream. The orchestrator subscribes to job completion events and flips the parent task's status accordingly.

### CLI Usage

Create and auto-spawn a task immediately:

```bash
# Create and dispatch task to run "fix bug" on an available agent
quicbaas task create \
  --subject "fix bug" \
  --auto-spawn \
  --backend ollama \
  --gpu

# With host affinity (strict)
quicbaas task create \
  --subject "process shard 42" \
  --auto-spawn \
  --host-affinity gpu-host-02 \
  --host-affinity-strict

# List only auto-spawned tasks
quicbaas task list --filter auto-spawned

# List queued (non-spawned) tasks
quicbaas task list --filter queued

# List all tasks regardless of spawn mode
quicbaas task list --filter all
```

### UI Integration

The QuicBaaS UI (Svelte) includes auto-spawn controls:

#### CreateTaskDialog

- **"Run immediately"** toggle switches between queued and auto-spawn modes
- When enabled, task dispatches on submit

#### TasksView

- **Filter buttons**: "All", "Auto-spawned", "Queued"; `#/tasks?project=<id>` narrows to one project
- **Columns**: Subject (with the forge issue `#N` from `source`) · Project · Status (blocked marker + `blocked_reason`, and a stop badge from `fault_origin` / `block_kind` — "Environment fault: <class>", "Failed on the work", "Node lost", "Awaiting owner ruling") · Attempts (`charged / budget (N dispatched)` from the server's `charged_attempts` and `effective_budget`) · Assigned · Job (`#/jobs/{spawned_job_id}`) · Updated
- A row opens `#/tasks/{id}` (`TaskDetailView`), which also lists prior attempts through `job.config.task_id`, `retry_not_before`, and `output_artifacts` of kind `pull_request`
- The pure helpers live in `src/lib/tasks.ts` (`budgetLabel`, `chargedAttempts`, `blockedReason`, `isWaitingOnPerson`, `issueRef`, `loadErrorMessage`, `taskStopKind`, `taskStopPresentation`)

### REST API

#### Create Task with Auto-Spawn

```
POST /api/v1/tasks
Content-Type: application/json

{
  "subject": "fix bug",
  "auto_spawn": true,
  "backend": "ollama",
  "gpu_required": true
}

201 Created
{
  "id": "task-uuid",
  "subject": "fix bug",
  "auto_spawn": true,
  "spawned_job_id": "job-uuid",
  "spawned_at": "2025-04-26T12:00:00Z",
  "status": "dispatched"
}
```

#### List Tasks with Filter

```
GET /api/v1/tasks?filter=auto-spawned&project_id=proj-uuid

200 OK
{
  "tasks": [
    {
      "id": "task-1",
      "auto_spawn": true,
      "spawned_job_id": "job-1",
      "status": "dispatched"
    },
    ...
  ],
  "total": 42
}
```

Valid filter values: `all`, `auto-spawned`, `queued`.

### gRPC API

```protobuf
rpc CreateTask(CreateTaskRequest) returns (CreateTaskResponse) {}

message CreateTaskRequest {
    string subject = 1;
    bool auto_spawn = 2;              // Enable immediate dispatch
    string backend = 3;               // "ollama", "claude", etc.
    bool gpu_required = 4;            // GPU-only constraint
    string host_affinity = 5;         // Preferred host
    bool host_affinity_strict = 6;    // Fail if unavailable
}

message CreateTaskResponse {
    string task_id = 1;
    string spawned_job_id = 2;        // Populated if auto_spawn=true
    int64 spawned_at_unix = 3;
}

rpc ListTasks(ListTasksRequest) returns (ListTasksResponse) {}

message ListTasksRequest {
    string filter = 1;                // "all", "auto-spawned", "queued"
    string project_id = 2;            // Filter by project
}
```

### Service Implementation

The `TaskService` in `pkg/service/task_service.go` handles auto-spawn logic:

1. **On create**: If `AutoSpawn=true`, mark task for immediate dispatch
2. **Scheduler run**: For each auto-spawn task, execute the dispatch flow above
3. **On job completion**: Trigger status mirror via event listener
4. **Error handling**: Log retry attempts without blocking; use exponential backoff

All mutations are audited. See `docs/security/audit.md` for event types.

### Configuration

Auto-spawn behavior is tuned in `Config.Orchestration`:

```go
type OrchestrationConfig struct {
    // ... other fields ...
    
    // TaskSchedulerInterval is how often the scheduler checks for
    // auto-spawn tasks and other work. Default: 5 seconds.
    TaskSchedulerInterval time.Duration
    
    // AutoSpawnRetryMax is the maximum number of retry attempts before
    // giving up on an auto-spawn task. Default: 10.
    AutoSpawnRetryMax int
    
    // AutoSpawnRetryBackoff is the backoff strategy between retries.
    AutoSpawnRetryBackoff time.Duration
}
```

### Cross-References

- Sub-job spawning: `docs/orchestration/sub-jobs.md`
- Job status lifecycle: `docs/orchestration/state-machine.md`
- Scheduler architecture: `docs/scheduler/architecture.md`
- Host affinity: `docs/orchestration/multi-host.md`

### Source Files

- `pkg/orchestration/orchestrator_types.go:783-792` — Task auto-spawn fields
- `pkg/orchestration/orchestrator.go` — Scheduler and dispatch logic
- `pkg/orchestration/auto_spawn_test.go` — Usage examples and tests
- `pkg/api/rest/handlers_orchestrator.go` — REST endpoints
- `pkg/api/grpc/server_orchestrator.go` — gRPC endpoints


---

## Task Tracking and Decomposition

The orchestrator decomposes high-level tasks into discrete work items using an AI agent backend. This enables multi-project planning where a complex objective is broken into trackable, dependency-aware units of work.

### Task Decomposition

`quicbaas_orchestrator_decompose` sends a structured prompt to any registered fleet node backend, asking it to break a task into work items.

#### Request

```json
{
  "backend": "claude",
  "task": "Add WebSocket support to the API gateway and update all client SDKs",
  "model": "claude-sonnet-4-20250514",
  "project_ids": ["proj-abc", "proj-def"],
  "use_rag": true
}
```

Required fields: `backend`, `task`. The optional `project_ids` array scopes the decomposition to specific registered projects, and `use_rag` enables RAG context enrichment.

#### How It Works

1. `DecomposeTask` validates the request and looks up the backend
2. `buildDecompositionPrompt` constructs a structured prompt that includes the task description and target project IDs
3. The backend receives a system prompt constraining it to respond with valid JSON
4. The response is parsed via `parseWorkItems`, which extracts the first JSON array from the response text
5. If JSON parsing fails, a single "raw" work item is returned containing the unparsed response

### WorkItem Structure

```go
type WorkItem struct {
    ID           string   `json:"id"`
    ProjectID    string   `json:"project_id"`
    Title        string   `json:"title"`
    Description  string   `json:"description"`
    Priority     int      `json:"priority"`
    Dependencies []string `json:"dependencies,omitempty"`
}
```

| Field | Description |
|-------|-------------|
| `id` | Unique identifier for the work item |
| `project_id` | Target project this work item belongs to |
| `title` | Short summary of the work |
| `description` | Detailed description of what needs to be done |
| `priority` | Numeric priority (1 = highest) |
| `dependencies` | IDs of work items that must complete first |

### Example Response

```json
{
  "work_items": [
    {
      "id": "ws-1",
      "project_id": "proj-abc",
      "title": "Implement WebSocket handler",
      "description": "Add WebSocket upgrade support to the API gateway router",
      "priority": 1,
      "dependencies": []
    },
    {
      "id": "ws-2",
      "project_id": "proj-def",
      "title": "Update Go SDK with WebSocket client",
      "description": "Add WebSocket connection and message handling to the Go client SDK",
      "priority": 2,
      "dependencies": ["ws-1"]
    }
  ],
  "timestamp": "2026-03-16T10:00:00.000000000Z"
}
```

### Agent Cache

The agent cache tools allow sessions to store and retrieve expensive analysis results via the KV store, avoiding redundant computation across sessions.

#### Tools

| Tool | Description |
|------|-------------|
| `quicbaas_orchestrator_cache_put` | Store a value scoped by project ID and key |
| `quicbaas_orchestrator_cache_get` | Retrieve a cached value by project ID and key |

#### Storage

Cache entries are stored as KV entities with entity type `agent_cache`. The composite key is formed as `{project_id}:{key}` and hashed via FNV-1a to produce a deterministic uint64 ID. The KV store provides CAS (compare-and-swap) semantics through versioned writes, ensuring safe concurrent updates.

#### Example Usage

```
# Store a dependency analysis result
quicbaas_orchestrator_cache_put(
  project_id: "proj-abc",
  key: "dependency-analysis",
  value: "{\"direct\": 12, \"transitive\": 47, ...}"
)

# Retrieve it in another session
quicbaas_orchestrator_cache_get(
  project_id: "proj-abc",
  key: "dependency-analysis"
)
```

### Build Tags

Task decomposition is always compiled. Agent cache MCP tools require the `mcp` build tag.


---

## Resumable Sessions

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

### Session states

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

### The durable record

`SessionRecord` holds everything needed to address a session and to rebuild the
agent that served it: identity and tenant, backend and model, system prompt,
work directory, environment, extra CLI arguments, MCP server configuration,
sandbox mode and image, host affinity, the persistent flag, the pooled provider
account it authenticates as, and the lifecycle status.

It lives under the standard orchestration keyspace, so it inherits the same
durability, replication, tenant isolation and at-rest encryption as projects,
agents and tasks (see persistence.md):

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

#### The account pin

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
per-account cost attribution (see accounts.md).

`AccountStrategy` is retained as audit history — it is what lets a cost anomaly
be traced back to the policy that produced it — and is deliberately *not*
replayed as a selection input.

A record with no account produces a request with no account, so a session that
never used the pool is unaffected.

### Respawn policy: lazy

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

### Resume mechanisms

`ACPBackendConfig.NativeSessionResume` (operator config:
`orchestration.claude_native_session_resume`) selects the mechanism.

#### Replay (default)

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

#### Native

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

### Replay bound

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

### No silent fresh starts

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

### What a successful resume records

- Durable record: `status=active`, `resume_mode=native|replay`,
  `resume_error` cleared, `last_activity` stamped.
- Transcript: a `session_resumed` event naming the mode and, for a replay, how
  many turns were replayed and how many omitted.
- Session metadata (the attach surface): status returned to `active`.

Concurrent prompts against one resumable session are serialised per session, so
two callers arriving together produce exactly one replacement process.

### Scope: local sessions only

A session dispatched to a remote fleet node daemon is owned by that daemon's
process, not by the control plane's. When the control plane restarts, the
session record is restored as `active` and follow-up prompts route to the same
daemon by host affinity — which is correct, because the agent there is still
running.

If the **remote daemon** restarts, its own in-process registry is lost and the
prompt fails with `acp: session not found` from that host. Reconstituting a
session on a remote daemon needs the same durable record and resume path inside
`pkg/fleet`'s runtime; that is not implemented. Until it is, a remote daemon
restart still ends its sessions.

### Related

- persistence.md — the KV keyspace and tenant isolation rules
  session records inherit.
- async-turns.md — the turn contract a resumed session serves.
- recovery.md — crash recovery and auto-restart for workflows.


---

## Session Attach & Event Log

Attach to any running orchestrator session to scroll back through its history and live-tail new events as they arrive. Sessions are tenant-scoped; operators see only their tenant's events.

### Two views of one transcript

There is exactly ONE store — the durable, tenant-routed session event log —
and two projections of it. Choosing the wrong one is the difference between a
readable page and forty fragments of one answer.

| | Events (`/events`, `SubscribeSessionEvents`, `GetSessionHistory`) | Conversation (`/history`, `GetSessionConversation`) |
|---|---|---|
| Unit | one event | one turn |
| A streaming answer | many `assistant_chunk` events | one `assistant` turn |
| Carries | thoughts, tool arguments, tool output, file operations, presence | role, text, tool-use summary, model, stop reason, token usage |
| Live tail | yes (SSE / gRPC stream) | no — page it |
| Use it to | FOLLOW one turn to its terminal event | READ a session |

The conversation view is derived on read. Writing turn records alongside the
events would mean two records of one conversation that can disagree, and a
second keyspace to bound, retain and delete in step with the first.

### Tenant scoping of the transcript

The transcript is filed under `StorageTenant(ctx)`, exactly like the session
record beside it: a caller carrying no tenant — an in-process job runner, an
embedded consumer, a test — resolves to the system tenant on BOTH the write
and the read. The resolution happens in `AppendNextSessionEvent`, the single
write path, and in each log implementation's key builders, so the key a
transcript is stored under always matches the key it is read back from. An
event that names a tenant explicitly is left alone; a cross-tenant append is
still refused. (Before go-quicbaas#793 the producers stamped
`TenantFromContext`, which is empty for those callers, and the KV log refuses
an untenanted event — so an untenanted session's whole conversation was
dropped to a log line while the operator saw an empty page.)

### Storage bounds

| Bound | Value | Behaviour past it |
|---|---|---|
| `MaxSessionEventTextBytes` | 64 KiB per text field | Clipped on a rune boundary, `truncated_bytes` recorded on the payload, visible marker appended to the stored text |
| `MaxSessionEventArgsBytes` | 16 KiB of tool arguments | REPLACED with `{"truncated_bytes":N}` — structured input cannot be clipped mid-value and stay parseable |
| `DefaultSessionRetentionTurns` | 200 turns per session | Oldest WHOLE TURNS dropped; `oldest_sequence` on the metadata record and on every conversation page says where the surviving transcript starts |

Retention is set deployment-wide and per tenant:

```yaml
orchestration:
  session_history:
    max_turns_per_session: 200
    per_tenant_max_turns:
      acme: 0        # 0 keeps everything — a tenant under legal hold
      noisy-tenant: 50
```

It prunes by whole turns rather than by event count because half a turn is
worse than none of it: a transcript cut mid-turn shows an answer with no
question above it. The metadata record carries the sequence each retained turn
opened at, so dropping the oldest is a bounded delete rather than a scan.
Deleting a session deletes its transcript with it — a record removed while its
conversation stayed readable would leave an operator able to open a session
the system reports does not exist.

### CLI

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

### REST

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

The conversation view is its own route rather than a third negotiated mode:

```
GET /api/v1/orchestrator/sessions/{id}/history
  ?tenant_id=<tenant>      (defaults to the authenticated principal's tenant, then "system")
  &limit=<N>               (turns per page; default 100, capped at 500)
  &cursor=<seq>            (next_cursor from a previous page; 0 = start of the retained conversation)
```

The response is a `SessionHistoryPage` — `turns`, `next_cursor`,
`oldest_sequence` — and the cursor is echoed on the `X-Next-Cursor` response
header. Permission `orchestrator:status`, the same as `/events`.

The SDK method is `GetSessionConversation`, not `GetSessionHistory`: that name
was already taken by the raw-event pager. The route names the resource an
operator reaches for; the method names the shape it returns.

SSE frames carry `id: <sequence>`; browsers resume via `Last-Event-ID` on reconnect. Server honors `Last-Event-ID` over the `from` query param so reconnects never miss or duplicate events.

#### Long-Lived Streams

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

#### Client Resumption

The SDK REST reader (`sdk/go/transport/rest`) resumes a `follow` stream that
ends without an explicit server error. It re-opens at `from=<last sequence+1>`
with a matching `Last-Event-ID` header, so a resumed stream has no gap and no
duplicate. Retries are bounded at 5 attempts with exponential backoff from
250ms (about 8 seconds total); exhausting them yields a typed
`SessionStreamReconnectError` carrying the session, the last delivered
sequence, and the final failure. A server-sent `event: error` frame is
terminal and is never retried.

### gRPC

```proto
service OrchestratorService {
  rpc SubscribeSessionEvents(SubscribeSessionRequest)
    returns (stream SessionEventEnvelope);
}
```

Server-streaming RPC mirrors the SSE semantics. The `from_sequence` field drives replay; omit to start from the first event.

### Event Kinds

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
Asynchronous Agent Turns.

Between those brackets, backends stream `assistant_chunk`, `agent_thought`,
`tool_invocation`, and `tool_result` events into the transcript live as the
turn runs — an attached consumer watches the agent work rather than receiving
only the final response.

### At-Least-Once Delivery

Every event carries a monotonic per-session `sequence` starting at 1. The log rejects any append whose sequence is not exactly `latest+1` (`ErrSessionLogGap`). Consumers validate monotonicity; if a gap appears they can `Range()` the missing window to catch up.

### Operator Presence

Presence describes a person watching a session, not the connection carrying the
stream. `operator_joined` is written only when an operator's live-attachment
count for a session rises from zero, and `operator_left` only after it has
stayed at zero for the linger window (15s, `orchestration.DefaultPresenceLinger`).
A dropped-and-resumed stream, a browser tab reload, or a second tab therefore
leave the transcript untouched, while a genuine departure is still recorded.
Server shutdown commits every outstanding departure immediately so a restart
never leaves phantom operators in a transcript.

### Tenant Isolation

The log keys events on `(tenant_id, session_id)` and never leaks across tenants. Cross-tenant attach attempts return an empty stream rather than another tenant's data. At rest, event payloads are encrypted with the tenant's DEK; the session ID is mixed into the AEAD AAD so ciphertexts cannot be relocated across sessions.


---

## Asynchronous Agent Turns

Prompting an agent session is asynchronous on every transport. The server
records the turn, dispatches it in the background, and answers immediately with
an acknowledgement. The agent's output is delivered on the durable session event
log, which is the same stream `quicbaas orchestrator attach` and the web UI
already consume — see Session Attach & Event Log.

### Why

A real agent turn runs for minutes. No request/response transport can hold a
connection open that long, and QuicBaaS does not try: the REST server caps a
request at 25 seconds (`pkg/server/lifecycle_rest.go`). A synchronous prompt
contract therefore made real agent work structurally impossible — a prompt
asking an agent to read a directory and summarise it died with
`request timeout after 25s` before the agent had finished thinking.

Raising the cap only moves the failure. The turn is decoupled from the request
instead.

### Contract

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

#### Acknowledgement

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

#### Stateless prompts stay synchronous

A prompt that names a `backend` but no `session_id` is a stateless one-shot. It
has no event log to publish to, so the completed turn is returned inline
(`content` populated, no `status`). This is a different operation, not a
fallback: the two shapes travel on one wire type and are told apart by `status`.

### Per-transport surface

| Transport | Dispatch | Turn output |
|-----------|----------|-------------|
| REST | `POST .../sessions/{id}/prompt` → `202 Accepted` + ack | SSE at `GET .../sessions/{id}/events?follow=1&from={sequence}` |
| gRPC | `OrchestratorService/PromptSession` → ack in `PromptSessionResponse` (`status`, `turn_id`, `sequence`) | `OrchestratorService/SubscribeSessionEvents` server stream |
| QUIC | orchestrator prompt sub-op → ack payload | session event stream |
| Unix | gRPC over a Unix socket; identical to gRPC | identical to gRPC |
| Embedded | `svc.Agent().PromptSessionAsync` → ack | in-process `SessionEventLog.Subscribe` |
| MCP | `quicbaas_orchestrator_prompt` → ack | **poll** `quicbaas_orchestrator_session_history` |

#### MCP has no stream

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

### SDK

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

### CLI

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

### Ordering and concurrency

Every session event carries a monotonic per-session sequence, and the log
rejects an append that is not exactly `latest+1`. A session has several genuine
concurrent writers — background turn goroutines, other transports, and every
live attacher recording its presence — so sequence assignment goes through one
shared, retrying write path, `orchestration.AppendNextSessionEvent`. A turn's
events therefore appear in the transcript in this order:

```
prompt_submitted → turn_started → (assistant_chunk | agent_thought | tool_invocation | tool_result | file_op)* → turn_completed | turn_failed
```

### Shutdown

`WaitForTurns` blocks until every accepted turn has written its terminal event.
It is deliberately *not* wired into daemon shutdown: an agent turn runs for
minutes, and a shutdown that stalls that long is worse than a transcript that
resumes on restart. It exists so callers that need a quiescent log — tests above
all — have a synchronization point instead of a sleep.


---

## Recovery

Crash recovery and auto-restart configuration for orchestrated AI agent workflows.

Source: `pkg/orchestration/workflow_types.go`

### Overview

When an orchestrated AI agent (Claude Code, Codex, OpenCode) crashes or becomes unresponsive, the recovery system determines whether and how to restart it. Auto-restart is disabled by default to prevent runaway cost accumulation.

### Recovery Settings

```go
type RecoverySettings struct {
    AutoRestart    bool   `yaml:"auto_restart"`     // Enable auto-restart (default: false)
    MaxRestarts    int    `yaml:"max_restarts"`      // Maximum restart attempts (default: 3)
    RestartBackoff string `yaml:"restart_backoff"`   // Delay between restarts (default: 30s)
}
```

### Bootstrap Ordering

When recovering a workflow, components must be restored in the correct order:

1. **KV Store** -- Ensure the underlying storage is available
2. **Lease Manager** -- Reconnect or recreate lease state
3. **File Lock Service** -- Expired locks are cleaned up automatically via lease TTL
4. **Cost Tracker** -- Read current budget state from KV
5. **Peer Messaging Service** -- Restore inbox state from KV
6. **State Machine** -- Transition crashed agents to `error` state using `Force` flag
7. **Agent Sessions** -- Restart agents if auto-restart is enabled

This ordering ensures that safety checks (budget, locks) are available before any agent resumes work.

### Auto-Restart Logic

When an agent failure is detected:

```
Agent crash detected
    |
    v
[auto_restart enabled?] --no--> Mark agent as "error", stop
    |
    yes
    v
[restart_count < max_restarts?] --no--> Mark as "error", stop
    |
    yes
    v
[budget check: status != exceeded?] --no--> Mark as "error", stop
    |
    yes
    v
[lead agent healthy?] --no--> Mark as "error", stop
    |
    yes
    v
Wait restart_backoff duration
    |
    v
Restart agent session
    |
    v
Increment restart counter
```

#### Safety Conditions

Before restarting an agent, the system verifies:

1. **Budget check**: `CheckBudget()` must not return `BudgetExceeded`
2. **Lead health**: The lead agent must be in `ready` or `busy` state (not crashed itself)
3. **Restart count**: Must be below `MaxRestarts`

If any condition fails, the agent is left in `error` state for manual intervention.

### Configuration

```yaml
settings:
  recovery:
    auto_restart: false    # Default: off (explicit opt-in)
    max_restarts: 3        # Attempts before giving up
    restart_backoff: 30s   # Delay between restart attempts
```

#### Conservative (Production)

```yaml
recovery:
  auto_restart: false
```

#### Recommended

```yaml
recovery:
  auto_restart: true
  max_restarts: 3
  restart_backoff: 30s
```

#### Aggressive (Development)

```yaml
recovery:
  auto_restart: true
  max_restarts: 10
  restart_backoff: 5s
```

### State Machine Integration

On crash detection, the state machine transitions the agent:

- **MemberStatus**: `busy` -> `error` (using `Force` flag to bypass normal validation)
- **ExecutionStatus**: current state -> `error` (using `Force` flag)

On successful restart:

- **MemberStatus**: `error` -> `ready`
- **ExecutionStatus**: `error` -> `idle`


---

## Hooks Integration

### Overview

Claude Code fires hooks at key lifecycle events during agent team coordination. QuicBaaS exposes REST endpoints that receive these hook payloads, persist task state, and publish events to the messaging subsystem for cross-host coordination.

Three hook types are supported:

| Hook Event | Trigger | QuicBaaS Endpoint |
|---|---|---|
| `TaskCreated` | A teammate creates a new task | `POST /api/v1/hooks/task-created` |
| `TaskCompleted` | A teammate finishes a task | `POST /api/v1/hooks/task-completed` |
| `TeammateIdle` | A teammate has no work to do | `POST /api/v1/hooks/teammate-idle` |

### Configuration

#### `.claude/settings.json`

Configure hooks to POST to QuicBaaS. For local (standalone) mode, use `localhost`. For remote fleet nodes, use the QuicBaaS server address.

**Local hooks:**

```json
{
  "hooks": {
    "TaskCreated": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "http://localhost:9090/api/v1/hooks/task-created"
          }
        ]
      }
    ],
    "TaskCompleted": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "http://localhost:9090/api/v1/hooks/task-completed"
          }
        ]
      }
    ],
    "TeammateIdle": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "http://localhost:9090/api/v1/hooks/teammate-idle"
          }
        ]
      }
    ]
  }
}
```

**Remote hooks (agent on a different machine):**

```json
{
  "hooks": {
    "TaskCreated": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "https://quicbaas-server.internal:9090/api/v1/hooks/task-created"
          }
        ]
      }
    ]
  }
}
```

### Request/Response Format

#### HookPayload (request body)

All three endpoints accept the same payload shape. Required fields vary by hook type.

```json
{
  "hook_event_name": "TaskCreated",
  "task_id": "task_abc123",
  "task_subject": "Implement WebSocket transport",
  "task_description": "Add WebSocket upgrade handler in pkg/api/",
  "teammate_name": "backend-worker",
  "team_name": "platform-team",
  "session_id": "sess_x1y2z3",
  "cwd": "/home/user/sources/go-quicbaas"
}
```

**Required fields by hook type:**

| Field | TaskCreated | TaskCompleted | TeammateIdle |
|---|---|---|---|
| `hook_event_name` | required | required | required |
| `task_id` | required | required | -- |
| `team_name` | required | required | required |
| `teammate_name` | required | -- | required |

#### HookResponse (response body)

```json
{
  "exit_code": 0,
  "stderr": ""
}
```

- `exit_code: 0` (or omitted) -- allow the action to proceed.
- `exit_code: 2` -- block the action with feedback in `stderr`.

### Endpoint Behavior

#### POST /api/v1/hooks/task-created

1. Validates required fields (`hook_event_name`, `task_id`, `team_name`, `teammate_name`).
2. Creates a `Task` record in the distributed task store with status `pending`.
3. Publishes a `TaskCreated` event to messaging channel `orchestrator:tasks:<team_name>`.
4. Returns `exit_code: 0` to allow task creation.
5. If the task ID already exists, returns `exit_code: 2` with feedback.

#### POST /api/v1/hooks/task-completed

1. Validates required fields (`hook_event_name`, `task_id`, `team_name`).
2. Marks the task as `completed` in the task store with a `TaskResult` containing session and teammate metadata.
3. Publishes a `TaskCompleted` event to messaging channel `orchestrator:tasks:<team_name>`.
4. Returns `exit_code: 0`.
5. Returns 404 if the task ID is not found.

#### POST /api/v1/hooks/teammate-idle

1. Validates required fields (`hook_event_name`, `teammate_name`, `team_name`).
2. Queries the task store for pending tasks matching the team.
3. If pending tasks exist, returns `exit_code: 2` with `stderr: "New task available: <subject>"` to prevent the teammate from going idle.
4. If no pending tasks, returns `exit_code: 0` to allow idle.

### Tenant Resolution

The `X-Tenant-ID` request header sets the tenant namespace. If absent, the `team_name` field from the payload is used as the tenant ID.

### Error Handling

| Condition | HTTP Status | Response |
|---|---|---|
| Malformed JSON | 400 | Bad request message |
| Missing required field | 400 | Field-specific error |
| Task already exists (TaskCreated) | 200 | `exit_code: 2`, stderr message |
| Task not found (TaskCompleted) | 404 | Not found message |
| Task store not configured | 501 | Service not configured |
| Messaging not configured | 501 | Service not configured |
| Internal store failure | 500 | Processing error |

Messaging publish failures are logged but do not fail the hook response. The task is already persisted, so the hook returns success.

### Error Types

The hooks handler uses typed errors defined in `pkg/api/rest/handlers_hooks.go`:

- `HookPayloadError` -- invalid or missing required field.
- `HookProcessingError` -- task store or messaging failure (wraps cause for `errors.Is`/`errors.As`).
- `HookServiceNotConfiguredError` -- required service dependency (task store, messaging) is nil.


---

## File Locking

Lease-based distributed file locks for coordinating file access among orchestrated AI agents.

Source: `pkg/orchestration/file_lock.go`, `pkg/orchestration/file_lock_store.go`

### Overview

When multiple AI agents (Claude Code, Codex, OpenCode) work on the same codebase, file locking prevents destructive concurrent edits. The system uses CAS (compare-and-swap) semantics backed by the KV store, leases for automatic expiry, and optional Raft fencing tokens for split-brain protection.

### Lock Structure

```go
type FileLock struct {
    LockKey      string // Deterministic hash: filelock:{tenant}:{project}:{sha256(path)}
    AgentID      string // Holding agent
    TenantID     string
    ProjectID    string
    FilePath     string
    LeaseID      string // Lease manager reference
    FencingToken uint64 // Raft term at acquisition (0 if standalone)
    AcquiredAt   int64  // Unix nanos
    Isolation    string // "shared" or "worktree"
}
```

### Lock Acquisition Flow

```
Agent                     FileLockService                 KV Store / LeaseManager
  |                           |                                |
  |-- Acquire(path) --------->|                                |
  |                           |-- loadLock(key) -------------->|
  |                           |<-- (existing lock or nil) -----|
  |                           |                                |
  |                           |   [if exists & lease valid]:   |
  |                           |     reject if different agent  |
  |                           |     (every isolation mode)     |
  |                           |                                |
  |                           |-- GetShardRaftState() -------->|  (fencing token)
  |                           |-- CreateLease(key, TTL) ------>|
  |                           |-- storeLock(lock) ------------>|
  |                           |                                |
  |<-- FileLock --------------|                                |
```

1. Build deterministic lock key from `sha256(filePath)`
2. Check KV store for existing lock
3. If lock exists with valid lease: reject with `FileLockedError` if held by a
   different agent, in **every** isolation mode. Same-agent re-acquire is
   idempotent.
4. If lock exists with expired lease: overwrite (crash recovery).
5. Obtain fencing token from Raft term (or 0 in standalone mode).
6. Create lease with configurable TTL.
7. Persist lock to KV store.

### Configurable TTL

| Setting | Default | Description |
|---------|---------|-------------|
| `lease_ttl` | `5m` | How long a lock remains valid without renewal |
| `heartbeat_interval` | `30s` | How often an agent should renew its lock |
| `max_renewals` | `60` | Maximum number of lease renewals |

Total maximum hold time: `lease_ttl + (heartbeat_interval * max_renewals)` = 5m + 30m = 35 minutes with defaults.

### Shared vs Worktree Isolation

| Behavior | Shared Mode | Worktree Mode |
|----------|-------------|---------------|
| Lock enforcement | Mandatory (reject) | Mandatory (reject) |
| Re-acquire by different agent | `FileLockedError` | `FileLockedError` |
| Same-agent re-acquire | Idempotent | Idempotent |
| Use case | All agents edit same files | Each agent has its own checkout |

The isolation mode is recorded on the lock as provenance and is not consulted
to decide whether to grant it. Worktree mode once made the lock advisory; why
that was withdrawn is set out under [Workspace
Isolation](architecture.md#workspace-isolation).

### Raft vs Standalone Compatibility

**With Raft**: The fencing token is set to the current Raft term at lock acquisition. On renewal, the token is validated against the current term. If the term has advanced (leader changed), the lock is stale and `StaleLockError` is returned.

**Without Raft** (`raftNode == nil`): Fencing tokens are set to 0 and validation is skipped. Lease-based expiry is the sole safety mechanism.

Fencing token validation logic:
- Token == 0: skip validation (standalone mode)
- raftNode == nil: skip validation
- Token < current term: return `StaleLockError`
- Token >= current term: valid

### Crash Recovery

When an agent crashes, its locks are recovered via lease TTL expiry:

1. Agent crashes -- no explicit release
2. Heartbeat stops -- lease is not renewed
3. Lease TTL expires
4. Next `Acquire()` or `IsLocked()` call detects expired lease
5. Stale lock is overwritten or cleaned up

`ListLocks()` also cleans up stale locks on read.

### FileLockService Interface

```go
type FileLockService interface {
    Acquire(ctx, tenantID, projectID, filePath, agentID) (*FileLock, error)
    Release(ctx, tenantID, lockKey) error
    Renew(ctx, tenantID, lockKey) error
    IsLocked(ctx, tenantID, projectID, filePath) (*FileLock, bool, error)
    WaitForRelease(ctx, tenantID, lockKey) error
    ListLocks(ctx, tenantID, projectID) ([]*FileLock, error)
}
```

### MCP Tools

| Tool | Description |
|------|-------------|
| `quicbaas_orch_file_lock` | Acquire a lock on a file path |
| `quicbaas_orch_file_unlock` | Release a previously acquired lock |
| `quicbaas_orch_file_lock_status` | Check lock status and list active locks |

### Configuration

```yaml
settings:
  file_locking:
    lease_ttl: 5m
    heartbeat_interval: 30s
    max_renewals: 60
```

### Error Types

| Error | Sentinel | Condition |
|-------|----------|-----------|
| `FileLockedError` | `ErrFileLocked` | File locked by another agent (shared mode) |
| `LockNotFoundError` | `ErrLockNotFound` | Lock key does not exist |
| `StaleLockError` | `ErrStaleLock` | Fencing token behind current Raft term |
| `LockReleaseNotOwnerError` | `ErrLockReleaseNotOwner` | Release by non-holder |
| `FileLockValidationError` | `ErrFileLockValidation` | Missing required field |


---

## Cost Controls

Token budget tracking for orchestration jobs, with per-job and per-agent limits.

Source: `pkg/orchestration/cost_tracker.go`, `pkg/orchestration/cost_tracker_store.go`

### Overview

The CostTracker monitors token consumption across all orchestrated AI agents in a job. It provides budget status checks, per-agent breakdowns, and configurable actions when budgets are approached or exceeded.

### CostTracker Interface

```go
type CostTracker interface {
    RecordTokenUsage(ctx, tenantID, jobID, agentID string, tokens int64) error
    GetJobUsage(ctx, tenantID, jobID string) (*UsageReport, error)
    GetAgentUsage(ctx, tenantID, jobID, agentID string) (*UsageReport, error)
    CheckBudget(ctx, tenantID, jobID string) (BudgetStatus, error)
}
```

### Budget Status

| Status | Constant | Condition |
|--------|----------|-----------|
| `ok` | `BudgetOK` | Usage < 80% of budget |
| `warning` | `BudgetWarning` | Usage >= 80% of budget |
| `exceeded` | `BudgetExceeded` | Usage >= 100% of budget |

The warning threshold is fixed at 80% (`budgetWarningThreshold = 0.80`).

### Budget Actions

Configured via `budget_action` in workflow settings:

| Action | Behavior |
|--------|----------|
| `warn` | Log a warning. Job continues. |
| `pause` | Pause all agents. Requires manual resume. |
| `shutdown` | Gracefully shut down all agents in the job. |

### Usage Report

```go
type UsageReport struct {
    TotalTokens int64            // Total tokens consumed
    BudgetLimit int64            // Configured budget limit
    PercentUsed float64          // (TotalTokens / BudgetLimit) * 100
    ByAgent     map[string]int64 // Per-agent token breakdown
    LastUpdated int64            // Unix nanos of last update
}
```

### KV Storage Layout

Token counters are stored as big-endian int64 values:

| Key Pattern | Description |
|-------------|-------------|
| `cost:{tenant}:job:{job}:total` | Aggregate job token count |
| `cost:{tenant}:job:{job}:agent:{agent}` | Per-agent token count |
| `cost:{tenant}:job:{job}:budget` | Budget limit for the job |

Budget limits are cached in memory (`sync.Map`) to reduce KV read pressure.

### Configuration

```yaml
settings:
  cost_controls:
    max_tokens_per_job: 1000000     # Total token budget for the entire job
    max_tokens_per_agent: 200000    # Per-agent token limit
    budget_action: warn             # warn | pause | shutdown
```

### Example: Checking Budget in a Control Loop

```go
status, err := costTracker.CheckBudget(ctx, tenantID, jobID)
if err != nil {
    // BudgetNotSetError if no budget configured
    return err
}

switch status {
case orchestration.BudgetWarning:
    logger.Warn("job approaching budget limit")
case orchestration.BudgetExceeded:
    logger.Error("job exceeded budget")
    // Apply configured budget_action
}
```

### Error Types

| Error | Sentinel | Condition |
|-------|----------|-----------|
| `BudgetExceededError` | `ErrBudgetExceeded` | Job total >= budget limit |
| `BudgetNotSetError` | `ErrBudgetNotSet` | No budget configured for job |
| `UsageRecordError` | `ErrUsageRecord` | Failed to write token count |
| `UsageRetrievalError` | `ErrUsageRetrieval` | Failed to read token count |
| `CostInvalidInputError` | `ErrCostInvalidInput` | Missing/invalid input field |


---

## RAG Indexing Pipeline

The RAG (Retrieval-Augmented Generation) indexing pipeline provides cross-project code intelligence for the orchestrator. It scans source files, chunks them by language, generates embeddings, and upserts vectors into namespaced storage for semantic search.

### Pipeline Overview

```
Source files --> Language detection --> Chunking --> Embedding generation --> Vector upsert
```

Each step:

1. **Scan** - Read files from disk (single file or project glob)
2. **Detect language** - Map file extension to language identifier
3. **Chunk** - Split content at double-newline boundaries with ~500 character target size
4. **Embed** - Generate embeddings via the configured embedding service
5. **Upsert** - Store vectors in the specified namespace with file metadata

### MCP Tools

#### quicbaas_rag_index_file

Index a single file into a vector namespace.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `file_path` | string | yes | Absolute path to the file |
| `namespace` | string | yes | Vector namespace for storage |
| `model` | string | no | Embedding model override |

Returns the file path, chunk count, namespace, detected language, and timestamp.

#### quicbaas_rag_index_project

Index all matching files in a project directory.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_path` | string | yes | Absolute path to the project |
| `namespace` | string | yes | Vector namespace for storage |
| `file_pattern` | string | no | Glob pattern (default: `*.go`) |
| `model` | string | no | Embedding model override |

Walks the project directory matching files against the glob pattern. Files that fail to read or embed are skipped without aborting the batch. Returns aggregate counts of files and chunks indexed.

#### quicbaas_rag_index_status

Query indexing statistics for a namespace.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `namespace` | string | yes | Namespace to query |

Returns the vector count, dimension, and timestamp.

### Language Detection

File extensions are mapped to language identifiers:

| Extensions | Language |
|------------|----------|
| `.go` | go |
| `.py` | python |
| `.js` | javascript |
| `.ts` | typescript |
| `.rs` | rust |
| `.java` | java |
| `.c`, `.h` | c |
| `.cpp`, `.cc`, `.cxx`, `.hpp` | cpp |
| `.rb` | ruby |
| `.sh`, `.bash` | shell |
| `.md` | markdown |
| `.yaml`, `.yml` | yaml |
| `.json` | json |
| `.toml` | toml |
| `.sql` | sql |
| Other | text |

### Chunking Strategy

The chunker splits file content using these rules:

1. Split on double newlines (`\n\n`) as a universal section boundary
2. Accumulate sections into a chunk until adding the next section would exceed ~500 characters
3. Flush the current chunk and start a new one when the limit is reached
4. If a single section exceeds the target size, it becomes its own chunk
5. Empty sections are skipped

This approach preserves logical groupings (functions, type declarations, paragraphs) while keeping chunk sizes within embedding model limits.

### Vector Storage

Each chunk is stored with metadata:

```json
{
  "file_path": "/path/to/file.go",
  "language": "go",
  "chunk_index": 0,
  "chunk_text": "first 500 chars of the chunk..."
}
```

Chunk IDs are deterministic, generated via FNV-1a hash of `{file_path}:{chunk_index}`. This means re-indexing the same file overwrites existing vectors rather than creating duplicates, enabling incremental re-indexing after an agent completes work on a project.

### Vector Namespaces

Namespaces isolate vectors for different scopes:

- **Per-project**: Use a project-specific namespace (e.g., `project-go-quicbaas`) for project-scoped queries
- **Cross-project**: Use a shared namespace (e.g., `org-automatethethings`) for cross-project code search

The namespace is caller-specified, giving the orchestrator flexibility to organize vectors by project, team, or organization.

### Error Handling

The pipeline uses typed errors for each failure mode:

| Error Type | Condition |
|------------|-----------|
| `RAGIndexFileReadError` | File cannot be read from disk |
| `RAGIndexEmbeddingError` | Embedding generation fails for a chunk |
| `RAGIndexUpsertError` | Vector upsert fails for a chunk |
| `RAGIndexGlobError` | File glob pattern fails |

Single-file indexing (`quicbaas_rag_index_file`) fails on the first error. Project indexing (`quicbaas_rag_index_project`) skips failing files and continues the batch.

### Build Tags

RAG indexing tools require both the `mcp` and `rag` build tags.


---

## Peer Messaging

KV-backed agent-to-agent message routing with backpressure, idempotent delivery, and real-time subscriber notifications.

Source: `pkg/orchestration/peer_messaging.go`, `pkg/orchestration/peer_messaging_store.go`

### PeerMessage Format

```go
type PeerMessage struct {
    ID        string            `json:"id"`         // Unique message ID (dedup key)
    From      string            `json:"from"`       // Sender agent ID
    To        string            `json:"to"`         // Recipient agent ID
    Action    string            `json:"action"`     // Message type (advisory, progress, assign, etc.)
    Channel   string            `json:"channel"`    // Optional routing channel
    Content   string            `json:"content"`    // Message payload
    Metadata  map[string]string `json:"metadata"`   // Arbitrary key-value metadata
    Timestamp int64             `json:"timestamp"`  // Unix nanos
    TTL       int64             `json:"ttl"`        // Optional time-to-live (nanos)
}
```

Required fields: `ID`, `From`, `To`, `Action`. Validation returns `PeerMessageValidationError` for missing fields.

### Message Lifecycle

```
Sender                    KV Store                    Recipient
  |                          |                           |
  |-- Send(msg) ------------>|                           |
  |   [validate]             |                           |
  |   [size check]           |                           |
  |   [dedup check]          |                           |
  |   [inbox capacity]       |                           |
  |   [write inbox key]----->|                           |
  |   [write dedup key]----->|                           |
  |                          |-- notify subscriber ----->|
  |                          |                           |
  |                          |<---- Poll(since) ---------|
  |                          |----> [messages] --------->|
  |                          |                           |
  |                          |<---- Acknowledge(id) -----|
  |                          |   [delete inbox key]      |
  |                          |   [retain dedup key]      |
```

#### Send

1. Validate required fields
2. Marshal and check against `MaxMessageSize`
3. Check dedup key -- return `ErrDuplicateMessage` if already delivered
4. Check inbox capacity -- apply overflow action if full
5. Write message to inbox under key `inbox:{tenant}:{agent}:{timestamp}:{msgID}`
6. Write dedup key under `dedup:{tenant}:{agent}:{msgID}`
7. Non-blocking notify to subscriber channel

#### Poll

Returns all messages with `timestamp >= since`, sorted chronologically (oldest first). Ties broken by message ID.

#### Subscribe

Returns a buffered channel (capacity 100) that receives new messages on delivery. One subscription per agent; subsequent calls replace the previous channel.

#### Acknowledge

Removes the message from the inbox. The dedup key is retained permanently to prevent redelivery. Idempotent: returns `nil` if the message does not exist.

### Backpressure

Configured via `InboxPolicy`:

| Field | Type | Description |
|-------|------|-------------|
| `MaxMessages` | `int` | Maximum messages in an inbox |
| `MaxMessageSize` | `int` | Maximum serialized bytes per message |
| `OverflowAction` | `string` | `"reject"` or `"drop_oldest"` |

**reject** (default): Returns `ErrInboxFull` when the inbox is at capacity. The sender must retry or discard.

**drop_oldest**: Evicts the oldest message (lowest timestamp) to make room. The evicted message is lost.

### Idempotent Delivery

Every message is keyed by its `ID` field. Before writing to the inbox, the service checks whether `dedup:{tenant}:{agent}:{msgID}` exists. If so, `ErrDuplicateMessage` is returned.

Dedup keys are never deleted, even after acknowledgment. This guarantees exactly-once delivery semantics within the lifetime of the KV store.

### Auto-Wake Notification

When a message is delivered, the service sends a non-blocking notification to the subscriber channel:

```go
select {
case ch <- msg:
default:
    // Buffer full, drop notification (message still in inbox for polling)
}
```

The recipient can use `Subscribe()` to avoid polling. If the buffer is full, the notification is dropped but the message remains in the inbox for the next `Poll()`.

### Configuration

```yaml
settings:
  peer_messaging:
    max_message_size: 10240   # bytes (default: 10KB)
    max_inbox_size: 1000      # messages (default: 1000)
    overflow_action: reject   # reject | drop_oldest
    inbox_ttl: 1h             # message retention
```

### MCP Tools

| Tool | Description |
|------|-------------|
| `quicbaas_orch_peer_send` | Send a message to another orchestrated agent's inbox |
| `quicbaas_orch_peer_inbox` | Poll the calling agent's inbox for new messages |

### Error Types

| Error | Sentinel | Condition |
|-------|----------|-----------|
| `InboxFullError` | `ErrInboxFull` | Inbox at capacity, overflow=reject |
| `MessageTooLargeError` | `ErrMessageTooLarge` | Serialized size > MaxMessageSize |
| `DuplicateMessageError` | `ErrDuplicateMessage` | Message ID already delivered |
| `PeerMessageValidationError` | `ErrPeerMessageValidation` | Missing required field |
| `PeerInboxError` | `ErrPeerInbox` | KV store operation failure |
| `PeerSubscriptionError` | `ErrPeerSubscription` | Subscription setup failure |


---

## MCP Tool Filtering by Agent Scopes

Agents only see MCP tools they are authorized to use. Unauthorized tools are invisible, not merely denied.

### How MCP Tool Filtering Works

1. Agent presents its OIDC token when connecting to the MCP server.
2. On `tools/list` request, the server extracts scopes from the token claims.
3. Each registered MCP tool maps to a `resource:action` pair.
4. The server iterates all tools, calling `CheckToolAuthorization()` for each.
5. Only tools where the agent's scopes satisfy the authorization check are included in the response.
6. Public tools (e.g., `ping`, `version`) are always visible regardless of scopes.

```
Agent Token (scopes: [kms:read, orchestrator:read])
     |
     v
MCP Server: tools/list
     |
     v
FilterToolsByScopes()
     |-- kms_get_key      -> requires kms:read     -> VISIBLE
     |-- kms_create_key   -> requires kms:write    -> HIDDEN
     |-- orchestrator_health -> requires orchestrator:read -> VISIBLE
     |-- secrets_get      -> requires secrets:read -> HIDDEN
     |-- ping             -> public                -> VISIBLE
     v
Response: [kms_get_key, orchestrator_health, ping]
```

### Tool to Resource:Action Mapping

Each MCP tool registers its required scope when added to the tool registry. Examples:

| MCP Tool | Required Scope | Description |
|----------|---------------|-------------|
| `kms_get_key` | `kms:read` | Read a KMS key |
| `kms_create_key` | `kms:write` | Create a KMS key |
| `kms_rotate_key` | `kms:write` | Rotate a KMS key |
| `secrets_get` | `secrets:read` | Read a secret |
| `secrets_put` | `secrets:write` | Write a secret |
| `orchestrator_health` | `orchestrator:read` | Check orchestrator health |
| `orchestrator_spawn` | `agent:spawn` | Spawn an agent session |
| `orchestrator_prompt` | `agent:prompt` | Prompt an agent |
| `orchestrator_close` | `agent:close` | Close an agent session |

### Security Benefits

**Smaller context window** -- Agents only see tools relevant to their role, reducing prompt size and improving response quality. A reviewer agent with `[orchestrator:read]` sees only read-only tools, not the full tool catalog.

**No prompt injection for unauthorized tools** -- Since unauthorized tools are invisible (not just denied), a malicious prompt cannot reference or attempt to use tools outside the agent's scope. The attack surface is reduced to only the tools the agent can see.

**Principle of least privilege** -- Each agent role receives exactly the permissions it needs. A coder agent does not see admin tools. A reviewer does not see write tools.

**Auditability** -- Every tool invocation is traced back to the agent's ServiceAccount with its specific scopes. The audit trail shows exactly which agent called which tool with what credentials.

### Example: Agent with `["kms:read"]` Scope

An agent provisioned with only `kms:read` scope:

**Sees:**
- `kms_get_key` (requires `kms:read`)
- `kms_list_keys` (requires `kms:read`)
- `ping` (public)
- `version` (public)

**Does not see:**
- `kms_create_key` (requires `kms:write`)
- `kms_rotate_key` (requires `kms:write`)
- `secrets_get` (requires `secrets:read`)
- `orchestrator_spawn` (requires `agent:spawn`)
- All other tools outside `kms:read` scope

The agent's MCP tools/list response contains only the visible tools. From the agent's perspective, the hidden tools do not exist.


---

## MCP Preconfiguration for Spawned Agents

A spawned agent (ACP backend — claude / codex / opencode — or a remote
SSH-deployed daemon) can be handed the QuicBaaS MCP endpoint at spawn time so it
has native access to platform tools (KV, inference, …) over the Model Context
Protocol. This document describes how that configuration is delivered and how
the agent authenticates.

### Authentication: a per-session mTLS certificate, enrolled as a principal

A preconfigured MCP agent authenticates with an mTLS client certificate rather
than a bearer token. This deliberately avoids the token-renewal problem: a
generic ACP agent has no way to refresh a 15-minute platform token, so a
token-based identity would strand a long-lived session the moment its refresh
token lapsed.

The certificate is **per session**, not per host. `AgentMCPPresetResolver.Resolve`
(`pkg/server/mcp_preset_resolver.go`) mints a fresh keypair for every spawn that
carries the `quicbaas` MCP preset:

- Subject `CN=agent-<sessionID>, O=<tenantID>, OU=agents`, one-day validity,
  signed by the platform CA the MCP listener trusts for client auth.
- A tenant-scoped SPIFFE URI SAN,
  `spiffe://<trust-domain>/tenant/<tenantID>/svc/<CN>`, synthesized by the CA
  from the operator-configured `iam.spiffe_trust_domain`.
- Written to a private `0700` per-session directory (key at `0600`) and delivered
  by environment variable; the private key never leaves an mlocked buffer except
  to reach that file.

#### The certificate alone confers nothing — it must be enrolled

go-qrdb owns identity. It authenticates a client certificate **as the
certificate's Subject CommonName**, then resolves that principal's roles by
looking the subject up in the user store (go-qrdb's own
`pkg/auth/middleware` `finalizeIdentity` — not quicbaas's `pkg/auth`, which
holds no user store). A certificate no user record names proceeds *with empty
roles*, and the MCP surface is default-deny (`AuthorizeMCP`). There is no code
path by which a certificate carries authority on its own — roles are always
resolved from the store, never trusted from the credential.

So `Resolve` also **enrolls** the certificate, through
`iam.IdentityStore.EnrollAgentSessionIdentity`:

| Property | Value |
|----------|-------|
| Username (qrdb subject) | the certificate CommonName, verbatim |
| Tenant | read back off the minted certificate's SPIFFE SAN, asserted equal to the spawning tenant |
| `AuthMethods` | `["mtls"]` — nothing else can present this account |
| `CertBindings` | one binding, `qrdbsdk.ComputeSPKIThumbprint(cert)`, kind `SPKIThumbprint`, expiring with the certificate |
| Roles | exactly one: the tenant-scoped `agent-session` role |
| Email / groups | none — a session is a machine identity with no mailbox and no membership |

This is the same seam registration uses for agent **daemons**
(`EnrollFleetNodeCertIdentity`, the `agent-runtime` role); one implementation is
parameterized over the role, so a session and a daemon share every validation,
convergence rule and race resolution. They do **not** share a role or a grant.

The **tenant binding is load-bearing, not decoration.** Without the SPIFFE SAN,
qrdb falls back to the first Subject OU — `agents` on every agent certificate —
so the session would authenticate into a phantom tenant named "agents" that no
enrollment can reach. `iam.spiffe_trust_domain` is therefore required for the MCP
preset to resolve at all; there is no synthesized default, for the same reason
first boot refuses to issue the admin certificate without one (a default would
bake a fake *pinned* security identifier into every certificate). A deployment
without one gets no MCP preset and a log line saying so.

#### Revocation: the session close

The cleanup the ACP backend fires on session `Close` calls
`RevokeAgentSessionIdentity`, which **deletes** the principal. Deletion — not
disablement — is what revokes: qrdb resolves roles from the record and never
consults the certificate binding on this path, so with the record gone the same
certificate authenticates and holds nothing. The one-day certificate expiry is a
bounded backstop beneath that, not the primary control.

The revocation is idempotent (Close can fire twice, and a crashed spawn's cleanup
can run late) and it refuses to delete anything that is not one of this build's
session principals: the record must hold exactly the tenant's `agent-session`
role and be restricted to `mtls`. A CommonName is not a capability, and without
that guard a session whose CN collided with a human account or a fleet node daemon
would delete that account on close.

#### What an agent session may do

A session principal holds exactly `iam.AgentSessionGrant()`, pinned against the
`orchestration.AgentMCPScopes()` declaration:

| Scope | Tool | Purpose |
|-------|------|---------|
| `orchestrator:inference` | `quicbaas_orchestrator_inference` | sessionless model inference |
| `kv:get` | `qrdb_get` | read its tenant's key-value workspace |
| `kv:put` | `qrdb_put` | write its tenant's key-value workspace |
| `kv:list` | `qrdb_list` | enumerate keys |

The set is the floor **and** the ceiling: the role is rewritten to exactly this
on every enrollment, so a grant cannot accumulate. The absences are deliberate:

- **No `kv:delete`.** A session writes its own workspace; it must not be able to
  erase another session's or the platform's records.
- **No secrets, KMS, IAM, barrier, seal, ceremony, cluster or tenant verb.** An
  agent must not be able to administer the platform that runs it, read its
  tenant's key material, or mint another identity.
- **No orchestrator control-plane verb beyond inference.** A session cannot spawn
  siblings, dispatch work, or claim another agent's task.
- **None of the daemon's grant.** The `agent-session` and `agent-runtime` grants
  are disjoint, so neither enrollment is a route to the other's authority.

Measured against the real MCP tool table, an enrolled session is admitted on
those four tools and refused on the other 331 permissioned tools
(`pkg/apimodule/module_agent_session_mcp_authz_test.go`, which drives go-qrdb's
own mTLS authenticator, RBAC authorizer and MCP gate over the record enrollment
actually writes). Tenant isolation is enforced downstream from the authenticated
tenant; a session only ever sees its own tenant's data.

Widening this set widens every spawned agent in every tenant at once, which is
why it lives in one function with one pinning test rather than at a call site.

### Delivery: the spawn environment

The selected MCP servers are delivered to the agent through its **process
environment**, not the ACP `session/new` `mcpServers` parameter (the
claude / codex / opencode agents do not share a single `mcpServers` JSON shape,
but they all inherit the spawned process environment).

At spawn, `ACPBackend.Spawn` calls `injectMCPServersEnv`
(`pkg/orchestration/acp_mcp_env.go`), which serializes the request's MCP servers
into a single self-describing environment variable:

```
QUICBAAS_MCP_SERVERS=[{"name":"quicbaas","url":"https://…/mcp","env":{…}}]
```

The value is a JSON array of `MCPServerConfig` objects. Each server's `env` map
carries connection material — for the QuicBaaS endpoint preset, the agent's mTLS
cert and key file paths. The variable is set once at the `Spawn` entry so it
reaches the initial subprocess **and** is persisted on the session for the
prompt-time relaunch. Sessions that do not opt in carry no extra environment,
and an operator-supplied `QUICBAAS_MCP_SERVERS` value is never silently
overwritten.

### Configuring it from the UI / SDK

The `SpawnSessionRequest` carries an optional `mcp_servers` field
(`MCPServerConfig[]`) across both SDKs (TypeScript and Go) and the REST surface:

```ts
await client.orchestrator.spawnSession({
  tenantID: 'system',
  backend: 'claude',
  mcp_servers: [{ name: 'quicbaas', url: '' }],
});
```

In the web UI, the **Spawn Session** dialog exposes an opt-in *"Attach QuicBaaS
MCP endpoint"* checkbox. When selected it sends `mcp_servers: [{ name:
'quicbaas', url: '' }]`. The empty `url` is a signal that the daemon resolves
its own MCP endpoint — the browser never needs to know the operator's MCP host
or port.

### `MCPServerConfig` shape

```
{
  "name":    "quicbaas",          // server identifier
  "url":     "https://…/mcp",     // HTTP MCP endpoint (empty = QuicBaaS preset)
  "command": "",                  // stdio server executable (alternative to url)
  "args":    [],                  // stdio server arguments
  "env":     { "KEY": "value" }   // connection material (e.g. cert/key paths)
}
```

`env` round-trips losslessly through the proto bridge (`map<string,string> env`)
and the SDK, so credential material configured at spawn reaches the agent intact.

### Scope of the current implementation

Wired and tested end-to-end:

- Certificate-identity MCP authorization with the least-privilege scope set.
- `mcp_servers` across the REST + SDK surfaces and the Spawn dialog.
- Environment delivery of the configured servers to the agent process.

Follow-ups (tracked separately):

- Daemon-side resolution of the empty-`url` `quicbaas` preset to the configured
  `Server.MCP` endpoint URL plus injection of the agent's actual cert/key file
  paths. (Embedded agents currently hold their key in memory only; persisting a
  disk certificate for the embedded case is an open decision.)
- MCP preconfiguration for the SSH-deployed **daemon** (as opposed to a single
  spawned session), which configures the deployed daemon's MCP client.


---

## Orchestration Workflows

Workflow definitions control how the orchestration engine decomposes tasks, assigns agents, and reviews results.

### Workflow YAML Format

```yaml
name: <string>              # Unique workflow identifier
description: <string>       # Human-readable description
mode: <canned|constrained|autonomous>
agents:
  - role: <string>          # Agent role: lead, coder, reviewer, etc.
    backend: <string>       # Backend name: claude, codex, ollama
    model: <string>         # Model identifier
    capabilities: [<string>]  # persistent_sessions, tool_use, file_access
    mcp_scopes: [<string>]  # IAM scopes: orchestrator:read, kms:read, etc.
    count: <int>            # Optional: number of agents with this role (default: 1)
settings:
  max_iterations: <int>    # Max review rounds before escalation
  max_parallelism: <int>   # Max concurrent work items
  poll_interval: <duration> # Status poll interval (e.g., 5s)
  timeout_per_item: <duration>  # Max time per work item
  total_timeout: <duration>     # Max total job time
  hooks: <string>          # Registered lifecycle hook set (empty = none)
  review_criteria:
    - name: <string>        # Criterion name
      validator: <string>   # A registered validator kind: agent, command, policy, or a module's
      config: <string>      # Validator-specific config
      required: <bool>      # Whether failure blocks completion
      phase: <string>       # validating (default) or reviewing
```

### A delivery pipeline is NOT a Workflow

This section used to claim the opposite, and the claim was false. A Workflow
is one shape of job: **a lead agent decomposing a description into a DAG of
work items executed by workers.** `handleDecompose` runs unconditionally,
nothing skips the lead, and a `canned` or `constrained` workflow is refused at
validation without a `lead` role. Run a delivery pipeline through it and one
issue becomes nine work items in seven waves, with the worker prompted with an
LLM's paraphrase instead of what a person wrote.

A delivery pipeline is a **different shape over the same envelope**: no lead,
no decomposition, one persistent worker prompted with the issue contract
verbatim. That is a `JobRunner`, not a Workflow — see
[job-runners.md](job-runners.md). Workflows keep every field below and are
unchanged; what changed is that they are no longer the only shape a job can
have.

A Workflow still supplies, for the jobs that are Workflows:

| Need | Workflow field |
|---|---|
| Each job works in its own directory | `settings.isolation` set to the per-job mode (see `WorkspaceIsolation`) |
| The agent that makes the change | a worker `agents[]` entry with `persistent_sessions` |
| What is verified before publishing | `review_criteria` in the `validating` phase (e.g. `command` criteria) |
| What judges the published work | `review_criteria` in the `reviewing` phase (a module-registered kind) |
| How many review rounds before the engine gives up | `settings.max_iterations` |
| What happens at each lifecycle moment | `settings.hooks` naming a registered `JobLifecycleHooks` set |

The engine runs every workflow the same way. What a hook or a validator does
is the module's business; the engine never learns it.

### Lifecycle seams

Four generic seams let a module plug into the engine without the engine
importing it. All four are registries in `pkg/orchestration`, populated by
name at startup (a module registers from its own mount point, **before** the
engine starts). The fifth row below is not a registry: `WorkflowSettings.Hooks`
is the field a workflow selects its hook set with, listed here because it is
how the third seam is reached for a workflow job:

| Seam | Registration | What the engine does with it |
|---|---|---|
| `JobRunner` | `RegisterJobRunner(runner)` | Owns the state sequence of one shape of job. `JobConfig.Runner` selects it; empty resolves to the engine's own `workflow` runner. The runner declares its ordered non-terminal states, refuses a configuration it cannot drive before any goroutine or lease exists, and drives the job to a terminal state over the engine's `*JobRun` envelope. Full contract: [job-runners.md](job-runners.md). |
| `TaskSource` | `RegisterTaskSource(kind, source)` | The task-source poller (`orchestration.sweep_interval`) asks every source, per **autonomous** project (`Project.Autonomous`), for new work and creates auto-spawning tasks, adopting at most what admission left room for — the project quota, the fleet's fairly divided worker capacity, and the provider pool's usage headroom ([admission-and-readiness.md](../software-factory/admission-and-readiness.md)). The source declares the provenance kind its tasks carry and the runner its tasks run under; `TaskSourceKind` is open — the engine only knows `scheduler`, `operator`, `job`. Re-polling is idempotent through the task store's guarded source index. |
| Validator | `RegisterValidator(kind, fn)` | `review_criteria[].validator` names a kind; `handleValidate` (validating phase) and `handleReview` (reviewing phase) dispatch by kind. Unknown kinds are refused at workflow validation. The built-in `command` kind runs only commands in the operator allowlist `orchestration.verification_commands`, in the job's prepared directory, ON THE DAEMON. A module whose jobs work inside a container registers its own kind instead and runs the command through `JobContext.Container` — the same allowlist, the other side of the boundary; the SDLC module's `container_command` is the one such kind. |
| `JobLifecycleHooks` | `RegisterJobHooks(name, hooks)` | Five moments. For the `workflow` runner the envelope resolves the set from `WorkflowSettings.Hooks` and `JobRun.SetState` fires the state-keyed moment; a module runner may instead call a moment directly through `JobRun.RunStage`, which still applies the operator's stage timeout, `JobStatus.stage` and the stage event, and which can re-run a moment (the `sdlc` runner re-runs `Reviewable` on each review round). The moments: `Prepare` (entering `initializing`, before any session spawns; returns the directory every session is spawned into), `Reviewable` (entering `reviewing`), `Complete` (entering `completing`), `Blocked` (entering `escalating`), `Discard` (once the job is over, however it ended, on a context detached from the job's own and bounded by `orchestration.stage_timeouts.cleanup`). A hook error fails the job with `failure_class: hook`. `JobStatus.stage` reports the last moment; a `stage` `JobEvent` is emitted per moment. |
| Workflow | `WorkflowSettings.Hooks` | Selects the hook set per workflow; see the table above. |

Per-moment bounds are `orchestration.stage_timeouts.{prepare,reviewable,complete,cleanup,blocked}`;
a zero bound takes `orchestration.DefaultStageTimeouts`.

**A job that is an attempt at a Task.** `JobConfig.TaskID` names the Task the
job is an attempt at, and it is an accounting relationship only: it is the
lease subject and generation, it decides where `concludeAttempt` and the
reaper write the outcome, and it reaches hooks as `JobContext.TaskID` and
`Attempt`. The work itself is unchanged — a lead-driven job creates a Task per
work item whether or not it is an attempt at one. (The earlier "bound-task
mode", which mapped work items onto the Task instead, is deleted: a module
that needs a different shape of job registers its own `JobRunner`.)

**Spawn environment.** Every session the control loop spawns — worker, lead
and reviewer — passes through one preparation (`worker_spawn.go`): the
engine's own grant, the configuration directory
`orchestration.agent_config_dir` (`CLAUDE_CONFIG_DIR`), and a pooled
account (`AccountAuto`) on the backends the server pools. A session never
depends on the daemon host's ambient identity; a pooled backend with no
account to resolve fails the spawn closed rather than prompting a person to
log in.

**The grant follows the isolation** (#927). On a Claude Code backend, a request
that states `SandboxMode: Required` takes `--dangerously-skip-permissions` —
the container is the boundary, and a containerized agent that cannot run a
command cannot build, test or read a diff. Everything else takes
`--permission-mode acceptEdits`, including a mode policy has not resolved yet,
so the operator's `ExecutionGuard` allowlist remains the authority for what
runs on the daemon host. An agent spec or runner that states its own
`--permission-mode` keeps it, which is how a read-only role (triage) narrows
itself. See `docs/orchestration/sandbox.md` for the container side: the
spawn uid is never root, and privilege inside the image is passwordless `sudo`.

### Workflow Modes

#### Canned

**Declared, not implemented — do not select it.** The mode is documented as
"the lead agent does not decompose tasks; work items are predefined", and
nothing implements that: `handleDecompose` runs unconditionally and a
workflow with no `lead` role is refused at validation, so a `canned` workflow
is decomposed like any other and says nothing about it. Whether it is built
or removed is an owner decision, tracked as issue #619.

#### Constrained

The lead agent decomposes tasks into work items, but operates within the agent roles and capabilities defined in the workflow YAML. The engine enforces role boundaries, parallelism limits, and review criteria. Best for structured development tasks.

#### Autonomous

The lead agent has full control over task decomposition, agent spawning, and execution strategy. Minimal guardrails beyond iteration limits and timeouts. Best for exploratory or complex tasks where the optimal approach is unknown.

### Built-in Workflows

The workflows shipped in `workflows/` are shown below exactly as they ship.
No role names a `model:`, so each role runs its backend's configured default
model (`ACPBackendConfig.DefaultModel`). That keeps the coding tier's version in
one place: the backend configuration, not every workflow file. Name a model only when a role
really wants a different tier, and name the current one: it must appear in the
backend's advertised catalog (its `ListModels()`).
`TestShippedWorkflows_ModelsAreInTheBackendCatalog` fails on any shipped
workflow that names a model its backend no longer offers.

#### software-dev (constrained)

Multi-agent software development with lead, coder, and reviewer roles.

```yaml
name: software-dev
description: "Multi-agent software development workflow with lead, coder, and reviewer roles"
mode: constrained
agents:
  - role: lead
    backend: claude
    capabilities: [persistent_sessions, tool_use]
    mcp_scopes: [orchestrator:read, orchestrator:write]
  - role: coder
    backend: claude
    capabilities: [persistent_sessions, tool_use, file_access]
    mcp_scopes: [kms:read]
    count: 2
  - role: reviewer
    backend: claude
    capabilities: [persistent_sessions, tool_use]
    mcp_scopes: [orchestrator:read]
settings:
  max_iterations: 10
  max_parallelism: 3
  poll_interval: 5s
  timeout_per_item: 10m
  total_timeout: 30m
  review_criteria:
    - name: tests_pass
      validator: command
      config: "make test"
      required: true
```

#### code-review (constrained)

Code review with lead and reviewer agents, gated on `make lint`.

```yaml
name: code-review
description: "Code review workflow with lead and reviewer agents"
mode: constrained
agents:
  - role: lead
    backend: claude
    capabilities: [persistent_sessions, tool_use]
    mcp_scopes: [orchestrator:read, orchestrator:write]
  - role: reviewer
    backend: claude
    capabilities: [persistent_sessions, tool_use]
    mcp_scopes: [orchestrator:read]
settings:
  max_iterations: 5
  max_parallelism: 2
  poll_interval: 5s
  timeout_per_item: 10m
  total_timeout: 20m
  review_criteria:
    - name: lint_pass
      validator: command
      config: "make lint"
      required: true
```

#### autonomous

A single lead agent with full control over task decomposition and execution.

```yaml
name: autonomous
description: "Autonomous single-agent workflow with full control over task decomposition and execution"
mode: autonomous
agents:
  - role: lead
    backend: claude
    capabilities: [persistent_sessions, tool_use, file_access]
    mcp_scopes: [orchestrator:read, orchestrator:write, kms:read]
settings:
  max_iterations: 20
  max_parallelism: 1
  poll_interval: 5s
  timeout_per_item: 15m
  total_timeout: 60m
```

### CLI Usage

```bash
# Run with a built-in workflow
quicbaas orchestrator run "Build authentication service" --workflow software-dev

# Run with a custom workflow file
quicbaas orchestrator run "Review PR #42" --workflow-file ./my-review.yaml

# Run with defaults (no workflow specified)
quicbaas orchestrator run "Build a REST API" --backend ollama

# List available workflows
quicbaas orchestrator workflow list
quicbaas orchestrator workflow list --output json

# Show workflow details
quicbaas orchestrator workflow show software-dev
quicbaas orchestrator workflow show code-review --output yaml
```

### GPU Profiles

GPU profiles allow workflows to select Ollama models based on available hardware. The tictactoe example demonstrates this with a tier + model-family matrix.

#### Model Matrix

| Tier | VRAM | llama | qwen | deepseek |
|------|------|-------|------|----------|
| `cpu` | 0 GB | tinyllama | tinyllama | tinyllama |
| `gpu-lite` | ~8 GB | llama3 | qwen2.5-coder:7b | deepseek-coder-v2:16b |
| `gpu-power` | ~48 GB | llama3.3:70b | qwen2.5-coder:72b | deepseek-coder-v2:236b |
| `gpu-max` | ~90 GB | llama3.3:70b | qwen2.5-coder:72b | deepseek-coder-v2:236b |

`gpu-max` uses the same models as `gpu-power` but with higher iteration limits (10 vs 5) and extended timeouts (60m vs 45m).

#### Template-Based Generation

The tictactoe example uses a workflow template (`workflow-template.yaml`) with placeholders that are resolved at runtime based on the selected profile:

```yaml
name: tictactoe-{{PROFILE}}
description: "Tic-tac-toe game development ({{PROFILE_DESC}})"
mode: constrained
agents:
  - role: lead
    backend: ollama
    model: {{OLLAMA_MODEL}}
  - role: coder
    backend: opencode
    model: anthropic:claude-sonnet-4-5-20250514
    capabilities: [persistent_sessions, tool_use, file_access]
    mcp_scopes: [orchestrator:read]
  - role: reviewer
    backend: ollama
    model: {{OLLAMA_MODEL}}
settings:
  max_iterations: {{MAX_ITERATIONS}}
  max_parallelism: 1
  poll_interval: 5s
  total_timeout: {{TOTAL_TIMEOUT}}
```

The `run.sh` script resolves these placeholders using `--gpu-profile` and `--model-family` flags:

```bash
./run.sh --gpu-profile gpu-lite --model-family llama
./run.sh --gpu-profile gpu-power --model-family qwen
```

#### Tier Settings

| Tier | Max Iterations | Total Timeout |
|------|---------------|---------------|
| `cpu` | 5 | 30m |
| `gpu-lite` | 5 | 30m |
| `gpu-power` | 5 | 45m |
| `gpu-max` | 10 | 60m |

### Creating Custom Workflows

1. Create a YAML file following the format above.
2. Place it in the `workflows/` directory or reference it with `--workflow-file`.
3. Define agent roles matching your team structure.
4. Set `mcp_scopes` for each role to control which MCP tools agents can access.
5. Configure `settings` for iteration limits, parallelism, and timeouts.

Validation rules:
- `name` is required and must be unique.
- `mode` must be `canned`, `constrained`, or `autonomous`.
- At least one agent must be defined.
- `canned` and `constrained` modes require a `lead` role agent.
- `mcp_scopes` follow the `resource:action` format (e.g., `kms:read`, `secrets:write`).


---

## Job Records and Amendments

A job's record (`JobStatus`, one KV key per tenant and job) has more than one
writer. The job's own run publishes a whole snapshot of its state. Other
writers amend the same record in between: a child spawn appends the child and
sets the lineage (`ParentJobID`, `SpawnedBy`, `SpawnedAt`), and a session
conclusion drops the session and appends an audit line. Writing the snapshot as
it stands would undo those amendments.

`storeJobStatus` (`pkg/orchestration/orchestrator.go`) therefore never does a
plain put:

- A record the store does not hold yet is created with `PutIfAbsent`.
- A record it does hold is read, has its amendments folded into the snapshot by
  `mergeJobAmendments` (`job_status_record.go`), and is replaced by a
  compare-and-swap against the exact bytes read.
- A swap that lost to another writer re-reads and merges again, up to
  `maxJobStatusSwapAttempts` times, then fails with `JobStatusContendedError`.
- A store without both verbs is refused with `JobStoreUnguardedError`;
  `updateJobStatus` refuses one for the same reason.

The merge keeps every field the snapshot owns and takes the rest from the
stored record: lineage where the snapshot has none; every stored child, ahead
of any only the snapshot has; every stored audit event the snapshot lacks
(compared whole and counted, ordered by timestamp); and no session the stored
trail records as concluded. Neither argument is modified.

---

## Fleet

The fleet registry, enrollment, heartbeat and recovery, the node daemon, its
sandbox, bind-mount allowlist, container environment and the embedded fleet
node are specified in [../agent-fleet/engineering.md](../agent-fleet/engineering.md).

## Orchestration environment

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_ORCHESTRATION_ENABLED` | Enable orchestration service | `true` |
| `QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT` | Silence after which a fleet node counts as gone for session reconciliation | `90s` |
| `QUICBAAS_ORCHESTRATION_LEAD_AGENT_AGENT_ID` | System-tenant [Agent](../software-factory/agents.md) the lead agent runs as | (none) |
| `QUICBAAS_ORCHESTRATION_LEAD_AGENT_POOL_ID` | Or the pool it fails over along | (none) |
| `QUICBAAS_TENANT_ID` | Default tenant for fleet node registration | `"default"` |

Check orchestrator health with `quicbaas orchestrator health --output json`.

## Agent Roles and Capabilities

When agents are provisioned through a workflow, each agent is assigned a role with specific capabilities and MCP scopes. See Agent Identity for IAM integration details.

### Role Types

| Role | Purpose | Typical Backend |
|------|---------|-----------------|
| lead | Decomposes tasks, coordinates workers, reviews results | claude |
| coder | Implements code changes, runs tests | claude, codex |
| reviewer | Reviews code quality, validates output | codex, ollama |

### Capabilities

Capabilities declared in workflow YAML control what features an agent can use:

- `persistent_sessions` - Agent maintains state across prompts
- `tool_use` - Agent can invoke MCP tools
- `file_access` - Agent can read/write project files

### MCP Scope Assignment

Each role receives IAM scopes that filter which MCP tools are visible. See MCP Permissions for how tool filtering works.

---

## Naming Convention

Strict nomenclature for distinguishing infrastructure agents from AI coding agents, and for naming the QuicBaaS planes.

### Planes

QuicBaaS as a Backend-as-a-Service spans two planes; the same `quicbaasd` process serves both, but the responsibilities are distinct:

| Plane | What it does | Who calls it |
|-------|--------------|--------------|
| **Data plane** | KV / vector / secrets / KMS / messaging / object storage / time-series CRUD. The actual BaaS workload. | Application clients via the SDK |
| **Control plane** | IAM (tenants, users, roles, policies, sessions), fleet (enroll, heartbeat, dispatch), orchestrator (jobs, sessions, projects), event log, certificate authority. | `quicbaas fleet serve`, the desktop/web GUI, go-trusted-platform, operator CLI |

The CLI flag is `--server` (it covers both planes — one process, one endpoint). Architecturally, internal narrative uses **control plane** when describing the role the fleet and orchestrator subsystems play, and **server** or **QuicBaaS server** for the addressable endpoint. The server may be a single `quicbaasd` process or a Raft-replicated cluster (via go-qrdb); from the agent's perspective the address is the same.

### Definitions

| Term | Meaning | Examples |
|------|---------|---------|
| **QuicBaaS agent** (daemon) | The infrastructure daemon (`quicbaas fleet serve`) that runs on remote systems. Handles enrollment with the control plane, fleet heartbeat, and gRPC FleetRuntime service for spawn/prompt/status/close RPCs. Self-enrolls using OAuth2 Client Credentials, reuses pkg/orchestration backends (ACPBackend, OllamaBackend, ContainerRuntime), and enforces sandbox-first execution with tenant isolation. | `quicbaas fleet serve` CLI, `pkg/fleet/daemon.go`, FleetRuntime gRPC service on port 9091, heartbeat into fleet registry |
| **AI agent** / **orchestrated agent** | An AI coding tool (Claude Code, Codex, OpenCode) managed by the QuicBaaS orchestrator. Runs inside a container or on a developer machine. | Claude Code CLI, Codex CLI, OpenCode CLI |

### Why This Distinction Matters

Without clear terminology, documentation and code become ambiguous:

- "The agent crashed" -- which agent? The infrastructure binary or the AI tool?
- "Register the agent" -- fleet registration (infrastructure) or project registration (AI agent)?
- "Agent health check" -- infrastructure heartbeat or AI session status?

Using precise terms prevents miscommunication in docs, code reviews, incident response, and operational runbooks.

### Where Each Term Is Used

#### QuicBaaS Agent (Infrastructure)

- `pkg/orchestration/fleet_registry.go` -- Fleet registry for QuicBaaS agent instances
- `pkg/orchestration/agent_identity.go` -- Agent identity and attestation
- REST API: `/api/v1/fleet` -- Register/deregister QuicBaaS agents
- MCP tools: `quicbaas_fleet_*` prefix -- Operations on the infrastructure layer
- Entrypoint scripts: Fleet registration and heartbeat loops
- Docker healthchecks: Verify the infrastructure agent is responsive

#### AI Agent / Orchestrated Agent

- `pkg/orchestration/state_machine.go` -- MemberStatus and ExecutionStatus track AI agent state
- `pkg/orchestration/peer_messaging.go` -- AI agents exchange messages
- `pkg/orchestration/file_lock.go` -- AI agents coordinate file access
- `pkg/orchestration/cost_tracker.go` -- Token budgets per AI agent
- MCP tools: `quicbaas_orch_*` prefix -- Coordination between AI agents
- Workflow definitions: `AgentRoleSpec` describes AI agent roles (lead, worker, validator)
- Container Dockerfiles: Package the AI agent CLI tools

#### Overlap

The entrypoint scripts bridge both worlds: they register the container as a QuicBaaS agent (infrastructure) and then exec the AI agent CLI (orchestrated agent). The QuicBaaS agent ID maps to the container hostname; the AI agent session is created separately via `quicbaas_orchestrator_spawn`.

### Style Guide

- Always write "QuicBaaS agent" (capitalized, two words) when referring to the infrastructure binary
- Always write "AI agent" or "orchestrated agent" when referring to Claude Code, Codex, or OpenCode
- Never use bare "agent" without qualification in documentation or comments
- In code, struct/variable names may use `Agent` alone when the context is unambiguous within a single package


---

## Workflow Settings Reference

Complete configuration reference for `WorkflowSettings` in workflow definitions.

Source: `pkg/orchestration/workflow_types.go`

### WorkflowDefinition Structure

```yaml
name: my-workflow
description: Description of the workflow
mode: constrained          # canned | constrained | autonomous
agents:
  - role: lead
    backend: acp
    model: claude-sonnet-4-20250514
    capabilities: [code_review, task_decomposition]
    mcp_scopes: [quicbaas_agent_*, quicbaas_orch_*]
  - role: worker
    backend: acp
    model: claude-sonnet-4-20250514
    capabilities: [code_write, test_write]
    mcp_scopes: [quicbaas_orch_peer_*, quicbaas_orch_file_*]
    count: 3
settings:
  # ... (see below)
```

### Workflow Modes

| Mode | Constant | Description |
|------|----------|-------------|
| `canned` | `WorkflowModeCanned` | Declared, not implemented — the lead decomposes anyway (issue #619). Requires lead agent. |
| `constrained` | `WorkflowModeConstrained` | Lead decomposes tasks within defined agent roles. Requires lead agent. |
| `autonomous` | `WorkflowModeAutonomous` | Lead has full control over task decomposition and agent spawning. |

### Complete Settings Reference

#### Core Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_iterations` | `int` | 0 (unlimited) | Maximum review rounds before escalation |
| `max_parallelism` | `int` | 0 (unlimited) | Maximum concurrent agent tasks |
| `poll_interval` | `string` | - | Duration between control loop polls (e.g., `"5s"`) |
| `timeout_per_item` | `string` | - | Maximum duration per work item (e.g., `"10m"`) |
| `total_timeout` | `string` | - | Maximum total workflow duration (e.g., `"2h"`) |
| `review_criteria` | `[]ReviewCriterion` | `[]` | Checks judged in the validating and reviewing phases |
| `hooks` | `string` | `""` | Registered `JobLifecycleHooks` set every job runs; unknown names are refused at validation |

#### Communication

| Field | Type | Default | Valid Values | Description |
|-------|------|---------|-------------|-------------|
| `topology` | `string` | `hub_and_spoke` | `hub_and_spoke`, `hybrid`, `peer_to_peer` | Inter-agent messaging topology |
| `isolation` | `string` | `shared` | `shared`, `worktree` | Workspace isolation mode |

#### Peer Messaging

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `peer_messaging.max_message_size` | `int` | `10240` | Maximum serialized message size in bytes |
| `peer_messaging.max_inbox_size` | `int` | `1000` | Maximum messages per agent inbox |
| `peer_messaging.overflow_action` | `string` | `"reject"` | `"reject"` or `"drop_oldest"` |
| `peer_messaging.inbox_ttl` | `string` | `"1h"` | Message retention duration |

#### File Locking

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `file_locking.lease_ttl` | `string` | `"5m"` | Lock lease duration |
| `file_locking.heartbeat_interval` | `string` | `"30s"` | Lease renewal interval |
| `file_locking.max_renewals` | `int` | `60` | Maximum lease renewals |

#### Vector DB / Auto-Indexing

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `vector_db.enabled` | `bool` | `false` | Enable auto-indexing |
| `vector_db.index_type` | `string` | `"auto"` | `"auto"`, `"memory"`, `"hnsw"` |
| `vector_db.auto_index` | `bool` | `false` | Auto-index on file changes |
| `vector_db.triggers` | `[]string` | `[]` | Indexing triggers |
| `vector_db.gpu_accelerated` | `bool` | `false` | Request GPU acceleration |

#### Cost Controls

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `cost_controls.max_tokens_per_job` | `int64` | `0` (unlimited) | Total token budget for the job |
| `cost_controls.max_tokens_per_agent` | `int64` | `0` (unlimited) | Per-agent token limit |
| `cost_controls.budget_action` | `string` | `""` | `"warn"`, `"pause"`, or `"shutdown"` |

#### Recovery

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `recovery.auto_restart` | `bool` | `false` | Enable automatic agent restart |
| `recovery.max_restarts` | `int` | `3` | Maximum restart attempts |
| `recovery.restart_backoff` | `string` | `"30s"` | Delay between restart attempts |

#### Review Criteria

Each entry in `review_criteria`:

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Human-readable criterion name (required) |
| `validator` | `string` | A registered validator kind: `"agent"`, `"command"`, `"policy"`, or one a module registered with `RegisterValidator`; unknown kinds are refused at validation |
| `config` | `string` | Validator-specific configuration; for `command`, the exact allowlisted command |
| `required` | `bool` | Whether failure blocks completion |
| `phase` | `string` | `"validating"` (default) runs before the work is published; `"reviewing"` runs on the published work and, when any reviewing criteria exist, replaces the engine's agent review |

### Example Configurations

#### Conservative (Production)

```yaml
settings:
  max_iterations: 5
  max_parallelism: 2
  poll_interval: 10s
  timeout_per_item: 15m
  total_timeout: 1h
  topology: hub_and_spoke
  isolation: shared
  peer_messaging:
    max_message_size: 10240
    max_inbox_size: 500
    overflow_action: reject
    inbox_ttl: 30m
  file_locking:
    lease_ttl: 5m
    heartbeat_interval: 30s
    max_renewals: 30
  cost_controls:
    max_tokens_per_job: 500000
    max_tokens_per_agent: 100000
    budget_action: shutdown
  recovery:
    auto_restart: false
```

#### Recommended

```yaml
settings:
  max_iterations: 10
  max_parallelism: 4
  poll_interval: 5s
  timeout_per_item: 10m
  total_timeout: 2h
  topology: hybrid
  isolation: shared
  peer_messaging:
    max_message_size: 10240
    max_inbox_size: 1000
    overflow_action: reject
    inbox_ttl: 1h
  file_locking:
    lease_ttl: 5m
    heartbeat_interval: 30s
    max_renewals: 60
  vector_db:
    enabled: true
    index_type: auto
    auto_index: true
    triggers:
      - file_change
  cost_controls:
    max_tokens_per_job: 1000000
    max_tokens_per_agent: 200000
    budget_action: warn
  recovery:
    auto_restart: true
    max_restarts: 3
    restart_backoff: 30s
  review_criteria:
    - name: tests_pass
      validator: command
      config: "make test"
      required: true
    - name: lint_clean
      validator: command
      config: "make lint"
      required: false
```

#### Advanced (Autonomous, High Throughput)

```yaml
settings:
  max_iterations: 50
  max_parallelism: 8
  poll_interval: 2s
  timeout_per_item: 5m
  total_timeout: 4h
  topology: peer_to_peer
  isolation: worktree
  peer_messaging:
    max_message_size: 32768
    max_inbox_size: 5000
    overflow_action: drop_oldest
    inbox_ttl: 2h
  file_locking:
    lease_ttl: 2m
    heartbeat_interval: 15s
    max_renewals: 120
  vector_db:
    enabled: true
    index_type: hnsw
    auto_index: true
    triggers:
      - file_change
      - iteration_complete
  cost_controls:
    max_tokens_per_job: 5000000
    max_tokens_per_agent: 500000
    budget_action: pause
  recovery:
    auto_restart: true
    max_restarts: 10
    restart_backoff: 10s
```

### Validation

`WorkflowSettings.Validate()` checks:

1. Topology is a known value (or empty for default)
2. Isolation mode is a known value (or empty for default)
3. Overflow action is `"reject"` or `"drop_oldest"` (or empty for default)
4. Budget action is `"warn"`, `"pause"`, or `"shutdown"` (or empty for default)

`WorkflowSettings.ApplyDefaults()` fills zero-value fields before validation.

`WorkflowDefinition.Validate()` additionally checks:
- Name is non-empty
- Mode is valid
- At least one agent is defined
- Canned/constrained modes include a `lead` role

## Software Factory modules

The SDLC module, projects and pooled accounts, and prompt sets are built on
this engine and specified in
[../software-factory/engineering.md](../software-factory/engineering.md).

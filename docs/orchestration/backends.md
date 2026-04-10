# Agent Backends

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
| `closed` | Session has been terminated. |
| `error` | Session encountered an unrecoverable error. |

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

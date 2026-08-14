# Agent Client Protocol (ACP)

ACP is an open standard ([agentclientprotocol.com](https://agentclientprotocol.com)) for communicating with AI coding agents. It defines a JSON-RPC 2.0 based protocol for session management and prompt execution over stdin/stdout.

go-quicbaas implements an ACP client through the `ACPBackend` and `StdioTransport` types in `pkg/orchestration` (`acp_backend.go` and `transport.go`).

## Architecture

```
QuicBaaS Server
    |
    v
ACPBackend (session management)
    |
    v
StdioTransport (JSON-RPC over stdio)
    |
    v
Agent subprocess (Claude Code, Codex, etc.)
```

The `ACPBackend` manages sessions as a `sync.Map` of `acpSession` structs. Each session owns a `StdioTransport` connected to a single agent subprocess.

## StdioTransport

`StdioTransport` manages bidirectional JSON-RPC communication with an agent subprocess.

**Wire format:** Newline-delimited JSON. Each message is a single JSON object terminated by `\n`.

**Concurrency model:**
- Writes to stdin are mutex-protected for safe concurrent use.
- An atomic counter (`nextID`) assigns unique request IDs.
- A `sync.Map` of pending channels dispatches responses to the correct caller.
- A background `readLoop` goroutine reads stdout and routes messages by ID.

**Key methods:**

| Method | Description |
|--------|-------------|
| `Send(ctx, method, params)` | Sends a request and blocks until the matching response arrives or context is cancelled. |
| `Notify(method, params)` | Sends a fire-and-forget notification (no ID, no response). |
| `Close()` | Closes stdin, sends SIGTERM, waits 5 seconds, then SIGKILL if needed. |
| `PID()` | Returns the subprocess PID. |

## Session Lifecycle

### 1. Initialize

The first message sent after subprocess start. Establishes the ACP connection.

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "client_info": {
      "name": "go-quicbaas",
      "version": "1.0.0"
    }
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "server_info": {
      "name": "claude-code",
      "version": "1.0.0"
    },
    "capabilities": {},
    "instructions": ""
  }
}
```

### 2. Session New

Creates a named session with optional system prompt, model, MCP servers, and working directory.

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "session/new",
  "params": {
    "system_prompt": "You are a Go backend engineer.",
    "model": "claude-sonnet-4-20250514",
    "work_dir": "/home/user/project",
    "mcp_servers": [
      {
        "name": "quicbaas",
        "command": "quicbaas",
        "args": ["mcp", "serve"]
      }
    ]
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "session_id": "sess_abc123"
  }
}
```

### 3. Session Prompt

Sends a prompt to an existing session. The agent processes the prompt and returns content blocks.

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "session/prompt",
  "params": {
    "session_id": "sess_abc123",
    "prompt": "Add unit tests for the UserService."
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "session_id": "sess_abc123",
    "content": [
      {"type": "text", "text": "I've added tests in user_service_test.go..."}
    ],
    "model": "claude-sonnet-4-20250514",
    "stop_reason": "end_turn"
  }
}
```

### 4. Session Close

Terminates a session. Sent as a notification (no response expected).

```json
{
  "jsonrpc": "2.0",
  "method": "session/close",
  "params": {
    "session_id": "sess_abc123"
  }
}
```

After the notification, the transport is closed (stdin EOF, SIGTERM, then SIGKILL after 5s timeout).

## Turn Contract

`PromptSession` is **synchronous**: the call returns only after the agent has
finished its turn. There is no deferred delivery — a successful response always
carries a completed turn, and a failure is an error, never an empty response.

### Service wire shape

The orchestrator service answers with a structured turn on every transport:

```json
{
  "session_id": "ce4b6a40-...",
  "content": [{"type": "text", "text": "SELF-HOSTING ONLINE"}],
  "model": "claude-fable-5",
  "stop_reason": "end_turn",
  "timestamp": 1786207030175886792
}
```

### SDK view

The SDK exposes the flattened `orchestrator.PromptSessionResponse`
(`response`, `done`, `model`, `stop_reason`). The two shapes share only
`session_id`, so **every transport must convert** through
`orchestrator.NewPromptSessionResponse` / the `*Wire` types in
`sdk/go/client/orchestrator/session_wire.go` — decoding the service payload
straight into the SDK type silently produces an empty `done: false`.

- `done` reports that the agent's turn completed. Because the call is
  synchronous it is true on every successful response.
- A response with neither content nor a stop reason is not a turn and yields
  `*orchestrator.EmptyTurnError`.
- The same rule applies to the sibling session responses: the service names the
  lifecycle field `state` (the SDK exposes it as `status`) and sends no `count`
  for `ListSessions`, both of which the `*Wire` converters map.

### Stop reasons across dialects

| Backend dialect | Turn terminator | Stop reason on the wire |
|-----------------|-----------------|-------------------------|
| ACP JSON-RPC (opencode, codex) | `session/prompt` result | Reported by the agent |
| stream-json CLI (claude) | `{"type":"result","subtype":"success"}` | `stop_reason` is `null` on the assistant message |
| One-shot CLI (`--print`) | Process exit | Not reported |

The stream-json and one-shot paths normalise a completed turn to
`orchestration.StopReasonEndTurn` so every backend describes completion
identically. An agent-reported stop reason (for example `max_tokens`) is always
preserved; the CLI prints it whenever it is not an ordinary end of turn.

## Protocol Types

### Core Message Types

| Type | Description |
|------|-------------|
| `JSONRPCMessage` | Base JSON-RPC 2.0 envelope with `jsonrpc`, `id`, `method`, `params`, `result`, `error`. |
| `JSONRPCError` | Error object with `code`, `message`, `data`. |
| `InitializeParams` | Client info sent during connection init. |
| `InitializeResult` | Server info and capabilities returned from init. |
| `SessionNewParams` | System prompt, model, MCP servers, work dir. |
| `SessionNewResult` | Contains the assigned `session_id`. |
| `SessionPromptParams` | Session ID and prompt text. |
| `SessionPromptResult` | Content blocks, model, stop reason. |
| `SessionCloseParams` | Session ID to close. |

### Event Types

| Type | Description |
|------|-------------|
| `SessionUpdate` | Event from agent during prompt execution. Contains content or a tool call. |
| `ToolCall` | Agent requesting tool execution: `id`, `name`, `arguments`. |
| `ToolResult` | Response sent back to agent: `tool_call_id`, `content`, `is_error`. |
| `ContentBlock` | A unit of response content with `type` (e.g., `"text"`) and `text`. |

## Callback Handling

During prompt execution, agents may issue callbacks requesting file system operations or permissions. The `CallbackHandler` processes these.

### Supported Callbacks

| Callback | Arguments | Description |
|----------|-----------|-------------|
| `fs/read_text_file` | `path` (string) | Read a file and return its contents. |
| `fs/write_text_file` | `path` (string), `content` (string) | Write content to a file. |
| `session/request_permission` | `path` (string, optional) | Request permission for an operation. Auto-approved if path is within allowed directories. |

### IAM Enforcement

The `CallbackHandler` enforces path-based access control:

1. Allowed paths are configured at handler creation via `NewCallbackHandler(allowedPaths)`.
2. All paths are resolved through `filepath.EvalSymlinks` to prevent symlink traversal.
3. `filepath.Rel` is used to verify the target path does not escape allowed directories (no `..` prefix).
4. For files that do not yet exist (write operations), the parent directory is resolved instead.

Unauthorized access returns an error result: `"access denied: path outside allowed directories"`.

```go
handler := acp.NewCallbackHandler([]string{
    "/home/user/projects/myapp",
    "/tmp/agent-workspace",
})

result, err := handler.HandleCallback(ctx, toolCall)
```

## Transport Details

- **Protocol:** JSON-RPC 2.0 over newline-delimited JSON on stdin/stdout.
- **Stderr:** Discarded (piped to `io.Discard`) to prevent blocking.
- **ID generation:** Atomic int64 counter, monotonically increasing.
- **ID matching:** JSON numbers unmarshal as `float64`; the transport handles both `float64` and `int64` ID types.
- **Pending requests:** Stored in a `sync.Map` keyed by request ID, with buffered channels (capacity 1) for responses.
- **Shutdown sequence:** Close stdin, close done channel, drain pending channels, SIGTERM, wait 5s, SIGKILL.
- **Environment:** Subprocess inherits `os.Environ()` plus any additional env vars from the spawn request.

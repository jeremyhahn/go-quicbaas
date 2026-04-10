# MCP Channel Server

## Overview

The channel server pushes events into active Claude Code sessions. While hooks flow from Claude Code to QuicBaaS (inbound), channels flow from QuicBaaS to Claude Code (outbound). This enables real-time coordination: task assignments, status updates, and cross-agent messaging arrive in the agent's context without polling.

```
QuicBaaS Messaging ──> ChannelServer ──stdio──> Claude Code Session
                                     <──stdio── (reply tool)
```

Events appear as `<channel source="quicbaas">` tags inside the Claude Code session.

## Architecture

`ChannelServer` (`pkg/orchestration/channel_server.go`) implements the MCP protocol over stdio using JSON-RPC 2.0. Claude Code spawns it as a subprocess and communicates via newline-delimited JSON on stdin/stdout.

The server runs two concurrent loops:

- **Read loop** -- reads JSON-RPC requests from stdin (initialize handshake, tool calls, pings).
- **Drain loop** -- dequeues buffered `ChannelEvent` structs and writes them as `notifications/claude/channel` JSON-RPC notifications to stdout.

Events are buffered until the MCP initialization handshake completes. The default buffer size is 256 events.

## Channel Server Modes

### Local Mode (in-process)

The channel server subscribes directly to QuicBaaS messaging channels in the same process. No network required.

```
Claude Code <--stdio--> ChannelServer <--in-process--> QuicBaaS Messaging
```

Set `QuicBaaSAddress` to empty in the config.

### Remote Proxy Mode

The channel server runs as a local process on the agent machine but connects to a remote QuicBaaS server via gRPC/QUIC for event sourcing. Acts as a thin proxy between Claude Code and the remote QuicBaaS instance.

```
Claude Code <--stdio--> ChannelServer <--gRPC/QUIC--> Remote QuicBaaS Server
```

Set `QuicBaaSAddress` to the remote server address.

## Configuration

### ChannelServerConfig

```go
type ChannelServerConfig struct {
    ServerName            string // MCP server name (channel source identifier)
    Instructions          string // Added to Claude's system prompt
    EnableReply           bool   // Enable the reply tool for two-way communication
    EnablePermissionRelay bool   // Enable remote permission approval/denial
    EventBufferSize       int    // Event channel buffer size (default: 256)
    QuicBaaSAddress       string // Remote server address (empty = in-process)
}
```

### `.mcp.json`

Register the channel server as an MCP server in your project:

```json
{
  "mcpServers": {
    "quicbaas-channel": {
      "command": "quicbaas",
      "args": ["channel", "serve", "--server-name", "quicbaas", "--enable-reply"],
      "type": "stdio"
    }
  }
}
```

For remote proxy mode:

```json
{
  "mcpServers": {
    "quicbaas-channel": {
      "command": "quicbaas",
      "args": [
        "channel", "serve",
        "--server-name", "quicbaas",
        "--enable-reply",
        "--address", "quicbaas-server.internal:9090"
      ],
      "type": "stdio"
    }
  }
}
```

## CLI Command

```bash
quicbaas channel serve [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--server-name` | `quicbaas` | MCP server name (appears as channel source) |
| `--enable-reply` | `false` | Enable the reply tool for two-way communication |
| `--enable-permission-relay` | `false` | Enable remote permission approval/denial |
| `--buffer-size` | `256` | Event channel buffer size |
| `--address` | (empty) | Remote QuicBaaS address (empty = in-process) |
| `--instructions` | (empty) | Instructions added to Claude's system prompt |

## Event Types

Events pushed through the channel carry a content string and optional metadata:

```go
type ChannelEvent struct {
    Content  string            `json:"content"`
    Metadata map[string]string `json:"meta,omitempty"`
}
```

Common event categories:

| Event | Source | Description |
|---|---|---|
| Task assignment | Task store | A new task is available for this agent |
| Task status change | Messaging | Another agent completed/failed a task |
| Agent broadcast | Messaging | Cross-agent coordination message |
| RAG update | Vector store | New codebase context indexed |

Events are serialized as JSON-RPC notifications:

```json
{
  "jsonrpc": "2.0",
  "method": "notifications/claude/channel",
  "params": {
    "content": "New task available: Implement WebSocket transport",
    "meta": {
      "task_id": "task_abc123",
      "team_name": "platform-team",
      "priority": "1"
    }
  }
}
```

## Two-Way Communication (Reply Tool)

When `EnableReply` is true, the channel server exposes a `reply` tool that Claude Code can invoke to send messages back through QuicBaaS messaging:

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "reply",
    "arguments": {
      "channel": "orchestrator:tasks:platform-team",
      "message": "Task task_abc123 accepted, starting work."
    }
  }
}
```

The `ReplyHandler` callback processes replies. Set it before calling `Start`:

```go
srv.SetReplyHandler(func(ctx context.Context, channel string, message string) error {
    return messagingSvc.Publish(ctx, channel, []byte(message))
})
```

## MCP Initialization Handshake

The channel server follows the standard MCP initialization sequence:

1. Claude Code sends `initialize` request.
2. Server responds with capabilities (including `claude/channel` experimental capability).
3. Claude Code sends `notifications/initialized` notification.
4. Server begins draining buffered events as channel notifications.

Events queued before initialization completes are re-buffered and delivered once the handshake finishes.

## Lifecycle

- `NewChannelServer(config, reader, writer, logger)` -- creates the server.
- `SetReplyHandler(handler)` -- registers the reply callback (before Start).
- `Start(ctx)` -- blocks, running read and drain loops until context cancellation or Stop.
- `PushEvent(ctx, event)` -- queues an event for delivery.
- `Stop()` -- gracefully shuts down (idempotent).
- `Initialized()` -- reports whether the MCP handshake completed.

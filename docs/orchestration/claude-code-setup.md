# Claude Code Setup Guide

## Prerequisites

- Claude Code installed and authenticated
- QuicBaaS binary built with orchestration support:
  ```bash
  make build WITH_RAG=1 WITH_VECTOR=1 WITH_MCP=1
  ```
- QuicBaaS server running (standalone or cluster mode)

## Step 1: Start QuicBaaS

Start a standalone instance for local development:

```bash
quicbaasd start
```

By default, QuicBaaS listens on `localhost:9090` with PebbleDB storage. For cluster mode, see the [multi-host documentation](multi-host.md).

## Step 2: Configure Hooks

Create or edit `.claude/settings.json` in your project root to send task lifecycle events to QuicBaaS.

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

For remote servers, replace `localhost:9090` with the QuicBaaS server address. See [hooks integration](hooks-integration.md) for full details on payload formats and error handling.

## Step 3: Configure MCP Server

Create or edit `.mcp.json` in your project root to register QuicBaaS MCP tools (RAG, KV, secrets, messaging):

```json
{
  "mcpServers": {
    "quicbaas": {
      "command": "quicbaas",
      "args": ["mcp", "serve"],
      "type": "stdio"
    }
  }
}
```

This gives Claude Code access to the orchestration MCP tools (`quicbaas_orchestrator_*`), RAG indexing, and agent cache.

## Step 4: Configure Channel Server

Add the channel server as a second MCP entry in `.mcp.json` to receive push events from QuicBaaS:

```json
{
  "mcpServers": {
    "quicbaas": {
      "command": "quicbaas",
      "args": ["mcp", "serve"],
      "type": "stdio"
    },
    "quicbaas-channel": {
      "command": "quicbaas",
      "args": ["channel", "serve", "--server-name", "quicbaas", "--enable-reply"],
      "type": "stdio"
    }
  }
}
```

See [channels documentation](channels.md) for configuration options and remote proxy mode.

## Step 5: Test the Integration

### Verify hooks are reachable

```bash
curl -s -X POST http://localhost:9090/api/v1/hooks/teammate-idle \
  -H "Content-Type: application/json" \
  -d '{
    "hook_event_name": "TeammateIdle",
    "teammate_name": "test-agent",
    "team_name": "test-team"
  }'
```

Expected response (no pending tasks):

```json
{"exit_code":0}
```

### Verify MCP tools

Start a Claude Code session and invoke a tool:

```json
{
  "tool": "quicbaas_orchestrator_list_backends",
  "arguments": {}
}
```

This should return the configured backends (ACP, Ollama).

### Verify channel server

Start a Claude Code session with the channel MCP configured. The channel server should complete the MCP initialization handshake. Check QuicBaaS logs for:

```
channel server: MCP initialization complete
```

## Step 6: Register Your Project

Use the MCP discover and register tools to set up orchestration:

```json
{
  "tool": "quicbaas_orchestrator_discover",
  "arguments": {
    "root_path": "/path/to/your/project"
  }
}
```

Then register:

```json
{
  "tool": "quicbaas_orchestrator_register",
  "arguments": {
    "name": "my-project",
    "path": "/path/to/your/project",
    "default_backend": "claude"
  }
}
```

## Common Issues

### Hooks return 501 Not Implemented

The task store or messaging service is not configured. Ensure QuicBaaS is started with orchestration support enabled in the config YAML:

```yaml
orchestration:
  enabled: true
  backends:
    claude:
      type: acp
      command: claude
      args: ["--chat"]
```

### Channel server does not receive events

- Confirm the channel server process is running (`ps aux | grep "quicbaas channel"`).
- Check that the MCP initialization handshake completed (look for `MCP initialization complete` in logs).
- In remote mode, verify the `--address` flag points to the correct QuicBaaS server.

### MCP tools not available in Claude Code

- Verify `.mcp.json` is in the project root.
- Confirm the `quicbaas` binary is on `PATH`.
- Check that QuicBaaS was built with `WITH_MCP=1`.

### Connection refused on hooks

- Ensure QuicBaaS is running and the port matches the hook URL.
- For remote setups, verify network connectivity and firewall rules.
- Check TLS configuration if using `https://` URLs.

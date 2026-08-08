# quicbaas mcp

Speak the **Model Context Protocol** to a QuicBaaS server from the CLI.

## Why a bridge

The QuicBaaS daemon publishes its full MCP tool registry over HTTP
(`server.mcp.port`, TLS-terminated like every other listener). MCP hosts —
Claude Code, Claude Desktop, opencode — do not dial HTTP: they spawn a
child process and speak JSON-RPC 2.0 over its stdin/stdout.

`quicbaas mcp serve --stdio` is that missing piece. It relays each
JSON-RPC message between the host's pipe and the configured server.

The bridge owns **no** tool registry of its own. Tools, resources, the
sealed-window gate, and the authorization decision point all live in the
daemon (`pkg/api/mcp`); a client-side registry would fork that surface and
drift. `initialize`, `tools/list`, and `tools/call` are forwarded
verbatim, so the host negotiates directly with the server and sees the
real tools with the server's own authorization applied.

## Subcommands

| Command | Description |
|---------|-------------|
| `serve --stdio` | Bridge an MCP host's stdio pipe onto a QuicBaaS server |
| `tools` | Call `tools/list` and print the server's reply |
| `call <tool> [--args JSON]` | Call `tools/call` for one tool |

## Transport selection

Every verb in this group speaks MCP, so the group selects the MCP
transport automatically. An explicit `--transport` always wins — and then
fails loudly if it cannot carry MCP framing, rather than silently
switching protocols.

Authentication and TLS use the same material as every other CLI command:
the stored platform token plus `--tls-ca`, `--tls-cert`, `--tls-key`.
`--address` and `--timeout` are honoured as usual.

## quicbaas mcp serve

```bash
quicbaas mcp serve --stdio --address quicbaas.example.com:11443
```

Register it with an MCP host in `.mcp.json`:

```json
{
  "mcpServers": {
    "quicbaas": {
      "command": "quicbaas",
      "args": ["mcp", "serve", "--stdio", "--address", "quicbaas.example.com:11443"]
    }
  }
}
```

Behaviour:

- Blocks until stdin reaches EOF or the process is interrupted
  (`SIGINT` / `SIGTERM`).
- A transport failure becomes a JSON-RPC error response **for that
  request only**, so one bad call cannot desynchronize the stream.
- Notifications (messages with no `id`) are acknowledged locally and not
  forwarded: each bridged call is an independent HTTP request, so there
  is no client session for them to update.
- Nothing but well-formed JSON-RPC is ever written to stdout; logs go to
  stderr (or `--log-file`).

`--stdio` is required. It is the only transport an MCP host can spawn;
the flag is explicit so future transports can be added without changing
the default behaviour.

## quicbaas mcp tools / call

```bash
quicbaas mcp tools
quicbaas mcp call quicbaas_health
quicbaas mcp call quicbaas_kv_get --args '{"entityType":"user","id":1}'
```

`--args` must be a JSON object; anything else is rejected locally before
a request is sent. Results are printed as re-indented JSON — the schema
belongs to the server, so the CLI does not impose a table shape on it.

## Related

- `quicbaas channel serve` — a *different* stdio MCP process that pushes
  QuicBaaS events into a Claude Code session. It is an event channel, not
  a bridge to the server's tool registry.
- `--transport mcp` on any other command group — routes that command's
  typed SDK call over MCP tool invocations.

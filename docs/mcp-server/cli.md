# MCP Server — CLI

> **Status: NORMATIVE** · Last verified: 2026-09-23
> Every verb and flag, with runnable examples. Feature folder: `docs/mcp-server/`.

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

The MCP endpoint is **its own listener**: it is served on
`server.mcp.port` at `/mcp`, only when `server.mcp.enabled: true`. It is
not a path on the REST port, so `--address` must name the MCP port.

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

Register it with Claude Code (user scope, mTLS):

```bash
claude mcp add quicbaas -s user -- \
  quicbaas mcp serve --stdio --address quicbaas.example.com:11443 \
  --tls --tls-ca /path/to/server-ca.crt \
  --tls-cert /path/to/agent.crt --tls-key /path/to/agent.key
```

Or with any MCP host in `.mcp.json`:

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
- Notifications (messages with no `id`) — including the host's
  `notifications/initialized`, which completes the MCP handshake — are
  forwarded on the same session. They take no reply, so a notification
  the server rejects is reported on stderr, never on stdout.
- The server's endpoint is session-based: the host's own `initialize`
  opens the session and every later request carries it. If the server
  expires the session mid-conversation (`404`, "session terminated"), the
  bridge replays the handshake **once** — the host's last successful
  `initialize`, then `notifications/initialized` — and retries the
  request. If there is no `initialize` to replay, or the
  recovery fails, the request gets the original error; the bridge never
  loops.
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

Each invocation opens its own MCP session with the full lifecycle
handshake — `initialize`, then `notifications/initialized` — before the
real request. `--args` must be a JSON object; anything else is rejected
locally before a request is sent. Results are printed as re-indented
JSON — the schema belongs to the server, so the CLI does not impose a
table shape on it.

## Troubleshooting a 404

A `404` has two very different causes:

- **MCP is not served at this address.** The port is wrong (for example
  the REST port), or `server.mcp.enabled` is `false`. Every request —
  including `initialize` — gets Go's stock `404 page not found`. Because
  `initialize` is always the first request, the MCP client reports this
  as `server returned 4xx for initialize POST, likely a legacy SSE
  server`. `tools` and `call` print it as
  `MCP initialize failed: ... likely a legacy SSE server`. The bridge
  returns it as the JSON-RPC error for the host's `initialize`.
- **No live session.** A non-`initialize` request without a valid
  `Mcp-Session-Id`. The CLI verbs and the bridge handle this themselves.

Tell them apart by sending `initialize` directly:

```bash
curl -sS -i --cacert server-ca.crt --cert agent.crt --key agent.key \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}' \
  https://quicbaas.example.com:11443/mcp
```

A `200` with an `Mcp-Session-Id` header means the endpoint is correct. A
`404 page not found` means MCP is not served there: check
`server.mcp.enabled` and `server.mcp.port`.

## Related

- `quicbaas channel serve` — a *different* stdio MCP process that pushes
  QuicBaaS events into a Claude Code session. It is an event channel, not
  a bridge to the server's tool registry.
- `--transport mcp` on any other command group — routes that command's
  typed SDK call over MCP tool invocations.

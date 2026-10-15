# WebSocket — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/websocket/`.

**No CLI surface.** `quicbaas` registers no WebSocket command group: there is no
entry in the root command list (`cmd/quicbaas/main.go:181-246`) and no
`newWebSocket*Command` constructor in `cmd/quicbaas/`.

This is not an oversight in the CLI. As [api.md](api.md) records, the WebSocket
server in `pkg/api/websocket` is not mounted on any listener, so there is
nothing for a client verb to connect to. A CLI surface would be the second
thing to build, not the first.

The nearest working commands are in the messaging feature, which registers
subscriptions on the same channels over the mounted transports. They do not
stream messages to the terminal:

```bash
quicbaas msg subscribe <channel>
quicbaas msg watch <key-prefix>
```

See [`../messaging/cli.md`](../messaging/cli.md).

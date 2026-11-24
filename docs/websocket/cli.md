# WebSocket — CLI

> **Status: NORMATIVE** · Last verified: 2026-11-24
> Feature folder: `docs/websocket/`.

**No CLI surface.** `quicbaas` registers no WebSocket command group: there is no
entry in the root command list (`cmd/quicbaas/main.go:181-246`) and no
`newWebSocket*Command` constructor in `cmd/quicbaas/`.

The WebSocket server is mounted at `GET /api/v1/ws` on the API listener (see
[api.md "Mounting"](api.md#mounting)), and any MQTT, AMQP 0-9-1 or STOMP over
WebSocket client connects to it. `quicbaas` has no verb of its own for it.

The nearest working commands are in the messaging feature, which registers
subscriptions on the same channels over the mounted transports. They do not
stream messages to the terminal:

```bash
quicbaas msg subscribe <channel>
quicbaas msg watch <key-prefix>
```

See [`../messaging/cli.md`](../messaging/cli.md).

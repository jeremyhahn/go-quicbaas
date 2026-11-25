# WebSocket — API

> **Status: NORMATIVE** · Last verified: 2026-11-22
> The WebSocket surface as it exists in this repository. Feature folder:
> `docs/websocket/`.

## Mounting

The server is mounted at **`GET /api/v1/ws`** on the API listener.
`buildRESTServer` (`pkg/server/lifecycle_rest.go`) builds it with
`subprotocol.NewMessagingServer` and sets it as the route's handler with
`SetWebSocketHandler`; `restapi.Server.Shutdown` closes it with the listener.
The client selects `mqtt`, `amqp` or `stomp` with `Sec-WebSocket-Protocol`;
the three share one in-memory pub/sub hub.

In FIPS-only mode (`GODEBUG=fips140=only`) `websocket.NewServer` refuses to
start, the node logs `WebSocket endpoint disabled`, and the route stays
registered and answers 503.

## Transport matrix

| Capability | Form | State |
|---|---|---|
| Connection upgrade | `GET /api/v1/ws` → `Server.ServeHTTP` (`pkg/api/websocket/server.go:298`) → `Server.HandleHTTP` (`:93`) | implemented, **mounted** |
| Subprotocol negotiation | `Server.selectSubprotocol` (`:151`), `parseSubprotocols` (`:282`) | implemented, reachable |
| Default protocol | `Server.handleDefaultProtocol` (`:195`) | implemented, reachable (no `Sec-WebSocket-Protocol`) |
| Channel broadcast | `Server.BroadcastToChannel` (`:242`) | implemented, no caller outside the package |
| Session inspection | `GetSession` (`:227`), `GetSessionCount` (`:232`), `GetSessionStats` (`:237`) | implemented, no caller outside the package |
| Subprotocol registration | `Server.RegisterSubprotocol` (`:73`), `GetSubprotocols` (`:80`) | implemented, called by `subprotocol.NewMessagingServer` |

There are no REST, gRPC, QUIC, MCP, Unix or embedded rows: WebSocket is a
transport in its own right, not a capability carried over the six. The
`../multiprotocol-api/` folder covers those.

## Registered subprotocols

`pkg/api/websocket/subprotocol/` holds:

| Subprotocol | Files | State |
|---|---|---|
| `echo` | `echo.go` | real handler |
| `mqtt` | `mqtt.go`, `mqtt_stub.go` | build-tag-gated; the stub is a no-op |
| `amqp` | `amqp.go`, `amqp_stub.go` | build-tag-gated; the stub is a no-op |
| `stomp` | `stomp.go`, `stomp_stub.go` | build-tag-gated; the stub is a no-op |

The default `make` build (`WITH_MQTT`, `WITH_AMQP` and `WITH_STOMP` default
to 1) compiles the real handlers. A build without the `quicbaas_mqtt`,
`quicbaas_amqp` or `quicbaas_stomp` tag compiles the `_stub.go` file instead:
the server still negotiates the subprotocol name, but the stub closes the
session.

`engineering.md` covers the build tags that select the real handlers.

## Authentication

`GET /api/v1/ws` requires the `messaging:subscribe` permission, the same grant
as `POST /api/v1/messaging/subscribe`. The upgrade request is authenticated and
authorized by the same go-qrdb middleware as every other API route: no
credential gives 401, a credential without the grant gives 403, and only then
does the WebSocket server see the request.

The server's own `Authenticator` is nil (`NoopAuthenticator`) because the check
has already happened before the upgrade. A cross-origin browser upgrade is
refused by `websocket.Accept`'s Origin check (the Origin must match the Host,
or be absent).

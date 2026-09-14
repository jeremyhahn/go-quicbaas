# WebSocket — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The WebSocket surface as it exists in this repository. Feature folder:
> `docs/websocket/`.

## The WebSocket server is not mounted on any listener

**`pkg/api/websocket` is imported by nothing outside itself.** A repository-wide
search for the import path returns fourteen files, and every one of them is
inside `pkg/api/websocket/` or its `subprotocol/` subpackage. No route in
`pkg/api/rest/routes.go` mounts `Server.HandleHTTP`
(`pkg/api/websocket/server.go:82`); `pkg/server/`'s listener startup never
constructs a `websocket.Server`; and there is no `/ws` pattern in the server
route table.

The consequence, stated plainly because it governs everything else on this page:
**a running `quicbaasd` does not accept WebSocket connections.** The package
compiles, it is unit-tested, and it is unreachable.

This is documented rather than omitted for the reason `feature-docs.md` §5 gives
for `MISSING` rows: an absent page is indistinguishable from an unwritten one,
and the surrounding documentation asserts a working WebSocket surface —
`architecture.md` in this folder describes the connection lifecycle in detail,
and the repository README lists "WebSocket pub/sub" as a capability. Those
describe the package, not a reachable endpoint.

**This is an owner question, not something this migration decided.** Either the
mount is missing and is a defect, or the package is staged for a listener that
has not landed. Both readings are consistent with the code; the documentation
cannot choose between them.

## Transport matrix

| Capability | Form | State |
|---|---|---|
| Connection upgrade | `Server.HandleHTTP` (`pkg/api/websocket/server.go:82`) | implemented, **not mounted** |
| Subprotocol negotiation | `Server.selectSubprotocol` (`:140`), `parseSubprotocols` (`:271`) | implemented, not reachable |
| Default protocol | `Server.handleDefaultProtocol` (`:184`) | implemented, not reachable |
| Channel broadcast | `Server.BroadcastToChannel` (`:231`) | implemented, not reachable |
| Session inspection | `GetSession` (`:216`), `GetSessionCount` (`:221`), `GetSessionStats` (`:226`) | implemented, not reachable |
| Subprotocol registration | `Server.RegisterSubprotocol` (`:62`), `GetSubprotocols` (`:69`) | implemented, not reachable |

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

The `_stub.go` files are what a default build compiles. Advertising
MQTT, AMQP or STOMP support from a stock binary is therefore inaccurate twice
over: the subprotocol is a no-op, and the server carrying it is not mounted.

`engineering.md` covers the build tags that select the real handlers.

## Authentication

`websocket.NewServer` (`pkg/api/websocket/server.go:46`) takes an
`Authenticator`. Because no caller constructs the server, nothing in this
repository decides what that authenticator is. Any deployment that mounts this
server must supply one — an unauthenticated WebSocket upgrade on a
network-facing listener is an open channel into the messaging bus.

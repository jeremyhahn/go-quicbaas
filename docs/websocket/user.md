# WebSocket — User Guide

> **Status: NORMATIVE** · Last verified: 2026-11-24
> Feature folder: `docs/websocket/`.

## Connecting

A running `quicbaasd` accepts WebSocket upgrades at **`GET /api/v1/ws`** on the
API listener (see [api.md "Mounting"](api.md#mounting)).

- **Choose a subprotocol** with the `Sec-WebSocket-Protocol` header: `mqtt`,
  `amqp` (AMQP 0-9-1) or `stomp`. Any MQTT, AMQP 0-9-1 or STOMP over WebSocket
  client can connect.
- **Authenticate** as for any other API route. The upgrade needs a verified
  tenant and the `messaging:subscribe` permission: no credential gives 401, a
  credential without the grant gives 403 (see
  [api.md "Authentication"](api.md#authentication)). Publishing on the
  connection also needs `messaging:publish`; without it MQTT refuses the
  publication, AMQP closes the channel with `ACCESS_REFUSED` and STOMP answers
  with an `ERROR` frame.
- **Build tags.** The real handlers compile only with the `quicbaas_mqtt`,
  `quicbaas_amqp` and `quicbaas_stomp` build tags, which the default `make`
  build sets. A build without one of them answers that subprotocol with the
  stub, which closes the session (see
  [api.md "Registered subprotocols"](api.md#registered-subprotocols)).
- **Tenants do not share a hub.** Each verified tenant gets its own messaging
  server and pub/sub hub; within a tenant, `mqtt`, `amqp` and `stomp` clients
  share that hub, so a message published over one reaches subscribers on the
  others.

## Related

The CLI does not speak WebSocket. `quicbaas msg subscribe <channel>` and
`quicbaas msg watch <key-prefix>` register subscriptions over the other
transports; see [`../messaging/user.md`](../messaging/user.md).

## Server wiring

The endpoint is already wired: [api.md "Mounting"](api.md#mounting) describes
how the node builds the server and mounts it on the REST router.

## FIPS 140-only mode

WebSocket is unavailable in FIPS 140-only deployments (`GODEBUG=fips140=only`).
RFC 6455 derives `Sec-WebSocket-Accept` with SHA-1, which the Go Cryptographic
Module refuses in that mode, so no handshake can complete. `websocket.NewServer`
checks `crypto/fips140.Enforced()` and returns
`WebSocketUnavailableInFIPSOnlyModeError` (`ErrCodeUnavailableInFIPSOnlyMode`)
instead of failing once per connection. The node logs `WebSocket endpoint
disabled` once at startup, and `GET /api/v1/ws` stays registered and answers
503. REST, gRPC, QUIC, MCP and Unix carry the same surface and are unaffected.

## Troubleshooting

| Symptom | Cause |
|---|---|
| 401 on `GET /api/v1/ws` | No credential on the upgrade request. |
| 403 on `GET /api/v1/ws` | No verified tenant, or the credential lacks `messaging:subscribe`. |
| 503 on `GET /api/v1/ws` | The node runs in FIPS 140-only mode; see [FIPS](#fips-140-only-mode). |
| `mqtt` / `amqp` / `stomp` negotiate, then the session closes | The binary was built without that subprotocol's build tag, so the `_stub.go` no-op answers. |
| Publishing is refused but subscribing works | The credential lacks `messaging:publish`. |
| A message from another tenant never arrives | Correct behaviour: tenants do not share a hub. |

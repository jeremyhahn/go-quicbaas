# WebSocket — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/websocket/`.

## Read this first

There is nothing to connect to. The WebSocket server implemented in
`pkg/api/websocket` is not wired to any listener in this repository — see
[api.md](api.md) for the evidence — so a running `quicbaasd` does not accept
WebSocket upgrades, and no amount of configuration turns one on.

This page exists so that finding is discoverable from the folder a reader would
actually open, rather than only from a code search they have no reason to run.

## What to use instead

For live channel data over a transport that is mounted:

```bash
# Follow a channel.
quicbaas msg watch orders

# Register a subscription and stream to it.
quicbaas msg subscribe orders
```

See [`../messaging/user.md`](../messaging/user.md) for channels, delta
publishing and deduplication, and [`../messaging/api.md`](../messaging/api.md)
for the transports those verbs actually travel over.

For server-sent streaming of orchestrator session events, the REST transport
implements `RequestStream` for exactly one method,
`Orchestrator/SubscribeSessionEvents`
(`sdk/go/transport/rest/transport.go:1419-1424`). That is the only streaming
read path the SDK offers today.

## FIPS 140-only mode

WebSocket is unavailable in FIPS 140-only deployments (`GODEBUG=fips140=only`).
RFC 6455 derives `Sec-WebSocket-Accept` with SHA-1, which the Go Cryptographic
Module refuses in that mode, so no handshake can complete. `websocket.NewServer`
checks `crypto/fips140.Enforced()` and returns
`WebSocketUnavailableInFIPSOnlyModeError` (`ErrCodeUnavailableInFIPSOnlyMode`)
instead of failing once per connection. Treat enabling WebSocket under
FIPS-only as a configuration error and log the refusal once at startup. REST,
gRPC, QUIC, MCP and Unix carry the same surface and are unaffected.

## If you are the one wiring the listener

The pieces are all present and unit-tested:

1. Construct the server with `websocket.NewServer(config, auth, log)`, which
   returns `(*Server, error)`. **The `Authenticator` is not optional in
   practice** — an unauthenticated upgrade on a network-facing listener is an
   open door into the messaging bus. Under FIPS 140-only mode it returns
   `WebSocketUnavailableInFIPSOnlyModeError`; see [FIPS](#fips-140-only-mode).
2. Register the subprotocols you want with `RegisterSubprotocol` (`:62`). In a
   default build, `mqtt`, `amqp` and `stomp` resolve to no-op stubs; only `echo`
   is a real handler without a build tag. See `engineering.md`.
3. Mount `Server.HandleHTTP` (`:82`) on the REST router.
4. Decide the bind posture. The REST listener advertises only `http/1.1` in
   ALPN, which a WebSocket upgrade is compatible with; the gRPC listener is not
   a candidate.

Whether that wiring should exist at all is an owner decision, not a
documentation one.

## Troubleshooting

| Symptom | Cause |
|---|---|
| Connection refused on any WebSocket URL | Correct behaviour: nothing serves one. |
| `mqtt` / `amqp` / `stomp` negotiate but do nothing | The default build compiles the `_stub.go` no-ops. |
| Documentation elsewhere describes a working WebSocket API | It describes the package. Trust this folder. |

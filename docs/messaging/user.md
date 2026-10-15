# Messaging — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to channels, publish/subscribe, delta publishing and
> deduplication. Package internals live in `engineering.md`.

Messaging is QuicBaaS's pub/sub layer: named channels, subscribers that receive
what is published to them, an optional delta mode for sending only what changed,
and deduplication so a message replayed by a retrying client is delivered once.

## Prerequisites

- A running server. Messaging is a server subsystem, not a client-side library:
  every verb below reaches `quicbaasd` over the transport selected by
  `--transport` and `--address`.
- Messaging enabled in the server config:

  ```yaml
  messaging:
    enabled: true        # default false — messaging is off unless you turn it on
    buffer_size: 4096    # default 4096, the notification hub buffer
    dedup_enabled: true  # default true
    dedup_window: 1m     # default 1m
  ```

  These four are among the settings that also accept an environment override,
  because they appear in the explicit list `pkg/config/loader.go` reads:
  `QUICBAAS_MESSAGING_ENABLED`, `QUICBAAS_MESSAGING_BUFFER_SIZE`,
  `QUICBAAS_MESSAGING_DEDUP_ENABLED`, `QUICBAAS_MESSAGING_DEDUP_WINDOW`.
  Settings *not* in that list are config-file-only — see `engineering.md`.
- Credentials with the messaging permissions. The route table requires
  `messaging:channel_create`, `messaging:publish`, `messaging:subscribe` and so
  on per verb; `api.md` has the full per-route mapping.

## First run

```bash
# 1. Create a channel.
quicbaas msg create-channel orders

# 2. Confirm it exists.
quicbaas msg list-channels
quicbaas msg channel-info orders

# 3. Subscribe in one terminal. The command blocks and prints messages
#    as they arrive.
quicbaas msg subscribe orders

# 4. Publish from another.
quicbaas msg publish orders '{"id":"ord-1","total":42.00}'
```

`subscribe` holds the connection open. To watch a channel without registering a
durable subscription, use `watch` instead:

```bash
quicbaas msg watch orders
```

## Common tasks

### Publish only what changed

Delta publishing sends a patch rather than a whole document, which matters when
subscribers hold large objects and updates are small.

```bash
quicbaas msg publish-delta orders '{"total":45.00}'
```

The subscriber receives the delta, not a merged document; merging is the
subscriber's job. `architecture.md` describes the delta representation.

### Stop receiving

```bash
quicbaas msg list-subscriptions
quicbaas msg unsubscribe <subscription-id>
```

`unsubscribe` takes the **subscription id**, not the channel name. That is the
single most common mistake with this command group: the id comes from
`list-subscriptions`, and passing a channel name gets you a not-found error that
does not say "you passed the wrong kind of identifier".

### Check that deduplication is working

```bash
quicbaas msg dedup-stats orders
```

A publisher that retries after a timeout will send the same message twice. With
`dedup_enabled: true`, the second copy is suppressed inside `dedup_window` and
the counter here goes up. If the counter stays at zero while you know you are
retrying, the window is shorter than your retry backoff.

### Remove a channel

```bash
quicbaas msg delete-channel orders
```

Deleting a channel drops its subscriptions. There is no undo and no soft-delete.

### Serve the MCP channel bridge

```bash
quicbaas channel serve
```

This exposes channels to MCP clients. It is a separate command group from `msg`
because it runs a server rather than issuing one request.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `subscribe` returns immediately with no error | The channel does not exist. `create-channel` first — subscribing does not auto-create. |
| Publishes succeed but no subscriber sees them | Subscribers attached after the publish. Messaging is not a durable log; there is no replay of messages published before a subscriber connected. |
| Messages stop arriving under load | The per-client send buffer filled and messages were **dropped**, not queued. `pkg/messaging/client.go:542` applies backpressure by discarding and incrementing `MessagesDropped`; a slow subscriber loses messages rather than stalling the publisher. Raise `messaging.buffer_size` (hub) or drain faster; `DefaultClientSendBufferSize` is 256 per client. |
| Duplicate deliveries | `dedup_enabled` is false, or `dedup_window` is shorter than the publisher's retry interval. |
| `permission denied` on a verb that used to work | The route's required permission is per-verb, not per-group. Check the exact `resource:action` pair for that route in `api.md` rather than granting the whole group. |
| Every call fails over `--transport quic` | QUIC carries the REST routes over HTTP/3, and `quicbaasd` has no HTTP/3 listener of its own. Point the client at an endpoint that serves the REST API over HTTP/3. See `../multiprotocol-api/architecture.md`. |

## What this feature does not do

- **No durable message log.** There is no offset, no replay, no consumer group.
  A subscriber sees what is published while it is attached.
- **No ordering guarantee across channels.** Order is per channel.
- **No Push API.** Push over REST, gRPC, QUIC, MCP and Unix is designed but not
  implemented; only the WebSocket delivery path exists. It is deliberately not
  documented in this folder — a feature folder describes what is built.

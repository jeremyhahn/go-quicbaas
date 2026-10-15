# Messaging — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-14
> Every verb and flag, with runnable examples. Feature folder: `docs/messaging/`.

## Overview

The `quicbaas msg` commands manage messaging channels and subscriptions and
publish messages. Every command goes through the SDK messaging client
(`Async().Messaging()`), so it runs on every transport the SDK offers.

`subscribe` and `watch` register a subscription and exit. The CLI does not
stream messages.

## Global Flags

```
--transport  Transport protocol (grpc, rest, quic, unix, mcp, embedded) [default: unix]
--address    Server address (Unix socket path or host:port)
--timeout    Request timeout [default: 30s]
--output     Output format (table, json, yaml) [default: table]
```

Every `msg` command checks `--output` before it contacts the server, so an
unknown format never leaves a half-finished change behind.

JSON and YAML output print the SDK types as they are. JSON keys use the Go
field names (`Name`, `BufferSize`); YAML keys are the same names in lower case
(`name`, `buffersize`).

## Channel Management

### msg create-channel

Create a notification channel for publishing messages.

```bash
quicbaas msg create-channel <name> [flags]
```

**Flags:**
```
--buffer-size       Channel buffer size [default: 256]
--max-subscribers   Maximum subscribers allowed [default: 1000]
--persistent        Enable message persistence
--ttl               Message time-to-live, a Go duration [default: 0s (no expiry)]
```

**Examples:**

```bash
quicbaas msg create-channel orders
quicbaas msg create-channel events --buffer-size 512 --ttl 1h
quicbaas msg create-channel notifications --persistent --max-subscribers 5000
quicbaas msg create-channel audit --output json
```

**Output:**
```
Channel created successfully.

Name:         orders
Buffer Size:  256
Persistent:   false
TTL:          0s (no expiry)
Created:      2026-01-21T10:30:00Z
```

### msg delete-channel

Delete a channel and disconnect all subscribers. Without `--force` the command
asks for confirmation and prints `Delete cancelled.` on any answer but `y`.

```bash
quicbaas msg delete-channel <name> [flags]
```

**Flags:**
```
--force    Delete without confirmation
```

**Examples:**

```bash
quicbaas msg delete-channel orders
quicbaas msg delete-channel orders --force
```

**Output:**
```
Channel 'orders' deleted successfully.
```

### msg list-channels

List all channels. The command has no flags of its own.

```bash
quicbaas msg list-channels
quicbaas msg list-channels --output json
```

**Output:**
```
NAME           SUBSCRIBERS  PERSISTENT  CREATED
orders         12           false       2026-01-20T08:00:00Z
events         45           true        2026-01-15T12:30:00Z

Total: 2 channels
```

### msg channel-info

Show a channel with its delivery and deduplication statistics.

```bash
quicbaas msg channel-info <name>
quicbaas msg channel-info orders --output yaml
```

**Output:**
```
Channel: orders
===============

Codec:                  json
Buffer Size:            256
Max History:            0
Persistent:             false
TTL:                    0s (no expiry)
Created:                2026-01-20T08:00:00Z

Statistics
  Active Subscribers:   12
  Messages Published:   1542
  Messages In History:  0

Deduplication
  Total Checks:         1542
  Duplicates Detected:  23
  Duplicate Rate:       1.49%
```

JSON and YAML output hold three objects: `Channel`, `Stats` and `Dedup`.

## Subscription Commands

### msg subscribe

Create a subscription on a channel and print it. The command exits once the
subscription exists.

```bash
quicbaas msg subscribe <channel> [flags]
```

**Flags:**
```
--delta          Enable delta mode (receive only changes)
--dedup          Include deduplication metadata
--dedup-skip     Skip duplicate messages silently
--filter         Only deliver messages whose key has this prefix
--buffer-size    Subscription buffer size [default: 256]
```

**Examples:**

```bash
quicbaas msg subscribe orders
quicbaas msg subscribe orders --delta
quicbaas msg subscribe orders --dedup-skip
quicbaas msg subscribe orders --filter "orders/eu/"
```

**Output:**
```
Subscription created successfully.

Subscription ID:  sub_abc123
Channel:          orders
Key Prefix:       orders/eu/
Created:          2026-01-21T10:30:00Z
```

### msg unsubscribe

Remove a subscription. Without `--force` the command asks for confirmation and
prints `Unsubscribe cancelled.` on any answer but `y`.

```bash
quicbaas msg unsubscribe <subscription-id> [flags]
```

**Flags:**
```
--force    Unsubscribe without confirmation
```

**Output:**
```
Subscription 'sub_abc123' removed successfully.
```

### msg list-subscriptions

List the number of active subscribers on each channel.

```bash
quicbaas msg list-subscriptions [flags]
```

**Flags:**
```
--channel    Show only this channel
```

**Examples:**

```bash
quicbaas msg list-subscriptions
quicbaas msg list-subscriptions --channel orders
quicbaas msg list-subscriptions --output json
```

**Output:**
```
CHANNEL  SUBSCRIBERS
orders   12
events   45
```

## Publishing Commands

### msg publish

Publish a message to a channel. Give the payload inline or from a file.

```bash
quicbaas msg publish <channel> [flags]
```

**Flags:**
```
--payload      Message payload (required unless --file)
--file         Read payload from file
--dedup-id     Deduplication key for idempotent publishing
--priority     Priority metadata (low, normal, high) [default: normal]
--ttl          Time-to-live metadata
```

`--priority` and `--ttl` travel as message metadata under the keys `priority`
and `ttl`.

**Examples:**

```bash
quicbaas msg publish orders --payload '{"order_id": "ord_001", "status": "created"}'
quicbaas msg publish orders --dedup-id "order-ord_001-created" --payload '{"order_id": "ord_001"}'
quicbaas msg publish orders --file ./message.json
quicbaas msg publish alerts --payload '{"level": "critical"}' --priority high --ttl 5m
```

**Output:**
```
Message published to channel 'orders'.
```

### msg publish-delta

Publish only the differences between two entity states. Both states must be
JSON, given inline or from a file.

```bash
quicbaas msg publish-delta <channel> [flags]
```

**Flags:**
```
--old           Previous state as JSON string
--new           Current state as JSON string
--old-file      Read previous state from file
--new-file      Read current state from file
--entity-id     Entity identifier metadata
--entity-type   Entity type metadata
--dedup-id      Deduplication key
```

`--entity-id` and `--entity-type` travel as message metadata under the keys
`entity_id` and `entity_type`.

**Examples:**

```bash
quicbaas msg publish-delta orders \
  --old '{"order_id": "ord_001", "status": "created"}' \
  --new '{"order_id": "ord_001", "status": "shipped"}'

quicbaas msg publish-delta orders \
  --old-file ./old-state.json \
  --new-file ./new-state.json

quicbaas msg publish-delta orders \
  --entity-id "ord_001" \
  --entity-type "Order" \
  --old '{"status": "processing"}' \
  --new '{"status": "shipped", "tracking": "TRK123"}'
```

**Output:**
```
Message published to channel 'orders'.
```

## Monitoring Commands

### msg watch

Create a subscription on KV changes under a key prefix and print it. The
command exits once the subscription exists.

The REST and QUIC servers have no watch route, so on those transports the
command returns the SDK's error.

```bash
quicbaas msg watch <key-prefix> [flags]
```

**Flags:**
```
--delta    Enable delta mode
```

**Examples:**

```bash
quicbaas msg watch orders/
quicbaas msg watch orders/ --delta
```

The output matches `msg subscribe`.

### msg dedup-stats

Show deduplication statistics for a channel. The command has no flags of its
own.

```bash
quicbaas msg dedup-stats <channel>
quicbaas msg dedup-stats orders --output json
```

**Output:**
```
Deduplication Statistics: orders
================================

Total Checks:         15234
Unique Messages:      14891
Duplicates Detected:  343 (2.25%)
Cache Size:           14891
Window:               1m0s
```

## Related Documentation

- [Architecture](architecture.md) - Messaging system design and delta publishing
- [API](api.md) - Messaging API reference

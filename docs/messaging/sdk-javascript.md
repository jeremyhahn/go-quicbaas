# JavaScript SDK - Messaging

The QuicBaaS JavaScript SDK provides a `MessagingClient` for pub/sub messaging with channels, subscriptions, delta publishing, key-prefix watching, and statistics.

## Installation

```bash
npm install @quicbaas/client
```

## Quick Start

```javascript
import { QuicBaaS } from '@quicbaas/client';

const db = new QuicBaaS('https://localhost:8080');
await db.auth().login('admin', 'password');

const messaging = db.messaging();

// Create a channel
const channel = await messaging.createChannel('events', {
  persistent: true,
  bufferSize: 256,
  ttl: 3600000,
});

// Subscribe to messages
const subscription = await messaging.subscribe('events', {
  onMessage: (msg) => {
    console.log(`Received: ${JSON.stringify(msg.payload)}`);
  },
  onError: (err) => {
    console.error(`Subscription error: ${err.message}`);
  },
});

// Publish a message
await messaging.publish('events', { event: 'order_created', orderId: '12345' });

// Cleanup
subscription.close();
await messaging.deleteChannel('events');
```

## API Reference

### Channel Operations

#### createChannel(name, options?)

Create a new messaging channel.

```javascript
const channel = await messaging.createChannel('events', {
  persistent: true,     // Persist messages to storage
  bufferSize: 256,      // Max messages buffered in memory
  ttl: 3600000,         // Message TTL in milliseconds
});
```

#### deleteChannel(name)

Delete a messaging channel.

```javascript
await messaging.deleteChannel('events');
```

#### getChannel(name)

Get information about a channel.

```javascript
const channel = await messaging.getChannel('events');
console.log(`Subscribers: ${channel.subscriberCount}`);
```

#### listChannels()

List all messaging channels.

```javascript
const channels = await messaging.listChannels();
for (const ch of channels) {
  console.log(`${ch.name} - ${ch.subscriberCount} subscribers`);
}
```

### Publishing

#### publish(channelName, payload, options?)

Publish a message to a channel.

```javascript
const response = await messaging.publish('events', { action: 'created' }, {
  codec: 'json',                  // json | msgpack | binary
  dedupKey: 'order-12345-create', // Prevent duplicate messages
});
console.log(`Published message: ${response.id}`);
```

#### publishDelta(channelName, oldValue, newValue, options?)

Publish a delta (diff between old and new values) to a channel.

```javascript
const response = await messaging.publishDelta(
  'users',
  { name: 'Alice', email: 'old@example.com' },
  { name: 'Alice', email: 'new@example.com' },
  { codec: 'json' },
);
```

### Subscribing

#### subscribe(channelName, options)

Subscribe to a channel via Server-Sent Events. Returns a `Subscription` object.

```javascript
const subscription = await messaging.subscribe('events', {
  onMessage: (msg) => {
    console.log(`[${msg.channel}] ${JSON.stringify(msg.payload)}`);
  },
  onError: (err) => {
    console.error(`Error: ${err.code} - ${err.message}`);
  },
});

// Check subscription state
console.log(`Subscription ID: ${subscription.id}`);
console.log(`Closed: ${subscription.closed}`);

// Close when done
subscription.close();
```

#### unsubscribe(subscriptionId)

Unsubscribe by server-assigned ID. Alternative to calling `subscription.close()` for cancelling from a different context.

```javascript
await messaging.unsubscribe(subscription.id);
```

### Watch

#### watchPrefix(prefix, options)

Watch a key prefix for changes via Server-Sent Events.

```javascript
const watcher = await messaging.watchPrefix('users/', {
  onEvent: (event) => {
    console.log(`Key change: ${JSON.stringify(event)}`);
  },
  onError: (err) => {
    console.error(`Watch error: ${err.message}`);
  },
});

// Stop watching
watcher.close();
```

### Statistics

#### getStats()

Get messaging system statistics.

```javascript
const stats = await messaging.getStats();
console.log(`Channels: ${stats.totalChannels}`);
console.log(`Subscribers: ${stats.totalSubscribers}`);
console.log(`Messages: ${stats.totalMessages}`);
console.log(`Throughput: ${stats.messagesPerSecond} msg/s`);
```

#### getDedupStats()

Get deduplication statistics.

```javascript
const dedup = await messaging.getDedupStats();
console.log(`Tracked keys: ${dedup.totalKeys}`);
console.log(`Duplicates blocked: ${dedup.duplicatesBlocked}`);
console.log(`Hit rate: ${dedup.hitRate}%`);
```

## Enums

### MessageCodec

Codec types for publish operations.

| Value | Description |
|-------|-------------|
| `'json'` | JSON-encoded payload |
| `'msgpack'` | MessagePack-encoded payload |
| `'binary'` | Raw binary payload |

```javascript
import { MessageCodec } from '@quicbaas/client';

await messaging.publish('events', data, { codec: MessageCodec.JSON });
```

### MessagingEventType

Event types received from subscriptions.

| Value | Description |
|-------|-------------|
| `'message'` | Standard message event |
| `'delta'` | Delta/diff event |
| `'error'` | Error event from server |

```javascript
import { MessagingEventType } from '@quicbaas/client';
```

## Type Definitions

### Channel

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Channel name |
| `persistent` | `boolean` | Whether the channel persists messages |
| `bufferSize` | `number` | Maximum messages buffered in memory |
| `ttl` | `number` | Message TTL in milliseconds |
| `subscriberCount` | `number` | Current number of subscribers |
| `createdAt` | `string` | ISO timestamp of channel creation |

### Message

| Field | Type | Description |
|-------|------|-------------|
| `id` | `string` | Unique message identifier |
| `channel` | `string` | Channel name |
| `payload` | `*` | Message payload |
| `codec` | `string` | Codec used for the payload |
| `dedupKey` | `string?` | Deduplication key (if provided) |
| `timestamp` | `string` | ISO timestamp of publication |

### DeltaPayload

| Field | Type | Description |
|-------|------|-------------|
| `oldValue` | `*` | Previous value |
| `newValue` | `*` | New value |
| `codec` | `string?` | Codec for encoding the delta |

### PublishResponse

| Field | Type | Description |
|-------|------|-------------|
| `id` | `string` | Published message identifier |
| `channel` | `string` | Channel name |
| `timestamp` | `string` | ISO timestamp of publication |

### Stats

| Field | Type | Description |
|-------|------|-------------|
| `totalChannels` | `number` | Total number of channels |
| `totalSubscribers` | `number` | Total active subscribers |
| `totalMessages` | `number` | Total messages published |
| `messagesPerSecond` | `number` | Current message throughput |

### DedupStats

| Field | Type | Description |
|-------|------|-------------|
| `totalKeys` | `number` | Total deduplication keys tracked |
| `duplicatesBlocked` | `number` | Total duplicate messages blocked |
| `hitRate` | `number` | Cache hit rate percentage |

### Subscription

| Field | Type | Description |
|-------|------|-------------|
| `id` | `string?` | Server-assigned subscription identifier |
| `closed` | `boolean` | Whether the subscription has been closed |
| `close()` | `Function` | Close the subscription and release resources |

## Examples

### Real-Time Event Handling

```javascript
import { QuicBaaS, MessagingEventType } from '@quicbaas/client';

const db = new QuicBaaS('https://localhost:8080');
await db.auth().login('admin', 'password');
const messaging = db.messaging();

await messaging.createChannel('orders', { persistent: true });

const subscription = await messaging.subscribe('orders', {
  onMessage: (msg) => {
    const { event, orderId, total } = msg.payload;
    switch (event) {
      case 'order_created':
        console.log(`New order ${orderId}: $${total}`);
        break;
      case 'order_shipped':
        console.log(`Order ${orderId} shipped`);
        break;
    }
  },
  onError: (err) => {
    console.error(`Subscription error [${err.code}]: ${err.message}`);
  },
});

// Publish events
await messaging.publish('orders', {
  event: 'order_created',
  orderId: 'ORD-001',
  total: 99.99,
});

// Cleanup
subscription.close();
```

### Delta Publishing

```javascript
const messaging = db.messaging();

await messaging.createChannel('user-updates', { persistent: true });

// Subscribe to deltas
const subscription = await messaging.subscribe('user-updates', {
  onMessage: (msg) => {
    console.log(`User changed from ${JSON.stringify(msg.payload.old_value)} to ${JSON.stringify(msg.payload.new_value)}`);
  },
});

// Publish a delta
await messaging.publishDelta(
  'user-updates',
  { name: 'Alice', role: 'user' },
  { name: 'Alice', role: 'admin' },
  { codec: 'json' },
);

subscription.close();
```

### Key Prefix Watching

```javascript
const messaging = db.messaging();

const watcher = await messaging.watchPrefix('config/', {
  onEvent: (event) => {
    console.log(`Config changed: ${JSON.stringify(event)}`);
  },
  onError: (err) => {
    console.error(`Watch error: ${err.message}`);
  },
});

// Stop watching when done
watcher.close();
```

### Deduplication

```javascript
const messaging = db.messaging();

// Both calls use the same dedupKey; the second publish is deduplicated
await messaging.publish('orders', { orderId: '123' }, {
  dedupKey: 'order-123-create',
});

await messaging.publish('orders', { orderId: '123' }, {
  dedupKey: 'order-123-create',
});

const dedup = await messaging.getDedupStats();
console.log(`Duplicates blocked: ${dedup.duplicatesBlocked}`);
```

## Error Handling

The messaging client throws typed errors from the SDK error hierarchy.

```javascript
import {
  ValidationError,
  NotFoundError,
  NetworkError,
  QuicBaaSError,
} from '@quicbaas/client';

try {
  await messaging.publish('nonexistent-channel', { data: 'test' });
} catch (err) {
  if (err instanceof ValidationError) {
    console.error(`Invalid input [${err.details.field}]: ${err.message}`);
  } else if (err instanceof NotFoundError) {
    console.error(`Channel not found: ${err.message}`);
  } else if (err instanceof NetworkError) {
    console.error(`Network error: ${err.message}`);
  } else if (err instanceof QuicBaaSError) {
    console.error(`QuicBaaS error [${err.code}]: ${err.message}`);
  }
}
```

### Common Errors

| Error | Code | Cause |
|-------|------|-------|
| `ValidationError` | `VALIDATION_ERROR` | Missing or invalid parameters (empty name, non-function callback) |
| `NotFoundError` | `NOT_FOUND` | Channel or subscription does not exist |
| `AuthenticationError` | `AUTHENTICATION_ERROR` | Invalid or expired credentials |
| `AuthorizationError` | `AUTHORIZATION_ERROR` | Insufficient permissions |
| `NetworkError` | `NETWORK_ERROR` | Connection failure or SSE stream interrupted |
| `ServerError` | `SERVER_ERROR` | Internal server error |
| `QuicBaaSError` | `PARSE_ERROR` | Failed to parse incoming SSE event data |
| `QuicBaaSError` | `SUBSCRIPTION_ERROR` | Failed to establish or maintain SSE subscription |

### Subscription Error Handling

SSE subscriptions surface errors through the `onError` callback rather than throwing.

```javascript
const subscription = await messaging.subscribe('events', {
  onMessage: (msg) => {
    // Process messages
  },
  onError: (err) => {
    // err.code is PARSE_ERROR or SUBSCRIPTION_ERROR
    console.error(`[${err.code}]: ${err.message}`);
  },
});
```

## Related Documentation

- [Go SDK](sdk-go.md) - Go client reference
- [Delta Publishing](delta-publishing.md) - Delta computation and wire format
- [Architecture](architecture.md) - System design overview
- [REST API Reference](api-reference-rest.md) - REST endpoint reference

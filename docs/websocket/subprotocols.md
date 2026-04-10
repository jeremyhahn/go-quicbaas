# WebSocket Subprotocols

## Overview

QuicBaaS supports multiple WebSocket subprotocols for interoperability with existing messaging systems. Each subprotocol is compiled conditionally via build tags to minimize binary size.

| Subprotocol | Build Tag | Protocol Version | Status | Use Case |
|-------------|-----------|------------------|--------|----------|
| `echo` | (always) | - | **Stable** | Testing and debugging |
| `pubsub` | (always) | - | **Stable** | Publish/subscribe messaging |
| `mqtt` | `quicbaas_mqtt` | 3.1.1, 5.0 | **Planned** | IoT, mobile, lightweight messaging |
| `amqp` | `quicbaas_amqp` | 0-9-1 | **Planned** | Enterprise messaging, queues |
| `stomp` | `quicbaas_stomp` | 1.2 | **Planned** | Simple text-based messaging |

> **Note:** The MQTT, AMQP, and STOMP subprotocols are currently stub implementations included as build-tag-gated placeholders for future development. When compiled without the corresponding build tag, a no-op stub is used. Full implementations are planned for a future release.

## Echo Subprotocol

**Build:** Always included

The echo subprotocol provides a simple request-response pattern for testing WebSocket connectivity and message handling.

### Behavior

- Receives any message type (text or binary)
- Echoes the exact message back to the sender
- Updates session activity on each message
- Tracks message counts for metrics

### Usage

```javascript
const ws = new WebSocket('wss://quicbaas:8443/ws', ['echo']);

ws.onopen = () => {
    ws.send('Hello, QuicBaaS!');
};

ws.onmessage = (event) => {
    console.log('Echo:', event.data); // "Hello, QuicBaaS!"
};
```

### Implementation

```go
func (h *EchoHandler) HandleConnection(ctx context.Context, conn *websocket.Conn, session *Session) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        msgType, data, err := conn.Read(ctx)
        if err != nil {
            return err
        }

        h.messagesReceived.Add(1)
        session.UpdateActivity()

        if err := conn.Write(ctx, msgType, data); err != nil {
            return err
        }
        h.messagesSent.Add(1)
    }
}
```

## MQTT over WebSocket

**Build Tag:** `quicbaas_mqtt`

```bash
go build -tags quicbaas_mqtt ./cmd/dragond
```

### Protocol Support

- MQTT 3.1.1 (protocol version 4)
- MQTT 5.0 (protocol version 5)
- QoS levels 0, 1, and 2
- Retain flag support
- Topic wildcards (`+` and `#`)

### Packet Types

| Packet | Direction | Description |
|--------|-----------|-------------|
| CONNECT | Client -> Server | Initiate connection |
| CONNACK | Server -> Client | Connection acknowledgment |
| PUBLISH | Bidirectional | Publish message to topic |
| PUBACK | Bidirectional | QoS 1 acknowledgment |
| PUBREC | Bidirectional | QoS 2 received |
| PUBREL | Bidirectional | QoS 2 release |
| PUBCOMP | Bidirectional | QoS 2 complete |
| SUBSCRIBE | Client -> Server | Subscribe to topics |
| SUBACK | Server -> Client | Subscription acknowledgment |
| UNSUBSCRIBE | Client -> Server | Unsubscribe from topics |
| UNSUBACK | Server -> Client | Unsubscription acknowledgment |
| PINGREQ | Client -> Server | Keep-alive ping |
| PINGRESP | Server -> Client | Keep-alive response |
| DISCONNECT | Client -> Server | Clean disconnect |

### Connection Example

```javascript
// Browser with MQTT.js over WebSocket
const client = mqtt.connect('wss://quicbaas:8443/ws', {
    protocol: 'mqtt',
    protocolVersion: 5,
    clean: true,
    keepalive: 60,
});

client.on('connect', () => {
    client.subscribe('sensors/+/temperature', { qos: 1 });

    client.publish('sensors/room1/temperature', JSON.stringify({
        value: 22.5,
        unit: 'celsius',
        timestamp: Date.now()
    }), { qos: 1 });
});

client.on('message', (topic, message) => {
    console.log(`${topic}: ${message.toString()}`);
});
```

### Go Client Example

```go
import mqtt "github.com/eclipse/paho.mqtt.golang"

opts := mqtt.NewClientOptions()
opts.AddBroker("wss://quicbaas:8443/ws")
opts.SetProtocolVersion(5)
opts.SetClientID("go-client-1")

client := mqtt.NewClient(opts)
if token := client.Connect(); token.Wait() && token.Error() != nil {
    log.Fatal(token.Error())
}

// Subscribe
client.Subscribe("sensors/#", 1, func(c mqtt.Client, m mqtt.Message) {
    log.Printf("Topic: %s, Payload: %s", m.Topic(), m.Payload())
})

// Publish
client.Publish("sensors/room1/humidity", 1, false, "45.2")
```

### Handler Architecture

```go
type MQTTHandler struct {
    hub              PubSubHub
    messagesReceived atomic.Uint64
    messagesSent     atomic.Uint64
    clientsMu        sync.RWMutex
    clients          map[string]*mqttClientState
}

type mqttClientState struct {
    clientID        string
    protocolVersion byte
    keepalive       uint16
    cleanSession    bool
    subscriptions   map[string]byte // topic filter -> QoS
    mu              sync.RWMutex
}
```

## AMQP 0-9-1 over WebSocket

**Build Tag:** `quicbaas_amqp`

```bash
go build -tags quicbaas_amqp ./cmd/dragond
```

### Protocol Support

- AMQP 0-9-1 specification
- Channel multiplexing
- Exchange types (direct, fanout, topic)
- Queue management
- Transaction support

### Frame Types

| Type | Value | Description |
|------|-------|-------------|
| Method | 1 | RPC-style method frames |
| Header | 2 | Content header properties |
| Body | 3 | Content body data |
| Heartbeat | 8 | Connection keep-alive |

### Class/Method Support

**Connection Class (10):**
- Start, Start-Ok, Secure, Secure-Ok
- Tune, Tune-Ok, Open, Open-Ok
- Close, Close-Ok

**Channel Class (20):**
- Open, Open-Ok, Flow, Flow-Ok
- Close, Close-Ok

**Exchange Class (40):**
- Declare, Declare-Ok, Delete, Delete-Ok

**Queue Class (50):**
- Declare, Declare-Ok, Bind, Bind-Ok
- Unbind, Unbind-Ok, Purge, Purge-Ok
- Delete, Delete-Ok

**Basic Class (60):**
- Qos, Qos-Ok, Consume, Consume-Ok
- Cancel, Cancel-Ok, Publish, Return
- Deliver, Get, Get-Ok, Get-Empty
- Ack, Reject, Recover-Async, Recover, Recover-Ok

**Tx Class (90):**
- Select, Select-Ok, Commit, Commit-Ok
- Rollback, Rollback-Ok

### Connection Example

```javascript
// Using amqplib with WebSocket
const amqp = require('amqplib');

async function main() {
    const conn = await amqp.connect('amqp://quicbaas:8443/ws');
    const channel = await conn.createChannel();

    // Declare queue
    await channel.assertQueue('tasks', { durable: true });

    // Publish
    channel.sendToQueue('tasks', Buffer.from(JSON.stringify({
        task: 'process_order',
        orderId: '12345'
    })));

    // Consume
    channel.consume('tasks', (msg) => {
        console.log('Received:', msg.content.toString());
        channel.ack(msg);
    });
}
```

### Handler Architecture

```go
type AMQPHandler struct {
    hub              PubSubHub
    messagesReceived atomic.Uint64
    messagesSent     atomic.Uint64
    clientsMu        sync.RWMutex
    clients          map[string]*amqpClientState
}

type amqpClientState struct {
    connected    bool
    vhost        string
    channels     map[uint16]*amqpChannelState
    consumers    map[string]amqpConsumer
    mu           sync.RWMutex
}

type amqpChannelState struct {
    open            bool
    flowActive      bool
    transacted      bool
    queues          map[string]*amqpQueueState
    exchanges       map[string]*amqpExchangeState
    prefetchSize    uint32
    prefetchCount   uint16
    prefetchGlobal  bool
    unackedMessages map[uint64]*amqpUnackedMessage
    nextDeliveryTag uint64
}
```

### Frame Format

```
+------+----------+--------+---------+----------+
| Type | Channel  | Size   | Payload | Frame-End|
| 1B   | 2B       | 4B     | N bytes | 1B (0xCE)|
+------+----------+--------+---------+----------+
```

## STOMP 1.2 over WebSocket

**Build Tag:** `quicbaas_stomp`

```bash
go build -tags quicbaas_stomp ./cmd/dragond
```

### Protocol Support

- STOMP 1.0, 1.1, 1.2
- Header escaping (1.2)
- Transactions (BEGIN, COMMIT, ABORT)
- ACK modes: auto, client, client-individual
- Receipt support

### Commands

**Client Commands:**

| Command | Required Headers | Description |
|---------|------------------|-------------|
| CONNECT/STOMP | accept-version, host | Initiate connection |
| SUBSCRIBE | destination, id | Subscribe to destination |
| UNSUBSCRIBE | id | Cancel subscription |
| SEND | destination | Send message |
| ACK | id | Acknowledge message |
| NACK | id | Negative acknowledge |
| BEGIN | transaction | Start transaction |
| COMMIT | transaction | Commit transaction |
| ABORT | transaction | Rollback transaction |
| DISCONNECT | - | Clean disconnect |

**Server Frames:**

| Frame | Description |
|-------|-------------|
| CONNECTED | Connection established |
| MESSAGE | Deliver message to subscriber |
| RECEIPT | Acknowledge receipt request |
| ERROR | Error notification |

### Frame Format

```
COMMAND
header1:value1
header2:value2

body^@
```

Where `^@` is the NULL byte (0x00).

### Header Escaping (STOMP 1.2)

| Escape | Character |
|--------|-----------|
| `\r` | Carriage return (13) |
| `\n` | Line feed (10) |
| `\c` | Colon (58) |
| `\\` | Backslash (92) |

Note: CONNECT/CONNECTED frames do NOT escape headers for backward compatibility.

### Connection Example

```javascript
const Stomp = require('@stomp/stompjs');

const client = new Stomp.Client({
    brokerURL: 'wss://quicbaas:8443/ws',
    connectHeaders: {
        'accept-version': '1.2',
        'host': 'quicbaas'
    }
});

client.onConnect = (frame) => {
    // Subscribe
    client.subscribe('/queue/orders', (message) => {
        console.log('Received:', message.body);
        message.ack();
    }, { ack: 'client' });

    // Send
    client.publish({
        destination: '/queue/orders',
        body: JSON.stringify({ orderId: '12345' }),
        headers: { 'content-type': 'application/json' }
    });
};

client.activate();
```

### ACK Modes

| Mode | Behavior |
|------|----------|
| `auto` | Messages auto-acknowledged on delivery |
| `client` | Cumulative ACK - acknowledges all previous messages |
| `client-individual` | Individual ACK - acknowledges single message |

### Transaction Example

```javascript
// Begin transaction
client.publish({
    destination: '/queue/tx-test',
    headers: { transaction: 'tx-1' },
    body: 'message 1'
});

client.publish({
    destination: '/queue/tx-test',
    headers: { transaction: 'tx-1' },
    body: 'message 2'
});

// Commit (both messages delivered atomically)
client.publish({
    destination: '',
    headers: { transaction: 'tx-1' },
    body: '',
    // COMMIT command
});
```

### Handler Architecture

```go
type STOMPHandler struct {
    hub              PubSubHub
    messagesReceived atomic.Uint64
    messagesSent     atomic.Uint64
    clientsMu        sync.RWMutex
    clients          map[string]*stompClientState
}

type stompClientState struct {
    connected     bool
    version       string
    session       string
    subscriptions map[string]stompSubscription
    transactions  map[string]*stompTransaction
    pendingAcks   map[string]*pendingMessage
    mu            sync.RWMutex
    ackCounter    uint64
}
```

## PubSubHub Interface

All subprotocol handlers share a common `PubSubHub` interface for topic-based messaging:

```go
type PubSubHub interface {
    Subscribe(clientID string, topicFilter string, qos byte) error
    Unsubscribe(clientID string, topicFilter string) error
    Publish(topic string, payload []byte, qos byte, retain bool) error
    RegisterClient(clientID string, callback func(topic, payload, qos, retain)) error
    UnregisterClient(clientID string) error
}
```

### In-Memory Implementation

```go
type InMemoryPubSubHub struct {
    subscriptions map[string]map[string]byte  // clientID -> topicFilter -> QoS
    callbacks     map[string]func(...)        // clientID -> callback
    retained      map[string]retainedMessage  // topic -> message
    mu            sync.RWMutex
}
```

### Topic Matching

Supports MQTT-style wildcards:
- `+` matches single level: `sensors/+/temp` matches `sensors/room1/temp`
- `#` matches remaining levels: `sensors/#` matches `sensors/room1/temp/celsius`

## Error Types

Each subprotocol defines typed errors:

**MQTT:**
```go
var ErrMQTTDisconnect = errors.New("mqtt: client disconnected")

type ErrMQTTProtocolError struct {
    Message string
}
```

**AMQP:**
```go
var ErrAMQPDisconnect = errors.New("amqp: client disconnected")

type ErrAMQPProtocolError struct {
    Message string
}

type ErrAMQPChannelError struct {
    ReplyCode uint16
    ReplyText string
    ClassID   uint16
    MethodID  uint16
}
```

**STOMP:**
```go
var ErrSTOMPDisconnect = errors.New("stomp: client disconnected")

type ErrSTOMPProtocolError struct {
    Message string
}

type ErrSTOMPMissingHeader struct {
    Header string
}

type ErrSTOMPInvalidEscape struct {
    Sequence string
}
```

## File Reference

| File | Description |
|------|-------------|
| `subprotocol/echo.go` | Echo handler (always compiled) |
| `subprotocol/mqtt.go` | MQTT handler (quicbaas_mqtt) |
| `subprotocol/mqtt_stub.go` | MQTT stub (!quicbaas_mqtt) |
| `subprotocol/amqp.go` | AMQP handler (quicbaas_amqp) |
| `subprotocol/amqp_stub.go` | AMQP stub (!quicbaas_amqp) |
| `subprotocol/stomp.go` | STOMP handler (quicbaas_stomp) |
| `subprotocol/stomp_stub.go` | STOMP stub (!quicbaas_stomp) |
| `subprotocol/pubsub.go` | PubSubHub interface and in-memory implementation |

## Related Documentation

- [WebSocket Architecture](architecture.md) - Server and session management
- [Messaging Architecture](../messaging/architecture.md) - Push notification system
- [Delta Publishing](../messaging/delta-publishing.md) - Efficient change delivery

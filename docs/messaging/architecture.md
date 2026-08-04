# Push Notification System Architecture

## Overview

The messaging system provides real-time event delivery to connected clients using a channel-based pub/sub architecture. It is designed for high throughput with lock-free hot paths and graceful backpressure handling.

## Component Diagram

```mermaid
graph TB
    subgraph "Public API Layer"
        PS[PushService]
    end

    subgraph "Core Components"
        NH[NotificationHub]
        DC[DeltaComputer]
        CR[CodecResolver]
        DD[Deduplicator]
    end

    subgraph "Client Layer"
        C1[Client]
        C2[Client]
        WSC[WebSocketClient]
    end

    subgraph "Storage"
        KV[KVStore]
    end

    PS --> NH
    PS --> DC
    PS --> CR
    PS --> DD

    NH --> C1
    NH --> C2
    WSC --> NH
    WSC -.->|hubBridge| C1

    DD --> KV
```

## Core Components

### NotificationHub

**Location:** `pkg/messaging/hub.go`

The NotificationHub is the central coordinator for all pub/sub operations. It uses a single-threaded event loop pattern with Go channels to eliminate mutex contention in the hot path.

**Key Design Decisions:**

- **Single-threaded event loop:** All state mutations occur in one goroutine via channel-based dispatch
- **256-buffer default:** Provides backpressure handling without blocking publishers
- **No locks in hot path:** All operations use non-blocking channel sends with fallback behavior

**Architecture:**

```mermaid
flowchart LR
    subgraph "Event Loop (single goroutine)"
        direction TB
        REG[register]
        UNREG[unregister]
        SUB[subscribe]
        UNSUB[unsubscribe]
        BC[broadcast]
        CREATE[createChannel]
        DELETE[deleteChannel]
    end

    CLIENTS[clients map] --> REG
    CHANNELS[channels map] --> SUB

    REG --> LOOP{select}
    UNREG --> LOOP
    SUB --> LOOP
    UNSUB --> LOOP
    BC --> LOOP
    CREATE --> LOOP
    DELETE --> LOOP
```

**Channel Structure:**

```go
type NotificationHub struct {
    clients       map[*Client]bool            // Active subscribers
    channels      map[string]map[*Client]bool // channel -> subscribers
    broadcast     chan *Message               // Inbound messages (buffered: 256)
    register      chan *Client                // New client registrations
    unregister    chan *Client                // Client removal
    subscribe     chan *SubscribeRequest      // Channel subscriptions
    unsubscribe   chan *UnsubscribeRequest    // Channel unsubscriptions
    createChannel chan *CreateChannelRequest  // Channel creation
    deleteChannel chan *DeleteChannelRequest  // Channel deletion
    // ...
}
```

**Backpressure Handling:**

When a client's send buffer is full, the hub disconnects the slow consumer rather than blocking:

```go
func (h *NotificationHub) sendToClient(client *Client, msg *Message) {
    select {
    case client.Send <- msg:
        // Message sent successfully
    default:
        // Client is slow - disconnect it
        h.handleUnregister(client)
    }
}
```

### Client

**Location:** `pkg/messaging/hub.go` (Client struct)

Represents a connected subscriber with dedicated send/done channels for lifecycle management.

```go
type Client struct {
    ID            string
    Send          chan *Message    // Outbound messages (buffered: 256)
    Done          chan struct{}    // Disconnection signal
    Metadata      map[string]string
    subscriptions map[string]bool  // Channel memberships
    connected     atomic.Bool
}
```

### WebSocketClient

**Location:** `pkg/messaging/client.go`

WebSocket-based client implementation with read/write pumps and hub integration via the hubBridge goroutine.

**Goroutine Architecture:**

```mermaid
flowchart TB
    subgraph "WebSocketClient"
        RP[readPump goroutine]
        WP[writePump goroutine]
        HB[hubBridge goroutine]
    end

    CONN[ClientConnection] --> RP
    WP --> CONN

    HC[hubClient.Send] --> HB
    HB --> SEND[send channel]
    SEND --> WP

    RP --> HANDLERS[Message Handlers]
    HANDLERS --> |subscribe/unsubscribe| HUB[NotificationHub]
```

**Message Dispatch:**

Uses map-based dispatch for O(1) message type routing:

```go
var wsClientMessageHandlers = map[ClientMessageType]wsMessageHandler{
    ClientMessageTypeSubscribe:   handleWSSubscribeMessage,
    ClientMessageTypeUnsubscribe: handleWSUnsubscribeMessage,
    ClientMessageTypePublish:     handleWSPublishMessage,
    ClientMessageTypePing:        handleWSPingMessage,
}
```

### PushService

**Location:** `pkg/messaging/service.go`

Service facade that provides the public API. Coordinates the hub, delta computer, codec resolver, and deduplicator.

**Interface:**

```go
type Service interface {
    // Channel operations
    CreateChannel(ctx context.Context, name string, opts ...CreateChannelOption) (*Channel, error)
    DeleteChannel(ctx context.Context, name string) error
    GetChannel(ctx context.Context, name string) (*Channel, error)
    ListChannels(ctx context.Context) ([]*Channel, error)

    // Subscription operations
    Subscribe(ctx context.Context, channel string, opts ...SubscribeOption) (*Subscription, error)
    Unsubscribe(ctx context.Context, subscriptionID string) error

    // Publishing
    Publish(ctx context.Context, channel string, payload interface{}, opts ...PublishOption) error
    PublishDelta(ctx context.Context, channel string, old, new interface{}, opts ...PublishOption) error

    // Lifecycle
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}
```

### DeltaComputer

**Location:** `pkg/messaging/delta.go`

Computes deltas between entity states using reflection. Supports custom diff logic via the Diffable interface.

**Delta Computation Flow:**

```mermaid
flowchart TB
    INPUT[old, new entities] --> CHECK{implements Diffable?}
    CHECK -->|yes| DIFFABLE[Call Diff method]
    CHECK -->|no| REFLECT[Reflection-based comparison]

    REFLECT --> RECURSE[Recursive field comparison]
    RECURSE --> |structs| COMPARE_STRUCT[compareStructs]
    RECURSE --> |maps| COMPARE_MAP[compareMaps]
    RECURSE --> |slices| COMPARE_SLICE[compareSlices]
    RECURSE --> |primitives| COMPARE_PRIM[DeepEqual]

    DIFFABLE --> CHANGES[FieldChange list]
    COMPARE_STRUCT --> CHANGES
    COMPARE_MAP --> CHANGES
    COMPARE_SLICE --> CHANGES
    COMPARE_PRIM --> CHANGES

    CHANGES --> PAYLOAD[DeltaPayload]
```

**Diffable Interface:**

```go
type Diffable interface {
    Diff(previous interface{}) ([]FieldChange, error)
    Apply(changes []FieldChange) error
    Version() uint64
}
```

**Output Format:**

```go
type DeltaPayload struct {
    EntityID    string
    EntityType  string
    Version     uint64
    PrevVersion uint64
    Timestamp   time.Time
    Changes     []FieldChange
}

type FieldChange struct {
    Path      string      // JSON path: "user.address.city"
    Operation OpType      // set, delete, append, increment
    Value     interface{} // New value
    OldValue  interface{} // Previous value (optional)
}
```

### CodecResolver

**Location:** `pkg/messaging/codec.go`

Resolves codecs with priority-based resolution. Built-in support for JSON and MessagePack.

**Resolution Priority:**

1. Message-level override (highest)
2. Subscription-level preference
3. Channel default
4. Session negotiated codec
5. System default: JSON (lowest)

**Supported Codecs:**

| Codec | Format | Use Case |
|-------|--------|----------|
| json | JSON | Default, human-readable |
| msgpack | MessagePack | Compact binary format |
| protobuf | Protocol Buffers | Schema-defined, cross-language |

### Deduplicator

**Location:** `pkg/messaging/dedup.go`

Content-based and ID-based message deduplication using KVStore persistence.

**Deduplication Flow:**

```mermaid
flowchart TB
    MSG[Message] --> KEY{Has ID?}
    KEY -->|yes| USE_ID[Use message ID as key]
    KEY -->|no| HASH[Compute SHA256 hash of payload]

    USE_ID --> LOOKUP[Lookup in KVStore]
    HASH --> LOOKUP

    LOOKUP --> EXISTS{Entry exists?}
    EXISTS -->|no| STORE[Store new entry]
    EXISTS -->|yes| WINDOW{Within TTL window?}

    WINDOW -->|no| STORE
    WINDOW -->|yes| INCREMENT[Increment count]

    STORE --> NEW[Return: not duplicate]
    INCREMENT --> DUP[Return: is duplicate]
```

**Storage Key Format:**

```
_messaging/dedup/{channel}/{dedup_key}
```

## Data Flow

### Message Publishing

```mermaid
sequenceDiagram
    participant P as Publisher
    participant PS as PushService
    participant DD as Deduplicator
    participant NH as NotificationHub
    participant C as Client

    P->>PS: Publish(channel, payload)
    PS->>DD: CheckAndStore(channel, msg)
    DD-->>PS: DedupInfo

    alt is duplicate
        PS-->>P: nil (silently dropped)
    else not duplicate
        PS->>NH: Broadcast(msg)
        NH->>NH: handleBroadcast()
        loop for each subscriber
            NH->>C: sendToClient(msg)
            C->>C: Send channel <- msg
        end
        PS-->>P: nil
    end
```

### Client Subscription

```mermaid
sequenceDiagram
    participant C as WebSocketClient
    participant NH as NotificationHub
    participant HC as hubClient

    C->>C: readPump receives subscribe msg
    C->>C: handleWSSubscribeMessage()
    C->>C: Subscribe(channel)
    C->>NH: SubscribeToChannel(hubClient, channel)
    NH->>NH: handleSubscribe()

    alt channel exists
        NH->>NH: Add client to channel map
        NH-->>C: nil
    else channel not exists
        NH->>NH: Auto-create channel
        NH->>NH: Add client to channel map
        NH-->>C: nil
    end
```

## Cluster Mode

In cluster mode, each node maintains its own NotificationHub. Events are distributed via Raft replication.

```mermaid
flowchart TB
    subgraph "Node 1 (Leader)"
        PS1[PushService]
        NH1[NotificationHub]
        SM1[State Machine]
    end

    subgraph "Node 2 (Follower)"
        PS2[PushService]
        NH2[NotificationHub]
        SM2[State Machine]
    end

    subgraph "Node 3 (Follower)"
        PS3[PushService]
        NH3[NotificationHub]
        SM3[State Machine]
    end

    PS1 -->|Publish| SM1
    SM1 -->|Raft Log| SM2
    SM1 -->|Raft Log| SM3

    SM1 -->|Apply| NH1
    SM2 -->|Apply| NH2
    SM3 -->|Apply| NH3

    NH1 --> C1[Local Clients]
    NH2 --> C2[Local Clients]
    NH3 --> C3[Local Clients]
```

**Replication Flow:**

1. Client publishes message to leader node
2. Leader appends to Raft log
3. Log entry replicated to followers
4. Each node's state machine calls `hub.Broadcast()` on apply
5. Clients connected to any node receive the message

## Error Handling

All errors follow the QRDBError pattern with typed errors:

```go
type QRDBError struct {
    Code    ErrorCode
    Op      string
    Err     error
    Context map[string]interface{}
    Retry   bool
}
```

**Key Error Types:**

| Error | Code | Description |
|-------|------|-------------|
| ChannelNotFoundError | 1000 | Channel does not exist |
| ChannelExistsError | 1001 | Channel already exists |
| HubBufferFullError | 1002 | Hub buffer full (retryable) |
| ClientSlowError | 1004 | Client consuming too slowly |
| SubscriptionNotFoundError | 1006 | Subscription not found |

## Configuration

### Hub Options

```go
NewNotificationHubWithBufferSize(logger, 256) // Default buffer size

// Channel options
WithBufferSize(512)      // Per-channel buffer
WithMaxSubscribers(1000) // Subscriber limit
WithPersistent(true)     // Enable persistence
WithTTL(time.Hour)       // Message TTL
```

### Service Options

```go
NewPushService(logger, storage,
    WithServiceBufferSize(256),
    WithServiceDefaultCodec("json"),
    WithServiceDedupEnabled(true),
    WithServiceDedupWindow(time.Minute),
)
```

## Performance Characteristics

| Metric | Value | Notes |
|--------|-------|-------|
| Hot path locks | 0 | Channel-based coordination |
| Buffer size | 256 | Default, configurable |
| Slow consumer action | Disconnect | Prevents head-of-line blocking |
| Hash algorithm | SHA256 | For content deduplication |
| Codec default | JSON | Switchable to msgpack |

## File Reference

| File | Purpose |
|------|---------|
| `pkg/messaging/hub.go` | NotificationHub, Client, channel management |
| `pkg/messaging/client.go` | WebSocketClient with read/write pumps |
| `pkg/messaging/service.go` | MessagingService facade |
| `pkg/messaging/delta.go` | DeltaComputer, Diffable interface |
| `pkg/messaging/codec.go` | CodecResolver, json/msgpack codecs |
| `pkg/messaging/dedup.go` | DedupStoreImpl |
| `pkg/messaging/message.go` | Message type definitions |
| `pkg/messaging/types.go` | Shared types (DeltaPayload, Subscription, etc.) |
| `pkg/messaging/errors.go` | Typed errors and error constructors |

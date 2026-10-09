# Push Notification System Roadmap

> **Status: REFERENCE** · Last verified: 2026-08-18
> Implementation status of the QuicBaaS messaging system.

This document tracks the implementation status of the QuicBaaS messaging system.

## Overview

The messaging system provides real-time event delivery to connected clients using a channel-based pub/sub architecture with support for delta publishing, multiple transport protocols, and seamless integration with QuicBaaS's watch system.

## Implementation Status

### Phase 1: Core Push Infrastructure

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| Create `pkg/messaging/` package with types and errors | Done | Core types in `types.go`, errors in `errors.go` |
| Implement NotificationHub | Done | Channel-based, single-threaded event loop in `hub.go` |
| Implement Client with read/write pumps | Done | WebSocketClient in `client.go` |
| Implement PushService | Done | Service facade accepts KVStore from server |
| Implement codec resolver with struct tag detection | Done | Priority-based resolution in `codec.go` |
| Implement delta types (DeltaPayload, FieldChange, OpType) | Done | Defined in `types.go` |
| Implement DeltaComputer (reflection-based diff) | Done | Supports Diffable interface for custom implementations |
| Implement dedup logic | Done | Stores in KVStore with `_messaging/dedup/` prefix |
| Write unit tests | Done | 90%+ coverage achieved |

**Key Files:**
- `pkg/messaging/hub.go` - NotificationHub, Client, channel management
- `pkg/messaging/client.go` - WebSocketClient with read/write pumps
- `pkg/messaging/service.go` - MessagingService facade
- `pkg/messaging/delta.go` - DeltaComputer, Diffable interface
- `pkg/messaging/codec.go` - CodecResolver, json/msgpack codecs
- `pkg/messaging/dedup.go` - DedupStoreImpl
- `pkg/messaging/message.go` - Message type definitions
- `pkg/messaging/types.go` - Shared types
- `pkg/messaging/errors.go` - Typed errors

---

### Phase 1.5: Watch System Delta Enhancement

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| Modify NotifyPut signature | Done | Added `prevValue` parameter: `NotifyPut(key, prevValue, value []byte, revision, version int64)` |
| Update state machine to pass prevValue to NotifyPut | Done | FastStateMachine captures prevValue before write |
| Add WithDeltaMode() watch option | Done | Enables delta computation for watch |
| Add WithIncludeOldValues() watch option | Done | Include previous values in delta changes |
| Add WithEntityType() watch option | Done | Required for delta computation (pointer to struct type) |
| Implement real-time delta computation in notify() | Done | Uses DeltaComputer.ComputeBytes() in sendEvent() |
| Add Delta field to WatchEvent | Done | `*messaging.DeltaPayload` field added |
| NewManagerWithDelta constructor | Done | Creates manager with DeltaComputer and Logger |
| Write unit tests for delta watch mode | Done | 91.3% coverage achieved |

**Key Files:**
- `pkg/watch/manager.go` - Delta computation in sendEvent(), new options
- `pkg/watch/types.go` - Updated WatchEvent with Delta field
- `pkg/watch/delta_test.go` - Comprehensive delta watch tests
- FastStateMachine WatchManager interface and prevValue capture are handled by the qrdb engine

**New NotifyPut signature:**
```go
NotifyPut(key, prevValue, value []byte, revision, version int64)
```

**New watch options:**
```go
Watch(prefix,
    WithDeltaMode(),           // Enable delta computation
    WithIncludeOldValues(),    // Include old values in delta
    WithEntityType(&Entity{}), // Entity type for JSON unmarshaling
)
```

**Delta event structure:**
```go
type WatchEvent struct {
    Type     EventType
    Kv       *KeyValue          // Full value (when delta mode disabled)
    PrevKv   *KeyValue          // Optional previous value
    Delta    *messaging.DeltaPayload // Changed fields only (when delta mode enabled)
    Revision int64
}
```

---

### Phase 2: WebSocket Transport

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| Implement WebSocket server | Done | Using `coder/websocket` library |
| Implement SubprotocolHandler interface | Done | Abstraction for protocol handlers |
| Implement subprotocol negotiation | Done | Sec-WebSocket-Protocol handling |
| Implement Echo subprotocol | Done | For testing and validation |
| Implement authentication flow | Done | Token-based authentication |
| Implement 256-shard SessionManager | Done | FNV-1a hash distribution |
| Write unit tests | Done | 90%+ coverage achieved |

**Key Files:**
- `pkg/api/websocket/server.go` - WebSocket server with upgrade handling
- `pkg/api/websocket/types.go` - Session, ServerConfig, interfaces
- `pkg/api/websocket/session_manager.go` - 256-shard session management
- `pkg/api/websocket/errors.go` - Typed error definitions
- `pkg/api/websocket/subprotocol/echo.go` - Echo subprotocol handler

**Design Notes:**
- WebSocket server on port 8443 (shared with REST)
- Subprotocol negotiation via `Sec-WebSocket-Protocol` header
- Connection lifecycle: connect -> authenticate -> subscribe -> receive

---

### Phase 2.5: Cluster Integration

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| Implement ClusterInfo interface | Done | `pkg/cluster/info.go` - cluster topology information |
| Implement NonLeaderNodes() | Done | `pkg/cluster/info_impl.go` - follower node discovery |
| Add `/cluster/nodes/non-leader` REST endpoint | Done | REST API exposure in `handlers_cluster.go` |
| WatchBridge cluster integration | Done | Leader-gated event delivery |
| Write unit tests | Done | 90%+ coverage achieved |

**Key Files:**
- `pkg/cluster/info.go` - ClusterInfo interface definition
- `pkg/cluster/info_impl.go` - NonLeaderNodes() implementation
- `pkg/api/rest/handlers_cluster.go` - REST endpoint

**Design Notes:**
- Enables clients to discover follower nodes for read-heavy workloads
- Supports external load balancer integration

---

### Phase 3: WebSocket Subprotocols

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| MQTT subprotocol | Done | Build tag: `quicbaas_mqtt`, MQTT 3.1.1 and 5.0 |
| AMQP subprotocol | Done | Build tag: `quicbaas_amqp`, AMQP 0-9-1 |
| STOMP subprotocol | Done | Build tag: `quicbaas_stomp`, STOMP 1.2 |
| PubSubHub interface | Done | Shared topic-based pub/sub abstraction |
| In-memory PubSubHub | Done | Wildcard support (+, #) |
| Stub files | Done | No-op implementations when build tags disabled |
| Write unit tests | Done | 90%+ coverage achieved |

**Key Files:**
- `pkg/api/websocket/subprotocol/mqtt.go` - MQTT handler (quicbaas_mqtt)
- `pkg/api/websocket/subprotocol/mqtt_stub.go` - MQTT stub (!quicbaas_mqtt)
- `pkg/api/websocket/subprotocol/amqp.go` - AMQP handler (quicbaas_amqp)
- `pkg/api/websocket/subprotocol/amqp_stub.go` - AMQP stub (!quicbaas_amqp)
- `pkg/api/websocket/subprotocol/stomp.go` - STOMP handler (quicbaas_stomp)
- `pkg/api/websocket/subprotocol/stomp_stub.go` - STOMP stub (!quicbaas_stomp)
- `pkg/api/websocket/subprotocol/pubsub.go` - PubSubHub interface and in-memory implementation

**Design Notes:**
- Subprotocols compiled conditionally via build tags
- Each subprotocol implements SubprotocolHandler interface
- Default build includes only Echo protocol

---

### Phase 4: Watch Manager Integration

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| Implement WatchBridge | Done | `pkg/watchbridge/watch_bridge.go` (888 lines) |
| Key prefix to channel mapping | Done | `watch:/prefix` -> messaging channel mapping |
| Event type filtering | Done | Filter by PUT, DELETE, etc. |
| Delta computation | Done | Real-time delta computation in bridge |
| Leader gating | Done | Leader-only event delivery via atomic flag |
| Write unit tests | Done | 16 test functions, 90%+ coverage |

**Key Files:**
- `pkg/watchbridge/watch_bridge.go` - WatchBridge implementation
- `pkg/watchbridge/watch_bridge_test.go` - Comprehensive test suite

**Design Notes:**
- WatchBridge subscribes to database changes and publishes to messaging channels
- Automatic channel creation for watched prefixes
- Configurable event filtering
- Lock-free leader gating using `atomic.Int32`

---

### Phase 4.5: Message Deduplication

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| Dedup storage with TTL | Done | Automatic expiration of dedup entries in `dedup.go` |
| Content-based deduplication | Done | SHA256 hash of payload |
| ID-based deduplication | Done | Message ID as dedup key |
| Dedup statistics API | Done | REST, gRPC, Proto, CLI endpoints |
| Write unit tests | Done | 90%+ coverage achieved |
| Integration tests | Done | Multi-protocol dedup validation |

**Key Files:**
- `pkg/messaging/dedup.go` - DedupStoreImpl with TTL and SHA256
- `pkg/messaging/dedup_test.go` - Comprehensive dedup tests

**Design Notes:**
- Dedup entries stored in KVStore with prefix `_messaging/dedup/{channel}/`
- Configurable TTL window (default: 1 minute)
- Statistics exposed via REST, gRPC, and CLI interfaces

---

### Phase 5: Multi-Protocol API

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| REST handlers with SSE | Done | Server-Sent Events for streaming |
| gRPC service | Done | Bidirectional streaming |
| QUIC opcodes | Done | Low-latency transport |
| MCP tools | Done | AI/LLM integration |
| Unix socket support | Done | Local IPC |
| Write unit tests | Done | 90%+ coverage achieved |

**Key Files:**
- `pkg/api/rest/messaging/` - REST handlers
- `pkg/api/grpc/messaging/` - gRPC service
- `pkg/api/quic/messaging/` - QUIC handlers
- `pkg/api/mcp/messaging/` - MCP tools
- `pkg/api/unix/messaging/` - Unix socket handlers

**Protocol Ports:**

| Protocol | Transport | Port |
|----------|-----------|------|
| REST/SSE | HTTP/HTTPS | 8443 |
| gRPC | TCP | 10443 |
| QUIC | UDP | 9444 |
| MCP | stdio/HTTP | 9445 |
| Unix | Unix Socket | /run/dragond.sock |

---

### Phase 5.5: Refactoring

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| Refactor messaging package | Done | Clean interfaces and service structure |
| Update service dependencies | Done | Service layer cleanup |
| SDK transport integration | Done | Unified transport layer |

---

### Phase 6: SDKs

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| Go SDK with DeltaApplier[E] | Done | Generic delta application in sdk/go |
| Multiple transport support | Done | gRPC, REST, QUIC, Unix, Embedded, MCP |
| Go messaging client | Done | Pub/sub, delta publishing, watch |
| All Go service clients | Done | KV, Batch, Transaction, Query, and more |
| JavaScript SDK messaging client | Done | SSE-based pub/sub, channels, watch, stats |
| JavaScript SDK integration | Done | Integrated into QuicBaaS client facade |
| JavaScript SDK unit tests | Done | 161 tests, 100% statement/line/function coverage |

**Key Files:**
- `sdk/go/quicbaas.go` - Go unified client interface
- `sdk/go/client/messaging/` - Go messaging client
- `sdk/javascript/src/clients/messaging.js` - JS messaging client
- `sdk/javascript/test/clients/messaging.test.js` - JS messaging tests

**Go SDK Example:**
```go
type DeltaApplier[E any] interface {
    Apply(entity *E, delta *DeltaPayload) error
}
```

**JavaScript SDK Example:**
```javascript
const db = new QuicBaaS('https://localhost:8443');
const messaging = db.messaging();

await messaging.createChannel('events');
const sub = await messaging.subscribe('events', {
    onMessage: (msg) => console.log('Received:', msg),
});
await messaging.publish('events', { action: 'created' });
```

---

### Phase 7: CLI

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| CLI commands in pkg/cli/commands/messaging/ | Done | Command structure |
| Channel management commands | Done | create, delete, list |
| Messaging commands | Done | subscribe, publish, publish-delta |
| Watch command with streaming | Done | `dragonctl msg watch` |
| Statistics commands | Done | stats, dedup-stats |
| Write unit tests | Done | 90%+ coverage achieved |

**Key Files:**
- `pkg/cli/commands/messaging/` - CLI command implementations
- `pkg/cli/commands/messaging/channel.go` - Channel management
- `pkg/cli/commands/messaging/subscribe.go` - Subscribe command
- `pkg/cli/commands/messaging/publish.go` - Publish commands
- `pkg/cli/commands/messaging/watch.go` - Watch command
- `pkg/cli/commands/messaging/stats.go` - Statistics commands

**Available Commands:**
```
dragonctl messaging channel create <name>
dragonctl messaging channel delete <name>
dragonctl messaging channel list
dragonctl messaging subscribe <channel>
dragonctl messaging publish <channel> <payload>
dragonctl messaging publish-delta <channel> --entity-id <id>
dragonctl messaging watch <key-prefix>
dragonctl messaging stats
dragonctl messaging dedup-stats
```

---

### Phase 8: Integration Testing

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| Multi-protocol adapter pattern | Done | Shared test interface |
| All protocol adapters | Done | REST, gRPC, QUIC, MCP, Unix |
| Comprehensive test suite | Done | End-to-end scenarios |
| CLI integration tests | Done | Command validation |
| Delta mode testing | Done | Delta computation verification |

**Key Files:**
- `test/integration/messaging/messaging_test.go` - Main test suite
- `test/integration/messaging/adapters.go` - Shared adapter interface
- `test/integration/messaging/rest_adapter.go` - REST adapter
- `test/integration/messaging/grpc_adapter.go` - gRPC adapter
- `test/integration/messaging/quic_adapter.go` - QUIC adapter
- `test/integration/messaging/mcp_adapter.go` - MCP adapter
- `test/integration/messaging/unix_adapter.go` - Unix adapter

**Test Structure:**
```
test/integration/messaging/
    messaging_test.go
    adapters.go
    rest_adapter.go
    grpc_adapter.go
    quic_adapter.go
    mcp_adapter.go
    unix_adapter.go
```

**Test Pattern:**
```go
//go:build integration

func TestPushMultiProtocol(t *testing.T) {
    t.Run("CLI", func(t *testing.T) { testPushCLI(t, ctx) })
    t.Run("REST", func(t *testing.T) { testPushProtocol(t, ctx, &restAdapter{}) })
    t.Run("gRPC", func(t *testing.T) { testPushProtocol(t, ctx, &grpcAdapter{}) })
    t.Run("QUIC", func(t *testing.T) { testPushProtocol(t, ctx, &quicAdapter{}) })
    t.Run("MCP", func(t *testing.T) { testPushProtocol(t, ctx, &mcpAdapter{}) })
    t.Run("Unix", func(t *testing.T) { testPushProtocol(t, ctx, &unixAdapter{}) })
}
```

---

### Phase 9: Documentation

**Status:** COMPLETE

| Task | Status | Notes |
|------|--------|-------|
| Architecture documentation | Done | `docs/messaging/architecture.md` |
| API reference overview | Done | `docs/messaging/api-reference.md` |
| REST API reference | Done | `docs/messaging/api-reference-rest.md` |
| gRPC API reference | Done | `docs/messaging/api-reference-grpc.md` |
| QUIC API reference | Done | `docs/messaging/api-reference-quic.md` |
| MCP API reference | Done | `docs/messaging/api-reference-mcp.md` |
| Unix API reference | Done | `docs/messaging/api-reference-unix.md` |
| Delta publishing guide | Done | `docs/messaging/delta-publishing.md` |
| CLI reference | Done | `docs/messaging/cli-reference.md` |
| ROADMAP | Done | This document |
| WebSocket architecture | Done | `docs/websocket/architecture.md` |
| WebSocket subprotocols | Done | `docs/websocket/subprotocols.md` |
| Go SDK documentation | Done | `docs/messaging/sdk-go.md` |
| JavaScript SDK documentation | Done | `docs/messaging/sdk-javascript.md` |

**Documentation Structure:**
```
docs/messaging/
    architecture.md
    api-reference.md
    api-reference-rest.md
    api-reference-grpc.md
    api-reference-quic.md
    api-reference-mcp.md
    api-reference-unix.md
    delta-publishing.md
    sdk-go.md
    sdk-javascript.md
    cli-reference.md
    ROADMAP.md
docs/websocket/
    architecture.md
    subprotocols.md
```

---

## Timeline

| Phase | Target | Dependencies | Status |
|-------|--------|--------------|--------|
| Phase 1 | Complete | None | COMPLETE |
| Phase 1.5 | Complete | Phase 1 | COMPLETE |
| Phase 2 | Complete | Phase 1 | COMPLETE |
| Phase 2.5 | Complete | Phase 2 | COMPLETE |
| Phase 3 | Complete | Phase 2 | COMPLETE |
| Phase 4 | Complete | Phase 1.5 | COMPLETE |
| Phase 4.5 | Complete | Phase 1 | COMPLETE |
| Phase 5 | Complete | Phase 2 | COMPLETE |
| Phase 5.5 | Complete | Phase 5 | COMPLETE |
| Phase 6 | Complete | Phase 5 | COMPLETE |
| Phase 7 | Complete | Phase 5 | COMPLETE |
| Phase 8 | Complete | Phase 5, 6, 7 | COMPLETE |
| Phase 9 | Complete | All phases | COMPLETE |

## Related Documentation

- Architecture - System design and component overview
- API Reference - Core operations and message types
- REST API Reference - REST/SSE endpoint documentation
- gRPC API Reference - gRPC service documentation
- QUIC API Reference - QUIC opcode documentation
- MCP API Reference - MCP tool documentation
- Unix API Reference - Unix socket documentation
- Delta Publishing - Delta computation and wire format
- Go SDK - Go SDK installation and usage
- JavaScript SDK - JavaScript SDK installation and usage
- CLI Reference - CLI command reference
- [WebSocket Architecture](../websocket/architecture.md) - WebSocket server design
- [WebSocket Subprotocols](../websocket/engineering.md) - MQTT, AMQP, STOMP support

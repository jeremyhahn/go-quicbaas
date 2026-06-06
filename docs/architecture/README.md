# QuicBaaS Architecture

QuicBaaS is a high-performance, distributed key-value database with native vector search capabilities, built on multi-Raft consensus and PebbleDB storage. It provides strong consistency, horizontal scalability, field-level encryption, and multi-protocol API access.

## Core Architecture

```
+-------------------------------------------------------------------------+
|                          Client Layer                                    |
|     gRPC Client | REST Client | QUIC Client | MCP Client | CLI          |
+--------------------------------------+--------------------------------------+
                                       |
+--------------------------------------+--------------------------------------+
|                       API Protocol Layer                                 |
|     gRPC Server | REST Server | QUIC Server | MCP Server | CLI          |
+--------------------------------------+--------------------------------------+
                                       |
+--------------------------------------+--------------------------------------+
|                        Service Facade Layer                              |
|   KVService | VectorService | TransactionService | ClusterService       |
+--------------------------------------+--------------------------------------+
                                       |
+--------------------------------------+--------------------------------------+
|                      Business Logic Layer                                |
|                                                                          |
|  +-------------------+  +--------------------+  +--------------------+   |
|  |   Router          |  |  Multi-Raft        |  |  State Machines    |   |
|  |  - Sharding       |->|  - Shard 1         |->|  - KV FSM          |   |
|  |  - Load Balance   |  |  - Shard 2         |  |  - Vector FSM      |   |
|  |  - Consistency    |  |  - Shard N         |  |  - TS FSM          |   |
|  +-------------------+  +--------------------+  +--------------------+   |
|                                                        |                 |
|                                                        v                 |
|  +-------------------------------------------------------------------+   |
|  |              Raft Consensus (QuicRaft)                          |   |
|  |  - Leader Election  - Log Replication  - Snapshots                |   |
|  +-------------------------------------------------------------------+   |
+--------------------------------------+--------------------------------------+
                                       |
+--------------------------------------+--------------------------------------+
|                         Storage Layer                                    |
|                                                                          |
|  +------------------+  +------------------+  +------------------+         |
|  |   PebbleDB       |  |  HNSW Index      |  |  Index Manager   |         |
|  |  - LSM-tree      |  |  - Vector ANN    |  |  - Secondary     |         |
|  |  - WAL           |  |  - Namespaces    |  |  - Metadata      |         |
|  |  - Snapshots     |  |  - Persistence   |  |  - Time-series   |         |
|  +------------------+  +------------------+  +------------------+         |
+--------------------------------------------------------------------------+
```

## Core Components

### API Layer

Multiple protocol support for maximum flexibility:

- **gRPC** - High-performance binary protocol (150K+ ops/sec, <5ms P99)
- **REST** - HTTP/JSON for web clients (25K+ ops/sec, <15ms P99)
- **QUIC** - Low-latency UDP protocol (100K+ ops/sec, <3ms P99)
- **MCP** - Model Context Protocol for AI agents
- **CLI** - Command-line interface with tab completion

All protocols share business logic through a service facade pattern, ensuring consistent behavior and code reuse. See [api.md](api.md)

### Service Facade

Abstracts business logic from protocol details, providing a single point of access for all database operations:

- Request routing and shard selection
- Consistency level enforcement
- Transaction coordination
- Cluster state management
- Security and authorization
- Metrics collection and logging

See [service-facade.md](service-facade.md)

### Consensus Layer

Multi-Raft architecture using QuicRaft for horizontal scaling:

- Multiple independent Raft groups per cluster
- Deterministic key-to-shard routing
- Strong consistency with linearizable reads
- Fault tolerance (survives (n-1)/2 failures)
- Automatic leader election and failover
- Configurable write modes (sync, WAL, batched)

See [raft-consensus.md](raft-consensus.md), [write-modes.md](write-modes.md)

### State Machines

Generic, type-safe state machines implementing business logic:

- **KV State Machine** - Key-value operations with indexing
- **Vector State Machine** - Vector search with HNSW indexing
- **Time-Series State Machine** - Temporal data management
- ACID guarantees via Raft consensus
- Snapshot support for recovery
- Concurrent and on-disk variants

See [state-machines.md](state-machines.md)

### Storage Layer

Embedded LSM-tree storage with PebbleDB:

- High write throughput with WAL
- Efficient range scans
- Block cache and bloom filters
- Compression (Snappy/Zstd)
- Secondary indexes for fast lookups

See [storage-engine.md](storage-engine.md)

### Vector Database

Native vector similarity search for AI/ML workloads:

- HNSW algorithm for approximate nearest neighbor search
- Multiple distance metrics (L2, Cosine, Dot Product)
- Namespace isolation for multi-tenancy
- Hybrid search with metadata filtering
- RAG workflow optimization

See [vector.md](vector.md)

### Security

Defense-in-depth security architecture:

- TLS 1.3 for data in transit
- Field-level encryption for data at rest
- Integrated Key Management System (KMS)
- FIPS 140-3 compliance mode
- API key and mTLS authentication
- Audit logging

See [security.md](security.md)

## Key Design Decisions

1. **Multi-Raft Design** - Horizontal scalability through independent Raft groups, avoiding single-leader bottleneck
2. **Generic State Machines** - Type-safe, reusable state machines using Go generics
3. **Service Facade Pattern** - Protocol-independent business logic for consistent behavior across APIs
4. **Dual-Codec System** - MessagePack for storage, flexible wire formats for APIs (3x faster, 62% fewer allocations)
5. **Embedded Storage** - PebbleDB for pure-Go, production-grade LSM-tree storage
6. **HNSW Vector Index** - Approximate nearest neighbor search with 95%+ recall and <10ms P99 latency
7. **Field-Level Encryption** - Granular data protection with automatic key rotation

## Performance Characteristics

| Operation | Throughput (ops/sec) | Latency (P99) | Notes |
|-----------|---------------------|---------------|-------|
| KV Put (gRPC) | 150,000+ | <5ms | Single shard, batched mode |
| KV Get (local read) | 200,000+ | <1ms | Stale allowed |
| KV Get (quorum read) | 100,000+ | <5ms | Linearizable |
| Vector Search (k=10) | 5,000+ | <50ms | 10M vectors, HNSW |
| Batch Operations | 10x single | - | Amortized overhead |

## Scalability

- **Nodes**: Linear scaling up to 10+ nodes
- **Shards**: 1-100+ Raft groups per cluster
- **Vectors**: 1B+ vectors across shards
- **Data Size**: Petabyte-scale with proper sharding

## Technology Stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Consensus | [QuicRaft](https://github.com/jeremyhahn/go-quicraft) | Multi-group Raft |
| Storage | [PebbleDB](https://github.com/cockroachdb/pebble) | LSM-tree database |
| RPC | [gRPC](https://grpc.io/) | High-performance RPC |
| CLI | [Cobra](https://github.com/spf13/cobra) | Command framework |
| Config | [Viper](https://github.com/spf13/viper) | Configuration |
| Logging | [log/slog](https://pkg.go.dev/log/slog) | Structured logging (stdlib) |
| Metrics | [Prometheus](https://prometheus.io/) | Monitoring |

## Documentation Index

### Core Architecture
- [API Layer](api.md) - Multi-protocol API design
- [Service Facade](service-facade.md) - Business logic abstraction
- [Raft Consensus](raft-consensus.md) - Multi-Raft implementation
- [State Machines](state-machines.md) - Business logic layer
- [Storage Engine](storage-engine.md) - PebbleDB integration
- [Write Modes](write-modes.md) - Durability guarantees and performance tradeoffs
- [Composition](composition.md) - How quicbaasd composes qrdb + xkms via `With*` injection
- [Embedded Mode](embedded-mode.md) - In-process qrdb routing (QRDBEmbeddedServicers + WithEmbeddedQRDB)
- [Barrier](barrier.md) - StorageBarrier design (Software + Shamir + None)

### Features
- [Vector Database](vector.md) - Vector search architecture
- [Security](security.md) - Encryption and authentication
- [Multi-Tenant Architecture](multi-tenant.md) - Tenant isolation and resource management
- [Multi-Tenant Service Isolation](../security/multi-tenancy.md) - Security policies for tenant isolation
- [Transactions](transactions.md) - ACID transaction support
- [Batch Operations](batch-operations.md) - Bulk operation optimization
- [Indexing](indexing.md) - Secondary index design

### Implementation Details
- [Binary Protocol](binary-protocol.md) - Wire protocol optimization
- [Implementation Details](implementation-details.md) - Internal design notes

## References

- [Raft Consensus Paper](https://raft.github.io/raft.pdf) - Consensus algorithm
- [LSM-tree Paper](https://www.cs.umb.edu/~poneil/lsmtree.pdf) - Storage structure
- [HNSW Paper](https://arxiv.org/abs/1603.09320) - Vector indexing
- [QuicRaft Documentation](https://github.com/jeremyhahn/go-quicraft)
- [PebbleDB Documentation](https://github.com/cockroachdb/pebble)

---

**Last Updated:** 2026-02-03

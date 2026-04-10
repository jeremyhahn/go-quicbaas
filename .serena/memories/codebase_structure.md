# Codebase Structure

## Top-Level Directories
```
cmd/quicbaas/       — CLI client binary
cmd/quicbaasd/      — Server daemon binary
pkg/                — All library code (see below)
sdk/go/             — Go SDK for clients
sdk/javascript/     — JavaScript SDK for clients
api/proto/          — Protobuf definitions
test/integration/   — Docker-based integration tests
test/performance/   — Performance tests
test/benchmark/     — Benchmark tests
configs/            — Example configuration files
deployments/        — Deployment manifests (K8s, Docker)
docs/               — Documentation
scripts/            — Helper scripts
examples/           — Usage examples
```

## pkg/ Package Map

### Core
- `pkg/server/` — Server lifecycle, startup orchestration
- `pkg/service/` — QuicBaaS service facade (business logic layer)
- `pkg/config/` — Configuration structs, defaults, validation
- `pkg/errors/` — Typed error definitions
- `pkg/api/` — Transport layer errors

### Transport/API
- `pkg/api/grpc/` — gRPC server + interceptors
- `pkg/api/rest/` — REST server (chi) + middleware
- `pkg/api/quic/` — QUIC protocol server
- `pkg/api/mcp/` — Model Context Protocol server
- `pkg/api/websocket/` — WebSocket server + subprotocols
- `pkg/api/unix/` — Unix socket transport

### Raft Consensus
- `pkg/raft/node/` — RaftNode wrapper over go-quicraft Host
- `pkg/raft/statemachine/` — FastStateMachine (sharded cache + PebbleDB)
- `pkg/raft/batch/` — Batch aggregator for write throughput
- `pkg/raft/writemode/` — Write durability modes (Sync, WALDurable, Batched)
- `pkg/raft/config/` — Raft configuration + validation
- `pkg/raft/bootstrap/` — Cluster bootstrap (discovery-driven, not yet integrated)
- `pkg/raft/coordinator/` — 2PC coordinator
- `pkg/raft/groupcommit/` — Group commit optimization
- `pkg/raft/shards/` — Shard ID registry + allocation

### Storage
- `pkg/storage/` — Storage backends
- `pkg/kvstore/` — KV store abstraction

### Security
- `pkg/seal/` — Barrier seal/unseal with Shamir + TPM
- `pkg/kms/` — Key Management Service
- `pkg/secrets/` — Secrets engine
- `pkg/auth/` — Authentication (JWT, password)
- `pkg/iam/` — Identity & Access Management (RBAC/ABAC)
- `pkg/crypto/` — Cryptographic utilities
- `pkg/certstore/` — Certificate store
- `pkg/security/` — Security utilities
- `pkg/fips/` — FIPS 140-3 compliance

### Features
- `pkg/vector/` — Vector database (HNSW index)
- `pkg/rag/` — Retrieval Augmented Generation
- `pkg/scheduler/` — Job scheduler
- `pkg/backup/` — Backup/restore
- `pkg/messaging/` — Pub/sub messaging
- `pkg/watch/` — Key watching
- `pkg/cache/` — Caching layer
- `pkg/catalog/` — Data catalog
- `pkg/embedding/` — Embedding service
- `pkg/ollama/` — Ollama integration

### Utilities
- `pkg/logger/` — Structured logging
- `pkg/metrics/` — Prometheus metrics
- `pkg/tracing/` — Distributed tracing
- `pkg/consistency/` — Consistency levels
- `pkg/routing/` — Shard routing
- `pkg/codec/` — Multi-format encoding
- `pkg/dao/` — Generic DAO pattern
- `pkg/models/` — Data models
- `pkg/lease/` — Distributed leasing
- `pkg/version/` — Version info

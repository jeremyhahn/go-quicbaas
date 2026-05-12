# QuicBaaS Documentation

High-performance distributed key-value database with vector similarity search built on multi-Raft consensus and PebbleDB storage.

## Quick Start

```bash
# Start server (REST :18080, gRPC :19090, MCP :11443, QUIC :10443, Unix socket)
quicbaasd start --http-port 18080 --grpc-port 19090 \
  --unix-socket /tmp/quicbaas.sock --data-dir ./data

# Basic operations (transport auto-selected from --address scheme)
quicbaas --address https://localhost:18080 data put users user1 '{"name": "Alice"}'
quicbaas --address https://localhost:18080 data get users user1

# Vector search
quicbaas vector namespace create embeddings --dimension 384
quicbaas vector upsert embeddings --id doc1 --vector "0.1,0.2,..." --metadata '{"title":"Doc"}'
quicbaas vector search similar embeddings --vector "0.1,0.2,..." --k 10
```

### Deploy a fleet agent and run inference

```bash
# 1. Stage an SSH key in the xkms-backed key store (private key never leaves the server).
quicbaas ssh keys generate --name fleet-deploy --type ed25519 --scope user
quicbaas ssh keys export-public --name fleet-deploy --scope user --owner $USER
# → copy the printed public key into ~/.ssh/authorized_keys on the target host.

# 2. Deploy an agent that exposes the local Ollama instance to the fleet.
quicbaas agent deploy \
  --ssh user@target-host \
  --ssh-key-name fleet-deploy \
  --server https://<server-ip>:18080 \
  --backend ollama \
  --binary bin/quicbaas-static \
  --wait-online 2m

# 3. Run a stateless prompt — routed transparently to the agent's local Ollama.
quicbaas orchestrator inference \
  --backend ollama \
  --model qwen3:1.7b \
  --prompt "explain QUIC in three sentences"

# Streaming variant (NDJSON over the same SDK call):
quicbaas orchestrator inference \
  --backend ollama --model qwen3:1.7b \
  --prompt "explain QUIC" --stream
```

The same flow is available in the **web/desktop GUI** at `Orchestration > Inference`
(see [GUI: Desktop & Web](gui/desktop-and-web.md) and the
[Inference view section](agents/inference.md#using-the-gui)). All five
transports — REST, gRPC, QUIC, MCP, Unix socket — accept the same
`orchestrator inference` command via the SDK transport abstraction
(see [API](api/) for protocol details).

## Documentation Index

| Section | Description |
|---------|-------------|
| [Services](services.md) | Complete list of all 28 services with 204+ operations |
| [Architecture](architecture/) | System design, components, data flow |
| [CLI Reference](cli/) | Complete command-line interface reference |
| [API](api/) | REST, gRPC, QUIC, and MCP protocol documentation |
| [Agents & Fleet Inference](agents/) | Fleet deploy, mTLS bootstrap, sessionless inference |
| [GUI Walkthroughs](gui/README.md) | Interactive walkthroughs for all 22+ web and desktop views |
| [GUI Quick Start](gui/desktop-and-web.md) | Wails desktop app and embedded web UI overview |
| [Deployment](deployment/) | Production deployment guides and checklists |
| [Signal](signal/README.md) | Signal service documentation |

### Core Components

| Component | Description |
|-----------|-------------|
| [Storage](storage/) | PebbleDB storage engine, LSM-tree, key encoding |
| [Raft](raft/) | Multi-raft consensus, replication, leader election |
| [Cache](cache/) | In-memory caching layer, configuration, tuning |
| [Codec](codec/) | Dual-codec system (MessagePack/JSON), serialization |
| [DAO](dao/) | Data Access Object layer, CRUD operations, pagination |

### Features

| Feature | Description |
|---------|-------------|
| [Vector Database](vector/) | Vector similarity search, HNSW indexes |
| [Time Series](timeseries/) | Metrics storage, retention policies, Raft consensus |
| [RAG Pipeline](rag/) | Retrieval-Augmented Generation, hybrid search, context building |
| [Embedding Providers](embedding/) | Ollama, LMStudio, OpenAI, Voyage AI |
| [Passwordless Authentication](auth/passwordless.md) | Hardware-backed FIDO2/WebAuthn, PIV smartcard, mTLS |
| [IAM](iam/) | Identity and access management, ABAC policies, OAuth2/OIDC |
| [Security](security/) | KMS, secrets engine, seal/unseal, encryption |
| [Multi-Tenant Service Isolation](security/multi-tenancy.md) | Tenant isolation, resource boundaries, security policies |
| [Kubernetes](kubernetes/) | Container secrets integration, Helm charts |

### Real-Time & Messaging

| Topic | Description |
|-------|-------------|
| [Messaging](messaging/) | Real-time pub/sub, delta publishing, channels |
| [WebSocket](websocket/) | WebSocket server, MQTT/AMQP/STOMP subprotocols |
| [Scheduler](scheduler/) | Distributed job scheduling, cron expressions |

### Transactions

| Topic | Description |
|-------|-------------|
| [DAO Transactions](transactions/dao_transactions.md) | Entity-aware transactions, read-your-own-writes |
| [Distributed Transactions](transactions/distributed_transactions.md) | 2PC coordinator, cross-shard transactions |
| [Optimistic Locking](transactions/optimistic_locking.md) | Version-based concurrency control |

### Batch Operations

| Topic | Description |
|-------|-------------|
| [Atomic Batch Operations](architecture/batch-operations.md) | Multi-key atomic writes |
| [Conditional Batch](batch/conditional_batch.md) | Precondition-based batch operations |
| [Streaming Batch](batch/streaming_batch.md) | Large dataset streaming with progress tracking |

### Operations

| Topic | Description |
|-------|-------------|
| [Backup & Recovery](operations/) | Multi-backend backup (S3, Azure, GCS, MinIO), restore, scheduling |
| [Cluster](cluster/) | Cluster management, discovery, membership, scaling |
| [KMS](kms/) | Key management service configuration |
| [Secrets](secrets/) | Secrets engine operations |
| [Deployment](deployment/) | Production checklists, performance tuning, HA setup |

## Project Structure

```
docs/
|-- architecture/      # System design and components
|-- api/               # Protocol documentation (REST, gRPC, QUIC, MCP)
|-- batch/             # Batch operations documentation
|-- cache/             # Caching layer
|-- cli/               # CLI command reference
|-- cluster/           # Cluster operations
|-- codec/             # Serialization system
|-- dao/               # Data Access Object layer
|-- deployment/        # Production deployment guides
|-- embedding/         # Embedding providers
|-- iam/               # Identity and access management
|-- kms/               # Key management service
|-- kubernetes/        # Container secrets integration
|-- messaging/         # Real-time messaging and notifications
|-- operations/        # Backup, recovery, monitoring
|-- raft/              # Consensus and replication
|-- rag/               # RAG pipeline and context building
|-- scheduler/         # Distributed job scheduling
|-- secrets/           # Secrets engine
|-- security/          # Encryption and secrets
|-- signal/            # Signal service
|-- storage/           # Storage engine internals
|-- testing/           # Testing guides and patterns
|-- timeseries/        # Time series database
|-- transactions/      # Transaction documentation
|-- vector/            # Vector database
+-- websocket/         # WebSocket server and subprotocols
```

## Key Capabilities

| Capability | Details |
|------------|---------|
| Storage | PebbleDB (LSM-tree), MVCC, compression |
| Consensus | Multi-raft via QuicRaft, linearizable reads |
| APIs | gRPC (150K+ ops/sec), REST, QUIC, MCP |
| Vector Search | HNSW indexes, cosine/euclidean/dot product |
| Transactions | DAO transactions, 2PC distributed transactions |
| Batch Operations | Atomic, conditional, streaming batches |
| Messaging | WebSocket pub/sub, MQTT/AMQP/STOMP, delta publishing |
| Security | AES-256-GCM, Shamir's Secret Sharing, mTLS |
| IAM | ABAC policies, OAuth2/OIDC, WebAuthn/FIDO2 |

## Support

- [GitHub Issues](https://github.com/jeremyhahn/go-quicbaas/issues) - Bug reports and feature requests

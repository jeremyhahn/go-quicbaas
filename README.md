# QuicBaaS

![QuicBaaS Logo](assets/logo.svg)

[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev/)
[![Build Status](https://img.shields.io/badge/build-passing-brightgreen)]()

**QuicBaaS** is a modern multi-model distributed database and Backend-as-a-Service built for the agentic cloud era.

## What Makes QuicBaaS Different

| Capability | QuicBaaS | Traditional DBs | Vector DBs |
|------------|----------|-----------------|------------|
| **Multi-Model** | KV + Vector + Time-series + Objects | Usually single model | Vector only |
| **AI-Native** | RAG pipelines, multi-provider embeddings, MCP tools | External integration required | Limited to search |
| **Agentic Ready** | 520 MCP tools registered, 478 reachable by AI agents | No agent support | Minimal |
| **Security** | IAM/ABAC/PEP, FIPS 140-3, per-tenant encryption | Varies | Basic |
| **BaaS Features** | IAM, KMS, Secrets, Scheduler, Service Discovery | Separate services required | None |
| **Auth** | WebAuthn, OAuth2/OIDC, SSO, MFA | Basic auth | API keys |
| **Multi-Protocol** | gRPC, REST, QUIC, MCP, Unix | Usually 1-2 protocols | REST/gRPC |
| **HA/Ops** | Multi-DC failover, zero-downtime ops | Manual | Limited |

## Key Features

### Core Database
- **Strong Consistency** - Linearizable reads/writes via Raft consensus
- **Multi-Raft Scaling** - Independent Raft groups for horizontal partitioning
- **High Performance** - 15,865 ops/sec with sub-12ms P99 latency
- **Modular Storage** - Pluggable backends: Memory, File, PebbleDB, or Raft-replicated
- **Smart Caching** - Multi-tier L1/L2 cache with sharding, TTL, and tag-based invalidation
- **Watch API** - Real-time change notifications with prefix watching

### AI-Native Capabilities
- **Vector Search** - HNSW/IVF indexing with similarity search, hybrid search, and metadata filtering
- **RAG Pipelines** - Embed -> search -> rerank -> build context with token-aware limits
- **Embedding Providers** - Ollama, OpenAI, LMStudio, Voyage AI with caching
- **MCP Protocol** - 520 registered tools, 478 of them reachable, for AI agent integration (the server and SDK both use mark3labs/mcp-go; no other MCP client is run against it yet)

### Security & Identity
- **IAM System** - Multi-tenant identity with users, groups, roles, policies, and sessions
- **ABAC/PEP Engine** - Sub-5us policy evaluation with distributed PEP cache sync
- **Policy Engine** - Time, network, and attribute conditions with bloom filter caching
- **WebAuthn/FIDO2** - Passwordless authentication with passkeys and hardware keys
- **MFA/TOTP** - Multi-factor authentication with backup codes
- **OAuth2/OIDC** - Authorization server (client credentials, token exchange, introspection, discovery) and federated identity provider; the browser authorization-code flow does not complete yet, because `/oauth2/authorize` does not authenticate the user
- **SSO** - Single Sign-On across applications
- **FIPS 140-3** - Compliant cryptography for regulated environments
- **GDPR Compliance** - Consent management, data export, and right-to-deletion

### Encryption & Key Management
- **KMS** - Multiple backends: AWS, GCP, Azure, HashiCorp Vault, PKCS#11 (HSM)
- **Secrets Engine** - Versioned secrets with soft delete, rotation, and watch API
- **Encryption at Rest** - Transparent barrier encryption with AES-256-GCM and ChaCha20-Poly1305
- **Barrier Strategies** - Passphrase, Shamir M-of-N, TPM2, PKCS#11, AWS KMS, GCP KMS, Azure Key Vault, Vault
- **Enable-Encryption** - Live migration from cleartext to encrypted barriers across all transports
- **Tenant Entitlements** - Quota governance and per-tenant barrier mode restrictions
- **Key Rotation** - Automatic rotation without downtime

### Storage Options
- **Key-Value** - Fast distributed KV with MVCC, transactions, and leases
- **Vector Store** - Multi-tenant namespaces with per-tenant encryption
- **Time-Series** - Metrics storage with aggregations and retention policies
- **Object Storage** - S3/GCS/Azure/MinIO with catalog, tiering, and ACLs
- **Parameter Store** - Configuration management with versioning and tags
- **Distributed Cache** - High-performance caching with cluster-wide invalidation

### High Availability & Operations
- **HA/Failover** - Automatic failover with multi-datacenter support
- **Multi-Protocol API** - gRPC, REST, QUIC, MCP, and Unix socket interfaces
- **Prometheus Metrics** - Comprehensive observability with Grafana dashboards
- **Container Secrets** - Works with Kubernetes, Docker, Podman
- **Backup/Restore** - Full and incremental backups to object storage
- **Node Replacement** - Zero-downtime node replacement and rebalancing

## Modular Build System

QuicBaaS uses Go build tags for modular compilation. Default builds include all features. Exclude features to reduce binary size or attack surface.

### Build Profiles

```bash
# Full-featured build (default)
make build

# Pre-configured profiles
make build-minimal     # Core KV + gRPC only (~60% smaller)
make build-embedded    # Single-node, no clustering
make build-standard    # Cluster mode, no AI features
make build-cluster     # All features, no cloud KMS
make build-enterprise  # All features + cloud KMS
make build-full        # Everything including HSM, quantum

# FIPS 140-3 compliant build
make build-fips
```

### Feature Control

Exclude specific features at build time:

```bash
# IAM is always compiled and cannot be excluded.

# Without time series (~7 commands excluded)
make build WITH_TIMESERIES=0

# Without AI features
make build WITH_VECTOR=0 WITH_RAG=0 WITH_MCP=0

# Without REST API (gRPC only)
make build WITH_REST=0

# Multiple exclusions
make build WITH_TIMESERIES=0 WITH_QUIC=0
```

### Build Tag Reference

| Tag | Default | Commands | Description |
|-----|---------|----------|-------------|
| `raft` | ON | - | Distributed consensus and clustering |
| `timeseries` | ON | 7 | Time series metrics storage |
| `vector` | ON | - | Vector database (HNSW indexing) |
| `rag` | ON | - | RAG pipelines (requires vector) |
| `rest` | ON | - | REST/HTTP API server |
| `quic` | ON | - | QUIC protocol support |
| `mcp` | ON | - | MCP AI agent tools |
| `fips` | OFF | - | FIPS 140-3 cryptography |

IAM/ABAC (~96 commands) is always compiled into every binary and has no build
tag; QuicBaaS is the central identity broker, so a no-IAM artifact is not
supported.

See [docs/multiprotocol-api/engineering.md](docs/multiprotocol-api/engineering.md) for the complete build-tag reference, including KMS backends, codecs and object-storage tags.

## Storage Backend Configuration

QuicBaaS supports multiple storage backends, configurable per use case:

```yaml
storage:
  # Backend options: memory, file, pebble, raft
  backend: pebble

  # PebbleDB settings (production recommended)
  pebble:
    data_dir: /var/lib/quicbaas
    cache_size: 512MB
    wal_dir: /var/lib/quicbaas/wal

  # Memory backend (testing, ephemeral data)
  memory:
    max_size: 1GB

  # File backend (simple persistence)
  file:
    data_dir: /var/lib/quicbaas
    sync_writes: true
```

| Backend | Use Case | Persistence | Performance |
|---------|----------|-------------|-------------|
| `memory` | Testing, caches | No | Fastest |
| `file` | Simple deployments | Yes | Good |
| `pebble` | Production | Yes | Best |
| `raft` | Distributed | Yes + Replicated | Consistent |

## Quick Start

### Build

```bash
git clone https://github.com/jeremyhahn/go-quicbaas.git
cd go-quicbaas
make deps && make build
```

### Run Standalone Server

```bash
./bin/quicbaas start \
  --grpc-port 50051 \
  --http-port 8080 \
  --data-dir ./data
```

### Run 3-Node Cluster

```bash
make dev-start          # Start cluster
docker compose ps       # Check status
make dev-down           # Stop cluster
```

## Usage Examples

### Key-Value Operations

```go
import quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"

client, _ := quicbaas.New(
    quicbaas.WithAddress("localhost:50051"),
    quicbaas.WithTransport(quicbaas.TransportGRPC),
)
defer client.Close()

// Basic operations
client.KV().Put(ctx, "user:1", []byte(`{"name":"Alice"}`))
value, _ := client.KV().Get(ctx, "user:1")

// Transactions
tx, _ := client.Transaction().Begin(ctx)
tx.Put("account:1", []byte(`{"balance":1000}`))
tx.Put("account:2", []byte(`{"balance":500}`))
tx.Commit()

// Watch for changes
watcher, _ := client.KV().WatchPrefix(ctx, "user:")
for event := range watcher.Events() {
    fmt.Printf("Key %s changed: %s\n", event.Key, event.Type)
}
```

### IAM & Access Control

`Identity().IAM()` is the single IAM entry point: tenants, users, groups,
roles, ABAC policies, the policy enforcement point, OAuth2 apps, sessions,
audit, GDPR, delegated admin and invite-based activation all hang off it.
A user record here IS the go-qrdb user record — QuicBaaS adds OIDC on top of
it and keeps no second user table and no second identifier space.

```go
import "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"

iamc := client.Identity().IAM()

// Create a tenant
tenant, err := iamc.Tenant().Create(ctx, &iam.CreateTenantRequest{
    Name:          "acme-corp",
    DisplayName:   "Acme Corporation",
    Description:   "Example tenant",
    IsolationType: iam.TenantIsolationBarrier,
})

// Create a user. Accounts are created `pending` and activate by enrolling a
// hardware authenticator (WebAuthn or PIV) — there is no password.
user, err := iamc.User().Create(ctx, tenant.ID, &iam.CreateUserRequest{
    Username:    "alice",
    Email:       "alice@acme.com",
    DisplayName: "Alice",
    AuthMethods: []string{"webauthn"},
    Roles:       []string{"developer"},
})

// Create an ABAC policy
policy, err := iamc.Policy().Create(ctx, tenant.ID, &iam.CreatePolicyRequest{
    Name:        "dev-read-staging",
    Description: "Developers can read staging resources",
    Enabled:     true,
    Effect:      iam.EffectAllow,
    Targets: []iam.Target{{
        ResourceTypes: []string{"staging"},
        Actions:       []string{"read", "list"},
    }},
})

// Check authorization through the policy enforcement point
decision, err := iamc.PEP().Check(ctx, &iam.CheckRequest{
    TenantID: tenant.ID,
    Subject: iam.CheckSubject{
        ID:       user.ID,
        TenantID: tenant.ID,
        Type:     iam.SubjectTypeUser,
        Roles:    []string{"developer"},
    },
    Action:   "read",
    Resource: "staging://config/database",
})
fmt.Printf("Allowed: %v, Reason: %s\n", decision.Allowed, decision.Reason)
```

### Vector Search & RAG

```go
// Store vectors with metadata
client.Vector().Upsert(ctx, "knowledge-base", "doc-1", embedding, map[string]string{
    "title":    "QuicBaaS Architecture",
    "category": "documentation",
})

// Similarity search
results, _ := client.Vector().Search(ctx, "knowledge-base", queryVector, 10)

// Hybrid search (vector + metadata filter)
results, _ := client.Vector().HybridSearch(ctx, &vector.HybridSearchRequest{
    Namespace: "knowledge-base",
    Vector:    queryVector,
    Filters:   map[string]string{"category": "documentation"},
    Limit:     10,
})

// RAG pipeline (embed -> search -> build context)
ragResult, _ := client.RAG().Query(ctx, &rag.QueryRequest{
    Query:      "How does Raft consensus work?",
    Namespace:  "knowledge-base",
    MaxTokens:  4000,
    Model:      "nomic-embed-text",
})
fmt.Println(ragResult.Context) // Ready for LLM prompt
```

### Secrets & KMS

```go
// Store secrets with versioning
client.Secrets().Put(ctx, "apps/myapp/db-password", map[string][]byte{
    "username": []byte("admin"),
    "password": []byte("secret123"),
})

// Get current version
secret, _ := client.Secrets().Get(ctx, "apps/myapp/db-password")

// Get specific version
secret, _ := client.Secrets().GetVersion(ctx, "apps/myapp/db-password", 2)

// KMS encryption
ciphertext, _ := client.KMS().Encrypt(ctx, "my-key", plaintext)
plaintext, _ := client.KMS().Decrypt(ctx, "my-key", ciphertext)

// Key rotation
client.KMS().RotateKey(ctx, "my-key")
```

### Caching

```go
// Cache with TTL
client.Cache().Set(ctx, "session:abc123", sessionData, 30*time.Minute)
data, _ := client.Cache().Get(ctx, "session:abc123")

// Cache with tags for bulk invalidation
client.Cache().SetWithTags(ctx, "user:1:profile", profileData, []string{"user:1"}, 1*time.Hour)
client.Cache().SetWithTags(ctx, "user:1:settings", settingsData, []string{"user:1"}, 1*time.Hour)

// Invalidate all caches for a user
client.Cache().InvalidateByTag(ctx, "user:1")
```

### REST API

```bash
# Key-Value
curl -X PUT http://localhost:8080/api/v1/kv/user:1 \
  -H "Content-Type: application/json" \
  -d '{"value":"eyJuYW1lIjoiQWxpY2UifQ=="}'

# IAM - Create user
curl -X POST http://localhost:8080/api/v1/iam/tenants/acme/users \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"username":"bob","email":"bob@acme.com","roles":["viewer"]}'

# IAM - Check authorization
curl -X POST http://localhost:8080/api/v1/iam/check \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"tenant_id":"acme","subject":"user:bob","action":"read","resource":"docs:readme"}'

# Vector Search
curl -X POST http://localhost:8080/api/v1/vector/search \
  -H "Content-Type: application/json" \
  -d '{"namespace":"docs","vector":[0.1,0.2,...],"limit":10}'

# RAG Query
curl -X POST http://localhost:8080/api/v1/rag/query \
  -H "Content-Type: application/json" \
  -d '{"query":"What is QuicBaaS?","namespace":"docs","max_tokens":4000}'
```

### MCP Tools (AI Agents)

QuicBaaS registers **520 MCP tools** (counted at the
registration site, `pkg/api/mcp/tools.go`'s `registerTools`, on the default
build; `WITH_RAG=0` drops the five RAG tools, leaving 515). Forty-two IAM tools
(federation, GDPR, policy, identity linking, MFA and login) are declared
deliberately unmapped in `pkg/api/mcp/authz_qrdb_unmapped.go` and are therefore
filtered out of `tools/list` under the default-deny authorization gate, leaving
**478 reachable tools** for AI agent integration:

| Category | Tools | Description |
|----------|-------|-------------|
| **IAM** | 112 | Tenants, users, groups, roles, policies, sessions, MFA, GDPR, federation |
| **Orchestration** | 47 | Agent sessions, jobs, tasks, fleet, pooled accounts, projects |
| **go-xkms compat** | 47 | Keys, certs, CA, PIV, FIDO2, PIN |
| **KMS + DEK** | 39 | Key generation, rotation, DEK management, cluster DEK sync |
| **Object Storage** | 34 | Objects, admin, transfers, policies |
| **Vector** | 24 | Upsert, search, namespaces, index rebuild |
| **Secrets** | 21 | Versioned secrets, rotation, dynamic secrets |
| **Barrier** | 20 | System and per-tenant barrier init, seal, unseal |
| **SSH** | 14 | Key and certificate operations |
| **Crypto** | 13 | Encrypt, decrypt, sign, verify, HMAC, hash |
| **KV + batch** | 11 | CRUD, range, batch, transactions |
| **Cluster ops** | 11 | Nodes, shards, rebalance, migration |
| **Scheduler** | 11 | Jobs CRUD, trigger, pause/resume, history |
| **Messaging** | 10 | Publish, subscribe, channel management |
| **Ceremony** | 9 | Shamir M-of-N initialization ceremony |
| **Certificates** | 9 | Issue, sign, revoke, verify, extensions |
| **Credentials** | 8 | Versioned named-credential rotation |
| **Parameters** | 8 | Config CRUD, batch, search |
| **Seal/Unseal** | 8 | Initialization, seal status, rekey |
| **Embedding** | 7 | Generation, batch ops, provider management |
| **Time-Series** | 7 | Write, query, retention, metrics |
| **RAG** | 5 | Query, context building, indexing |
| **Ollama** | 5 | Embedding and model operations |
| **Backup** | 5 | Create, restore, verify, list, delete |
| **Cache** | 4 | Stats, clear, configuration |
| **Attestation** | 3 | Verify, verify chain, attest container |
| **Trust** | 3 | DANE publication, cross-signed pools, AIA import |
| **Health** | 3 | Liveness, readiness, health check |
| **Tenant admin** | 2 | Quota and entitlement updates |

```json
// Key-Value operations
{"tool": "qrdb_put", "arguments": {"entityType": "user", "id": 1, "data": "base64data"}}
{"tool": "qrdb_get", "arguments": {"entityType": "user", "id": 1}}
{"tool": "quicbaas_batch_put", "arguments": {"operations": [{"entityType": "user", "id": 1, "data": "base64data"}]}}

// Vector search
{"tool": "qrdb_vector_upsert", "arguments": {"namespace": "docs", "id": "doc1", "vector": "[0.1, 0.2]"}}
{"tool": "quicbaas_vector_search", "arguments": {"namespace": "docs", "vector": [0.1, 0.2], "k": 10}}

// RAG pipeline
{"tool": "quicbaas_rag_query", "arguments": {"query": "How does Raft work?", "namespace": "docs", "top_k": 10}}

// KMS operations
{"tool": "quicbaas_kms_generate_key", "arguments": {"name": "my-key", "algorithm": "AES-256-GCM"}}
{"tool": "quicbaas_crypto_encrypt", "arguments": {"key_id": "my-key", "plaintext": "base64data"}}

// Secrets management
{"tool": "quicbaas_secrets_store", "arguments": {"name": "myapp-db", "data": "base64data"}}
{"tool": "quicbaas_secrets_get", "arguments": {"secret_id": "sec-123"}}

// IAM operations
{"tool": "quicbaas_iam_user_create", "arguments": {"tenant_id": "acme", "email": "alice@acme.com", "name": "Alice"}}
{"tool": "quicbaas_iam_admin_check_permission", "arguments": {"user_id": "usr-1", "permission": "user:create"}}

// Scheduler
{"tool": "quicbaas_scheduler_create_job", "arguments": {"name": "backup", "type": "recurring", "schedule": "0 2 * * *", "handler": "backup"}}
```

## Performance

| Metric | Value |
|--------|-------|
| **Throughput** | 15,865 ops/sec |
| **P50 Latency** | 2.52 ms |
| **P99 Latency** | 11.64 ms |
| **ABAC Evaluation** | < 5 microseconds |
| **Cache Hit** | < 100 nanoseconds |
| **Vector Search** | brute-force scan — `auto` and `hnsw` both resolve to in-memory linear search today ([details](docs/vector/engineering.md)) |

## Documentation

Every feature has a folder under [`docs/`](docs/) holding five documents —
architecture, engineering, CLI, API and user guide. Start at
**[docs/README.md](docs/README.md)**, which is the index and the migration
record.

### Start here
- [Documentation index](docs/README.md) — all 38 feature folders
- [Multiprotocol API](docs/multiprotocol-api/) — the transport model: how any capability reaches REST, gRPC, QUIC, MCP, Unix or embedded
- [Go SDK](docs/go-sdk/) — the canonical entry point
- [Composition](docs/composition/) — how the stack is assembled at startup

### Core
- [Data services](docs/data-services/) — KV, DAO, transactions, batch, sharding, storage engine
- [Cache](docs/cache/) · [Codec](docs/codec/) · [Cluster](docs/cluster/) · [Backup & restore](docs/backup-restore/)

### AI and agents
- [Software Factory](docs/software-factory/) — agent sessions, jobs, workflows,
  projects, pooled provider accounts, fleet, container trust, and the SDLC
  module that turns a labelled forge issue into a merged pull request
- [Vector](docs/vector/) · [RAG](docs/rag/) · [Embeddings](docs/embeddings/) · [Ollama](docs/ollama/)
- [MCP server](docs/mcp-server/) — 520 tools, scope-filtered per principal

### Security and identity
- [IAM](docs/iam/) · [Authorization](docs/authorization/) · [OAuth 2.1 / OIDC](docs/oauth2-oidc/) · [Audit logging](docs/audit-logging/)
- [Barrier](docs/barrier/) · [KMS](docs/kms/) · [Data encryption keys](docs/data-encryption-keys/) · [Secrets](docs/secrets/) · [Credentials](docs/credentials/)
- [Certificate authority](docs/certificate-authority/) · [SSH](docs/ssh/) · [Multi-tenancy](docs/multi-tenancy/) · [Security hardening](docs/security-hardening/)

### Services and surfaces
- [Messaging](docs/messaging/) · [Scheduler](docs/scheduler/) · [Object storage](docs/object-storage/) · [Parameter store](docs/parameter-store/) · [Time series](docs/timeseries/)
- [Desktop GUI](docs/desktop-gui/) · [Observability](docs/observability/) · [Signal bridge](docs/signal-bridge/) · [WebSocket](docs/websocket/)

### Operations
- [Bootstrap](docs/bootstrap/) — first-boot ceremony and trust bootstrap
- [Production checklist](docs/deployment/production-checklist.md) · [Kubernetes](docs/deployment/kubernetes.md)

## Architecture

```
+--------------------------------------------------------------+
|                    Client Layer                              |
|     Go SDK | gRPC | REST | QUIC | MCP (AI Agents) | Unix    |
+--------------------------------------------------------------+
|                    API Layer                                 |
|   Authentication | Authorization (RBAC/ABAC) | Rate Limit   |
+--------------------------------------------------------------+
|                    Service Layer                             |
|  KV | Vector | IAM | KMS | Secrets | Objects | RAG | Cache  |
+--------------------------------------------------------------+
|                    Raft Consensus Layer                      |
|         Multi-Raft Groups | Leader Election | Replication   |
+--------------------------------------------------------------+
|                    Storage Layer                             |
|   Memory | File | PebbleDB | HNSW Indexes | Object Backends |
+--------------------------------------------------------------+
```

## Testing

```bash
make test              # Unit tests
make integration-test  # Integration tests
make perf-test         # Performance benchmarks
make coverage          # Coverage report
```

## Deployment

### Docker

```bash
make docker-build RUN_ID=local

docker run -p 50051:50051 -p 8080:8080 quicbaas:latest-local
```

### Kubernetes

```bash
kubectl apply -f deployments/kubernetes/
helm install quicbaas ./deployments/helm/quicbaas
```

## Contributing

Contributions welcome.

## License

QuicBaaS is dual-licensed:

1. **GNU Affero General Public License v3.0 (AGPL-3.0)** - [LICENSE](LICENSE)
2. **Commercial License** - Contact licensing@automatethethings.com

## Acknowledgments

- [QuicRaft](https://github.com/jeremyhahn/go-quicraft) - Multi-group Raft library with QUIC transport
- [PebbleDB](https://github.com/cockroachdb/pebble) - LSM-tree storage engine
- [HNSW](https://github.com/nmslib/hnswlib) - Approximate nearest neighbor search

---

**QuicBaaS** - The AI-native database for the agentic era.

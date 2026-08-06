# Changelog

All notable changes to QuicBaaS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed — BREAKING: ceremony claim-share is now a Begin/Complete handshake

- **Single-shot `CeremonyClaimShare` deleted** (tracks go-qrdb's regenerated
  qrdbv1 protos + SDK). The officer Shamir-share claim is now a two-step
  challenge-response handshake, mirroring qrdb's officer-share wire contract
  verbatim on every transport:
  - **SDK** (`Security().Ceremony()`, the re-exported qrdb `CeremonyClient`):
    `CeremonyClaimShare(*ClaimShareRequest)` →
    `CeremonyBeginClaimShare(*BeginClaimShareRequest)` (returns
    `nonce_hex` + `expires_at`) then
    `CeremonyCompleteClaimShare(*CompleteClaimShareRequest{Username,
    NonceHex, Signature})` (returns `share_index` + `share_json`, released
    exactly once). The caller signs the nonce with the private key of their
    enrolled officer certificate; the server derives the caller thumbprint
    from the mTLS peer chain — it never travels on the wire.
  - **REST**: `POST /api/v1/init/claim-share` and the qrdb wire mirror
    `POST /v1/ceremony/claim-share` are replaced by
    `/api/v1/init/officer-share/{begin,complete}` and
    `/v1/ceremony/officer-share/{begin,complete}`.
  - **gRPC** (`quicbaas.ceremony.v1.Ceremony`): `CeremonyClaimShare` RPC
    replaced by `CeremonyBeginClaimShare` / `CeremonyCompleteClaimShare`.
  - **Native QUIC**: `SubOpCeremonyClaimShare` (0x05) is now
    `SubOpCeremonyBeginClaimShare` (0x05) plus
    `SubOpCeremonyCompleteClaimShare` (0x09).
  - **qrdb-QUIC bridge**: `ceremony.ClaimShare` replaced by
    `ceremony.BeginClaimShare` / `ceremony.CompleteClaimShare`.
  - **MCP**: tool `qrdb_ceremony_claim_share` replaced by
    `qrdb_ceremony_begin_claim_share` / `qrdb_ceremony_complete_claim_share`
    (signature and released share travel base64-encoded).
  - **CLI**: `quicbaas init claim-share` keeps its flags but now drives the
    full handshake internally, signing the challenge nonce with `--tls-key`.
  - Service facade (`service.CeremonyServicer`), sealed-window allowlists
    (new `ceremony-officer-share-begin/complete` verbs mirroring qrdb's
    canonical 34-verb table), and gRPC PEP mappings
    (`ceremony:claim_share_begin` / `ceremony:claim_share_complete`)
    migrated accordingly. There is no compatibility shim — the stack is
    pre-v1 with exactly one wire format.

### Added

- **Barrier enable-encryption (live on all transports).** System (`POST /api/v1/barrier/enable-encryption`) and per-tenant (`POST /api/v1/tenants/{tenant_id}/barrier/enable-encryption`) routes enable migration from cleartext (`none`) to encrypted barriers across REST, gRPC, QUIC, MCP, Unix, and embedded transports. Driven by the embedded qrdb node (wired as `EncryptionMigrator`), not the qrdb `BarrierClient` transport, providing direct cluster coordination and fail-safe recovery. Supports passphrase, Shamir, TPM2, PKCS#11, AWS KMS, GCP KMS, Azure Key Vault, and Vault strategies. Request fields: `strategy`, `passphrase`, `shamir_threshold`, `shamir_total`, `mechanism`, `availability`.
- **Tenant quota and entitlement governance.** All qrdb `Quotas` fields (MaxDatabases, MaxRaftGroups, MaxReplicasPerGroup, MaxTotalReplicas, MaxCollections, MaxKeys, MaxStorageBytes, MaxDatabaseSizeBytes, MaxUsers, MaxRoles, MaxCertificates, MaxBackups, MaxVectorDims, MaxTSMetrics, MaxDedicatedWALGroups, SystemBarrierQuota) and `Entitlements` fields (AllowedIsolationModes, AllowedBarrierModes, AllowDedicatedRaft, AllowVolumeStorage, AllowSystemManagedBarrier, AllowDedicatedWAL) are settable at tenant create-time via the provisioner and via dedicated update endpoints on REST/gRPC. Quota enforcement blocks operations that exceed limits; entitlement checks reject feature use (e.g., enable-encryption to a disallowed barrier mode) with 403.
- **Expanded tenant isolation modes.** Quicbaas config now allows `keyspace`, `barrier`, `volume`, and `raft` isolation modes (matching qrdb's full set), enabling per-tenant enable-encryption. Raft mode requires clustered quicbaas deployment.
- **Entitlement-denied error mapping.** `ErrSharedBarrierImmutable` / `EntitlementError` → 403 Forbidden; `ErrMigrationInProgress` / `ErrMigrationSourceNotNone` → 409 Conflict; invalid mechanism/availability → 400 Bad Request; `ErrMigrationProviderUnavailable` → 501 Not Implemented. Consistent across all transports.
- **Barrier in embedded mode.** Full barrier surface (status, init, unseal, seal, rekey, recovery, enable-encryption) now wired in embedded mode via the embedded qrdb node, eliminating the prior "NilServiceError" limitation. CLI commands like `quicbaas barrier status --transport embedded` and the init ceremony operate without a network listener.

### Changed — `seal` → `barrier` rename (cross-project)

- **Encryption-at-rest unified with go-qrdb / go-xkms.** The barrier
  subsystem now wraps every Pebble-backed data-plane store via the
  encrypting `vfs.FS` from `go-qrdb/pkg/seal/encfs`; sealed-window
  middleware enforces a canonical allowlist of `/v1/health` +
  `/v1/barrier/*` + `/v1/init/*` + `/v1/bootstrap/*` while the barrier
  is sealed. See `docs/architecture/barrier.md`.
- **REST surface renamed** `/api/v1/seal/*` → `/api/v1/barrier/*` (16
  endpoints) plus 7 new `/api/v1/init/*` and 4 new `/api/v1/bootstrap/*`
  endpoints. Legacy `/seal/*` paths retained for one release with
  `Deprecation:` and `Sunset:` headers. Migration table at
  `docs/migrations/seal-to-barrier.md`.
- **CLI surface split**: `quicbaas seal …` → `quicbaas barrier …` /
  `quicbaas init …` / `quicbaas bootstrap …`, mirroring `xkmsctl`
  verb-for-verb. `quicbaas seal …` retained for one release as a
  deprecation alias that warns to stderr.
- **TypeScript SDK** ships `BarrierClient`, `CeremonyClient`,
  `BootstrapClient` interfaces with REST + stub grpc-web/quic/mcp
  transports. `SealClient` retained as a deprecation alias that
  `console.warn`s on construction.
- **`defaultSealManager` deleted** in favour of the SDK-backed
  `pkg/barrier/manager.go` adapter. `pkg/interfaces/managers.go`
  exposes both `SealManager` (alias) and `BarrierManager` for one
  release.
- **`cmd/quicbaasd/init.go`** added — daemon-side init command mirroring
  `xkmsd init` (`--barrier-strategy`, `--shares`, `--threshold`,
  `--so name:KIND:path`, `--trusted-ca`).
- **Frontend** `BarrierView.svelte`, `CeremonyView.svelte`, and
  `BootstrapView.svelte` replace `SealView.svelte`. Sidebar label
  "Seal" → "Barrier" with "Ceremony"/"Bootstrap" sub-items under
  SECURITY.
- **`make verify-naming`, `make verify-cli-flags`, `make verify-api-shapes`**
  added — CI watchdogs that catch operator/SDK drift across QuicBaaS,
  go-qrdb, and go-xkms.

### Breaking

- **No in-place upgrade from pre-encfs data directories.** A new
  format-version sentinel at `<data-dir>/seal/__sys/barrier_format_version`
  is checked at startup; daemons refuse to start on a non-empty data
  dir without the sentinel. Operators must export and re-import. See
  `docs/migrations/seal-to-barrier.md`.

### Changed

- **Agent identity is now mTLS-based.** The one-shot enrollment-token
  bootstrap was removed. `quicbaas agent deploy` issues an X.509 client
  certificate for each deployed daemon via the server-embedded CA and
  uploads it to `$DATA_DIR/agent.crt`, `$DATA_DIR/agent.key`, and
  `$DATA_DIR/server-ca.crt`. The daemon presents the cert on the TLS
  handshake when calling `POST /api/v1/agents/register`; the server
  returns OAuth2 client credentials the daemon uses for heartbeat and
  event streams thereafter.
- **Renamed `--central` → `--server` everywhere.** The central
  orchestrator terminology on the agent wire protocol, CLI, GUI, and
  SDK DTOs is now uniformly "server". `agent.Config.CentralAddr` is
  now `agent.Config.ServerAddr`, `deploy.Spec.Central` is now
  `deploy.Spec.Server`, and the Svelte deploy dialog input is now
  labelled "Server URL".
- **`agent serve` requires `--agent-cert`, `--agent-key`, `--server-ca`.**
  The `--enrollment-token` and `--device-cert` flags have been removed.

### Removed

- `iam role grant agent-runtime` CLI command.
- `iam enrollment-token issue` CLI command.
- `POST /api/v1/agents/enroll` REST endpoint.
- `POST /api/v1/agents/enrollment-tokens` REST endpoint.
- `iam.AgentEnrollmentService` and `iam.MemoryEnrollmentTokenStore`.
- `sdk/go/client/iam.IssueEnrollmentToken` and `IssueEnrollmentTokenRequest/Response`.
- The `enrollment_token` field on the `EnrollRequest` proto message.

## [1.0.0] - 2026-02-03

### First Public Release

QuicBaaS is a high-performance, multi-faceted, distributed key-value database built on multi-Raft consensus and PebbleDB storage. Designed for speed, strong consistency, horizontal scalability, security, and operational simplicity.

### Core Features

#### Storage & Data
- **Multi-Raft Consensus** - Distributed consensus using QuicRaft with configurable write modes (sync, WAL, batched)
- **PebbleDB Storage Engine** - High-performance LSM-tree storage with MVCC support
- **Key-Value Operations** - Full CRUD with transactions, batch operations, and prefix queries
- **Vector Database** - HNSW and IVF indexing for similarity search
- **Time-Series Database** - Metrics storage with retention policies and downsampling
- **Object Storage** - Multi-backend support (S3, GCS, Azure, MinIO) with tiering

#### Security
- **IAM/ABAC** - Identity and access management with attribute-based access control
- **KMS Integration** - Support for AWS KMS, GCP KMS, Azure Key Vault, HashiCorp Vault, PKCS#11
- **Secrets Engine** - Versioned secrets with rotation and watch API
- **Encryption** - At-rest and in-transit encryption with FIPS 140-3 compliance option
- **Seal/Unseal** - Vault-style initialization with Shamir secret sharing
- **mTLS** - Mutual TLS for all inter-node communication

#### APIs & Protocols
- **gRPC** - High-performance RPC with streaming support
- **REST/HTTP** - RESTful API with SSE for real-time updates
- **QUIC** - Low-latency UDP-based transport
- **WebSocket** - Real-time communication with MQTT, AMQP, STOMP subprotocols
- **MCP** - Model Context Protocol for AI/LLM integration (270+ tools)
- **Unix Socket** - Local IPC for embedded use cases

#### AI & Machine Learning
- **RAG Pipeline** - Retrieval-Augmented Generation with chunking, embedding, and ranking
- **Embedding Providers** - Support for OpenAI, Ollama, and custom providers
- **Vector Search** - Semantic similarity search with configurable distance metrics

#### Messaging & Real-Time
- **Push Notifications** - Channel-based pub/sub with delta publishing
- **Watch System** - Real-time change notifications with delta computation
- **Message Deduplication** - Content-based and ID-based deduplication

#### Operations
- **Distributed Scheduler** - Cron-based job scheduling across the cluster
- **Backup & Recovery** - Point-in-time recovery with multiple storage backends
- **Cache Layer** - L1 in-memory cache with optional L2 Redis
- **Cluster Management** - Dynamic node addition/removal, rebalancing

#### SDKs
- **Go SDK** - Full-featured client with all transports
- **JavaScript SDK** - Browser and Node.js support with 100% test coverage

#### CLI
- **308 Commands** - Comprehensive CLI across 24 categories
- **Multiple Transports** - gRPC, REST, QUIC, Unix socket, embedded

### Build Profiles
- **Minimal** - Core KV + gRPC only
- **Embedded** - Single-node, no clustering
- **Standard** - Cluster mode, no AI features
- **Cluster** - All features, no cloud KMS
- **Enterprise** - All features + cloud KMS
- **Full** - Everything including HSM and quantum-safe crypto

### Documentation
- Comprehensive documentation in `/docs/`
- API references for all protocols
- Production deployment checklist
- Security hardening guides

### Testing
- 88%+ unit test coverage
- Integration test suite with Docker
- Benchmark suite for performance validation

---

## [0.1.0-alpha] - 2025-01-01

### Initial Development Release
- Core key-value storage
- Basic Raft consensus
- gRPC API
- Foundation for future development

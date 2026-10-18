# Multiprotocol API — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/multiprotocol-api/`.

This document provides comprehensive documentation for all QuicBaaS services, their operations, and transport support.

## Table of Contents

1. [Core Data Services](#core-data-services)
   - [KV Service](#kv-service)
   - [Batch Service](#batch-service)
   - [Transaction Service](#transaction-service)
   - [Query Service](#query-service)
2. [Cluster Services](#cluster-services)
   - [Cluster Service](#cluster-service)
   - [Health Service](#health-service)
   - [Operations Service](#operations-service)
   - [Cache Service](#cache-service)
3. [Security Services](#security-services)
   - [Auth Service](#auth-service)
   - [Seal Service](#seal-service)
   - [KMS Service](#kms-service)
   - [Secrets Service](#secrets-service)
   - [Crypto Service](#crypto-service)
   - [RBAC Service](#rbac-service)
   - [DynamicSecrets Service](#dynamicsecrets-service)
   - [Certificate Service](#certificate-service)
   - [ClusterDEK Service](#clusterdek-service)
4. [IAM Services](#iam-services)
   - [IAM Service](#iam-service)
5. [Storage Services](#storage-services)
   - [ObjectStorage Service](#objectstorage-service)
   - [ObjectStorageAdmin Service](#objectstorageadmin-service)
   - [ObjectTransfer Service](#objecttransfer-service)
   - [ParameterStore Service](#parameterstore-service)
6. [Scheduling Services](#scheduling-services)
   - [Scheduler Service](#scheduler-service)
7. [Specialized Data Services](#specialized-data-services)
   - [Vector Service](#vector-service)
   - [TimeSeries Service](#timeseries-service)
   - [Backup Service](#backup-service)
8. [AI/ML Services](#aiml-services)
   - [Embedding Service](#embedding-service)
   - [Ollama Service](#ollama-service)
9. [Transport Support Matrix](#transport-support-matrix)

---

## Core Data Services

### KV Service

Basic key-value store operations with configurable consistency levels.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `Put` | Stores a key-value pair with optional consistency guarantees |
| `Get` | Retrieves a value by key with configurable consistency |
| `Delete` | Removes a key-value pair |
| `List` | Returns all entities of a type with pagination (streaming) |
| `Watch` | Monitors a key or key prefix for changes (streaming) |
| `ListEntityTypes` | Returns all unique entity types stored in the database |

**Consistency Levels:**
- `Eventual` - Allows stale reads (fastest, lowest latency)
- `Linearizable` - Uses ReadIndex (fast, linearizable reads)
- `Strong` - Uses full Raft consensus (slowest, strongest)

---

### Batch Service

Atomic multi-operation requests for bulk operations.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `BatchPut` | Performs multiple Put operations atomically |
| `BatchGet` | Retrieves multiple values in a single request |
| `BatchDelete` | Removes multiple keys atomically |

---

### Transaction Service

Compare-and-swap transactions for conditional operations.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `Transaction` | Performs atomic compare-and-swap transaction with success/failure operations |

**Compare Targets:** Version, Value, Exists

**Compare Operators:** Equal, NotEqual, Greater, Less, GreaterEqual, LessEqual

---

### Query Service

Secondary index queries and scans.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `Query` | Searches for entities using secondary indexes (streaming) |
| `ScanIndex` | Performs a full index scan (streaming) |

---

## Cluster Services

### Cluster Service

Cluster management and topology operations.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `ClusterStatus` | Returns the current state of all cluster nodes |
| `GetLeader` | Returns the current leader for a specific shard |
| `AddNode` | Adds a node to a Raft shard |
| `RemoveNode` | Removes a node from a Raft shard |
| `CreateShard` | Creates a new Raft shard with initial members |
| `RebalanceCluster` | Redistributes data across shards for better balance |
| `GetRebalanceProgress` | Returns the status of an ongoing rebalance operation |

---

### Health Service

Health checks and liveness probes.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `Health` | Returns the health status of the node with optional details |
| `Liveness` | Returns a simple liveness probe (is the service running?) |
| `Readiness` | Returns a readiness probe (is the service ready to serve?) |

---

### Operations Service

Migration, rebalancing, and upgrade operations.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `MigrateData` | Migrates data between shards or nodes |
| `GetMigrationProgress` | Returns the status of an ongoing migration |
| `ReplaceNode` | Replaces a failed node with a new node |
| `UpgradeNode` | Performs a rolling upgrade of a node |
| `GetUpgradeStatus` | Returns the status of an ongoing upgrade |

---

### Cache Service

Cache management and statistics.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `GetCacheStats` | Returns current cache statistics |
| `ClearCache` | Invalidates all or specific cache entries |
| `GetCacheConfig` | Returns current cache configuration |
| `UpdateCacheConfig` | Updates cache configuration |

---

## Security Services

### Auth Service

Authentication and session management.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `Login` | Authenticates a user and returns access/refresh tokens |
| `Logout` | Invalidates the current session and tokens |
| `RefreshToken` | Refreshes an access token using a refresh token |
| `ValidateToken` | Validates an access token and returns user info |

---

### Seal Service

Server seal/unseal operations with Shamir secret sharing.

**Source:** `Security().Barrier()`, the go-qrdb `BarrierClient` re-export; the SDK seal client was removed (#1559)

| Operation | Description |
|-----------|-------------|
| `Status` | Returns the current seal status (sealed, initialized, progress, required keys) |
| `Init` | Initializes the seal with Shamir secret sharing (returns keys and root token) |
| `Unseal` | Provides an unseal key to progress toward unsealing |
| `Seal` | Seals the server, requiring unseal keys to access data |

**SealStatus Fields:**
- `Sealed` - Whether the server is currently sealed
- `Initialized` - Whether the server has been initialized
- `Progress` - Number of unseal keys provided so far
- `Required` - Number of unseal keys required to unseal
- `ClusterName` - Name of the cluster
- `ClusterVersion` - Version of the cluster

---

### KMS Service

Cryptographic key lifecycle management.

**Source:** `pkg/service/kms_service.go`

**Key Operations (14):**

| Operation | Description |
|-----------|-------------|
| `GetSupportedAlgorithms` | Returns the list of supported cryptographic algorithms |
| `GenerateKey` | Creates a new cryptographic key with specified parameters |
| `ImportKey` | Imports an externally generated key into the KMS |
| `GetKey` | Retrieves key material or public key by key ID |
| `ListKeys` | Returns all keys accessible to the requester with pagination |
| `RotateKey` | Creates a new version of an existing key |
| `RevokeKey` | Marks a key version as revoked and unusable |
| `DeleteKey` | Permanently removes a key and all its versions |
| `GetKeyMetadata` | Retrieves metadata for a key without exposing key material |
| `UpdateKeyMetadata` | Updates mutable metadata fields for a key |
| `UpdateKeyStatus` | Enables or disables a key or key version |
| `ListKeyVersions` | Returns all versions of a key |
| `SetKeyPolicy` | Sets the access control policy for a key |
| `GetKeyPolicy` | Retrieves the access control policy for a key |

**DEK Operations (9):**

| Operation | Description |
|-----------|-------------|
| `CreateDEK` | Creates a new data encryption key |
| `GetDEK` | Retrieves a data encryption key |
| `ListDEKs` | Lists all data encryption keys |
| `RotateDEK` | Rotates a data encryption key |
| `DeleteDEK` | Deletes a data encryption key |
| `GetDEKStatus` | Gets the status of a data encryption key |
| `SetDefaultDEK` | Sets the default data encryption key |
| `SetDEKSafety` | Sets AEAD safety limits for a DEK |
| `ClearDEKSafety` | Clears AEAD safety limits for a DEK |

**Backend Operations (2):**

| Operation | Description |
|-----------|-------------|
| `BackendStatus` | Returns health and capability information about the KMS backend |
| `BackendTest` | Performs a seal/unseal test cycle to verify backend functionality |

**Key Export/Import Operations (2):**

| Operation | Description |
|-----------|-------------|
| `ExportKey` | Exports a key wrapped with a wrapping key |
| `UnwrapKey` | Imports a wrapped key into the KMS |

**AEAD Safety Operations (4):**

| Operation | Description |
|-----------|-------------|
| `SetKeySafety` | Configures AEAD safety limits for a key |
| `ClearKeySafety` | Removes custom AEAD safety limits from a key |
| `SafetyCheck` | Returns warnings for keys/DEKs approaching safety limits |
| `SafetyReport` | Returns a comprehensive safety report for all keys/DEKs |

**Re-encryption Operations (2):**

| Operation | Description |
|-----------|-------------|
| `StartReencryption` | Starts background re-encryption |
| `GetReencryptionProgress` | Gets the progress of re-encryption |

---

### Secrets Service

Secret storage and retrieval with versioning and rotation.

**Source:** `pkg/service/secrets_service.go`

| Operation | Description |
|-----------|-------------|
| `StoreSecret` | Stores a secret with optional metadata and encryption |
| `GetSecret` | Retrieves the latest version of a secret |
| `ListSecrets` | Returns all secrets accessible to the authenticated user |
| `DeleteSecret` | Permanently removes a secret and all its versions |
| `GetSecretVersion` | Retrieves a specific version of a secret |
| `ListSecretVersions` | Returns all versions of a secret |
| `RollbackSecret` | Rolls back to a previous version of a secret |
| `RotateSecret` | Manually rotates a secret to a new value |
| `SetRotationPolicy` | Sets or updates the automatic rotation policy |
| `GetRotationPolicy` | Retrieves the current rotation policy for a secret |
| `UpdateSecretStatus` | Enables or disables a secret |
| `GetSecretsByPrefix` | Fetches all secrets matching a path prefix |
| `WatchSecretsByPrefix` | Streams secret changes for a prefix (streaming) |

---

### Crypto Service

Encryption as a Service (similar to HashiCorp Vault transit secrets engine).

**Source:** `pkg/service/crypto_service.go`

**Symmetric Encryption (3):**

| Operation | Description |
|-----------|-------------|
| `Encrypt` | Encrypts plaintext data using a named encryption key |
| `Decrypt` | Decrypts ciphertext that was encrypted with a named key |
| `Rewrap` | Re-encrypts ciphertext with the latest version of the key |

**Batch Operations (2):**

| Operation | Description |
|-----------|-------------|
| `EncryptBatch` | Performs multiple encrypt operations in a single request |
| `DecryptBatch` | Performs multiple decrypt operations in a single request |

**Data Key Generation (2):**

| Operation | Description |
|-----------|-------------|
| `GenerateDataKey` | Generates a new data encryption key for envelope encryption |
| `GenerateWrappedDataKey` | Generates a new encrypted DEK without plaintext |

**Signing and Verification (2):**

| Operation | Description |
|-----------|-------------|
| `Sign` | Creates a digital signature using a named signing key |
| `Verify` | Verifies a digital signature against data |

**HMAC Operations (2):**

| Operation | Description |
|-----------|-------------|
| `GenerateHMAC` | Generates an HMAC of data using a named key |
| `VerifyHMAC` | Verifies an HMAC against data using a named key |

**Utility Operations (2):**

| Operation | Description |
|-----------|-------------|
| `GenerateRandom` | Generates high-quality cryptographically secure random bytes |
| `Hash` | Computes a cryptographic hash of data |

---

### RBAC Service

Role-based access control operations.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `CreateUser` | Creates a new user with initial roles |
| `DeleteUser` | Removes a user from the system |
| `ListUsers` | Returns all users with optional filtering |
| `CreateAuthRole` | Creates a new RBAC role with permissions |
| `DeleteAuthRole` | Removes an RBAC role from the system |
| `ListAuthRoles` | Returns all RBAC roles with optional filtering |
| `AssignRole` | Assigns a role to a user |
| `RevokeRole` | Removes a role from a user |

---

### DynamicSecrets Service

Dynamic credential generation with lease-based TTL management.

**Source:** `pkg/service/dynamic_secrets_service.go`

| Operation | Description |
|-----------|-------------|
| `CreateRole` | Creates a new credential generation role |
| `GetRole` | Retrieves a credential generation role by name |
| `ListRoles` | Returns all credential generation roles |
| `DeleteRole` | Removes a credential generation role |
| `GenerateCredential` | Generates a new dynamic credential based on a role |
| `RenewLease` | Extends the expiration time of a credential lease |
| `RevokeLease` | Revokes a credential lease immediately |
| `ListLeases` | Returns all active leases |

---

### Certificate Service

Certificate operations for PKI management.

**Source:** `pkg/service/certificate_service.go`

| Operation | Description |
|-----------|-------------|
| `IssueCertificate` | Generates a new certificate |
| `SignCSR` | Signs a Certificate Signing Request |
| `RevokeCertificate` | Marks a certificate as revoked |
| `VerifyCertificate` | Checks if a certificate is valid |
| `GetCACertificate` | Returns the CA certificate |
| `GetTLSConfig` | Returns a TLS configuration |
| `IsInitialized` | Returns whether the provider is ready |
| `Provider` | Returns the underlying CertificateProvider |

---

### ClusterDEK Service

Cluster-wide Data Encryption Key management.

**Source:** `pkg/service/cluster_dek_service.go`

**DEK Lifecycle Operations (3):**

| Operation | Description |
|-----------|-------------|
| `InitializeClusterDEK` | Creates and distributes the initial cluster DEK |
| `GetClusterDEKStatus` | Retrieves current cluster DEK status and metadata |
| `RotateClusterDEK` | Generates a new cluster DEK and initiates re-encryption |

**Node Key Management (2):**

| Operation | Description |
|-----------|-------------|
| `SyncNodeDEK` | Synchronizes cluster DEK to a specific node |
| `GetNodeDEKStatus` | Retrieves DEK sync status for a specific node |

**Re-encryption Operations (3):**

| Operation | Description |
|-----------|-------------|
| `StartReencryption` | Manually initiates re-encryption of data with new DEK |
| `GetReencryptionStatus` | Retrieves current re-encryption progress |
| `CancelReencryption` | Cancels ongoing re-encryption process |

**Recovery Operations (1):**

| Operation | Description |
|-----------|-------------|
| `RecoverDEK` | Recovers cluster DEK using recovery keys or backup mechanism |

---

## IAM Services

### IAM Service

Identity and Access Management - a composite service with 11 sub-clients for comprehensive identity, access, and compliance management.

**Source:** `sdk/go/client/iam/client.go`

| Sub-Client | Description |
|------------|-------------|
| `Tenant()` | Multi-tenant management |
| `User()` | User CRUD and roles |
| `Role()` | Permission bundles |
| `Policy()` | ABAC policies |
| `PEP()` | Policy Enforcement Point for authorization checks |
| `App()` | OAuth2 applications |
| `Session()` | Session management |
| `Audit()` | Audit logging |
| `GDPR()` | GDPR compliance operations |
| `Admin()` | Delegated administration |

**Example usage:**

```go
client := iam.NewClient(transport)

// Create a tenant
tenant, err := client.Tenant().Create(ctx, &iam.CreateTenantRequest{...})

// Create a user in the tenant
user, err := client.User().Create(ctx, tenant.ID, &iam.CreateUserRequest{...})

// Check authorization
result, err := client.PEP().Check(ctx, &iam.CheckRequest{...})
```

---

## Storage Services

### ObjectStorage Service

Object storage operations with multi-tenancy support.

**Source:** `pkg/service/objectstorage_service.go`

**Object CRUD (4):**

| Operation | Description |
|-----------|-------------|
| `PutObject` | Stores an object with specified metadata and policies |
| `GetObject` | Retrieves an object by key |
| `DeleteObject` | Removes an object from storage |
| `HeadObject` | Retrieves object metadata without the data |

**Listing Operations (3):**

| Operation | Description |
|-----------|-------------|
| `ListObjects` | Lists objects matching the specified criteria |
| `ListByTag` | Lists objects with a specific tag |
| `ListByTenant` | Lists objects owned by a specific tenant |

**Copy Operations (1):**

| Operation | Description |
|-----------|-------------|
| `CopyObject` | Copies an object to a new location |

**Replication & Migration (2):**

| Operation | Description |
|-----------|-------------|
| `ReplicateObject` | Replicates an object to target backends |
| `MigrateObject` | Migrates an object to a new backend |

**Archive & Restore (2):**

| Operation | Description |
|-----------|-------------|
| `ArchiveObject` | Moves an object to archive storage |
| `RestoreObject` | Restores an archived object to active storage |

**Verification (1):**

| Operation | Description |
|-----------|-------------|
| `VerifyObject` | Verifies object integrity across storage backends |

**Policy Management (2):**

| Operation | Description |
|-----------|-------------|
| `SetObjectPolicy` | Sets policies for an object |
| `GetObjectPolicy` | Retrieves policies for an object |

**Backend Management (2):**

| Operation | Description |
|-----------|-------------|
| `ListBackends` | Returns available storage backends |
| `GetBackendHealth` | Returns health status for a backend |

**Tenant Management (3):**

| Operation | Description |
|-----------|-------------|
| `GetTenantUsage` | Returns storage usage for a tenant |
| `GetTenantQuotas` | Returns quota settings for a tenant |
| `SetTenantQuotas` | Sets or updates quotas for a tenant |

**Batch Operations (3):**

| Operation | Description |
|-----------|-------------|
| `BatchPutObjects` | Stores multiple objects in a single operation |
| `BatchGetObjects` | Retrieves multiple objects in a single operation |
| `BatchDeleteObjects` | Deletes multiple objects in a single operation |

---

### ObjectStorageAdmin Service

Administrative operations for storage management.

**Source:** `pkg/service/objectstorage_admin_service.go`

**Backend Management (4):**

| Operation | Description |
|-----------|-------------|
| `CreateBackend` | Registers a new storage backend |
| `UpdateBackend` | Updates backend configuration |
| `DeleteBackend` | Removes a storage backend |
| `SetBackendStatus` | Enables or disables a backend |

**Statistics (1):**

| Operation | Description |
|-----------|-------------|
| `GetStorageStats` | Returns cluster-wide storage statistics |

**Garbage Collection (2):**

| Operation | Description |
|-----------|-------------|
| `RunGarbageCollection` | Initiates garbage collection on specified backends |
| `GetGarbageCollectionProgress` | Returns GC progress |

**Repair Operations (1):**

| Operation | Description |
|-----------|-------------|
| `RepairObject` | Attempts to repair an object with inconsistent replicas |

**Compaction (1):**

| Operation | Description |
|-----------|-------------|
| `CompactBackend` | Triggers compaction on a backend |

---

### ObjectTransfer Service

Peer-to-peer object data transfer for lazy snapshot recovery.

**Source:** `pkg/service/object_transfer_service.go`

| Operation | Description |
|-----------|-------------|
| `FetchObject` | Retrieves object data from a peer's local object store |
| `StreamFetchObjects` | Retrieves multiple objects in a single operation |

---

### ParameterStore Service

Hierarchical parameter management with encryption and TTL support.

**Source:** `pkg/service/parameter_store_service.go`

| Operation | Description |
|-----------|-------------|
| `GetParameter` | Retrieves a single parameter by path |
| `SetParameter` | Creates or updates a parameter |
| `DeleteParameter` | Removes a parameter by path |
| `GetParameters` | Retrieves multiple parameters by paths (batch get) |
| `SetParameters` | Creates or updates multiple parameters (batch set) |
| `DeleteParameters` | Removes multiple parameters (batch delete) |
| `ListParameters` | Returns parameters matching a path prefix |
| `SearchByTags` | Finds parameters matching tag key-value pairs |

**Performance Targets:**
- Get operations: < 1ms p99
- Set operations: < 5ms p99
- List operations: < 10ms p99 (up to 100 results)
- Batch operations: < 50ms p99 (up to 100 items)

---

## Scheduling Services

### Scheduler Service

Job scheduling and execution with multi-tenant support.

**Source:** `pkg/service/scheduler_service.go`

| Operation | Description |
|-----------|-------------|
| `CreateJob` | Creates a new scheduled job |
| `GetJob` | Retrieves a job by ID |
| `UpdateJob` | Updates an existing job |
| `DeleteJob` | Removes a scheduled job |
| `ListJobs` | Returns jobs matching the query criteria |
| `TriggerJob` | Immediately executes a job |
| `PauseJob` | Pauses a scheduled job |
| `ResumeJob` | Resumes a paused job |
| `CancelJob` | Cancels a scheduled job |
| `GetJobHistory` | Returns execution history for a job |
| `GetStatus` | Returns scheduler status and statistics |

**Job Types:**
- `one-shot` - Executes exactly once at the specified time
- `recurring` - Executes repeatedly according to a schedule
- `delayed` - Executes once after a specified delay

**Job Status:**
- `pending` - Waiting to be executed
- `running` - Currently executing
- `completed` - Finished successfully
- `failed` - Execution failed
- `paused` - Temporarily suspended
- `canceled` - Was canceled

---

## Specialized Data Services

### Vector Service

Vector database operations for similarity search.

**Source:** `pkg/service/vector_service.go`

**Namespace Management (4):**

| Operation | Description |
|-----------|-------------|
| `CreateNamespace` | Creates a new vector namespace with specified dimensions |
| `DeleteNamespace` | Deletes a namespace and all its vectors |
| `ListNamespaces` | Lists all namespaces with optional filtering |
| `GetNamespaceInfo` | Gets detailed information about a namespace |

**Vector CRUD (4):**

| Operation | Description |
|-----------|-------------|
| `UpsertVector` | Inserts or updates a vector with metadata |
| `GetVector` | Retrieves a vector by ID |
| `DeleteVector` | Deletes a vector by ID |
| `BatchUpsert` | Upserts multiple vectors in a single operation |

**Search Operations (3):**

| Operation | Description |
|-----------|-------------|
| `SearchSimilar` | Searches for similar vectors using ANN |
| `SearchByMetadata` | Searches vectors by metadata filters |
| `HybridSearch` | Combines vector similarity with metadata filtering |

**Index Management (5):**

| Operation | Description |
|-----------|-------------|
| `RebuildIndex` | Triggers a rebuild of the HNSW index |
| `GetRebuildProgress` | Gets the progress of an ongoing index rebuild |
| `GetIndexStats` | Gets statistics about the vector index |
| `CompactNamespace` | Triggers compaction of a namespace |
| `GetCompactionProgress` | Gets the progress of an ongoing compaction |

---

### TimeSeries Service

Time-series data storage and aggregation.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `Aggregate` | Performs time-series aggregation (sum, avg, min, max, count) |
| `TimeSeriesQuery` | Retrieves time-series data within a time range (streaming) |
| `WritePoint` | Writes a single data point to time series storage |
| `WriteBatch` | Writes multiple data points atomically |
| `TSQuery` | Performs time range queries with optional aggregation |
| `DeleteSeries` | Deletes a time series or specific data points |
| `GetRetentionPolicy` | Retrieves the retention policy for a metric |
| `SetRetentionPolicy` | Sets the retention policy for a metric |
| `ListMetrics` | Lists all available metrics |

---

### Backup Service

Backup and restore functionality.

**Source:** `pkg/service/service.go`

| Operation | Description |
|-----------|-------------|
| `CreateBackup` | Creates a consistent backup of the database |
| `ListBackups` | Returns available backups with optional filtering |
| `RestoreBackup` | Restores the database from a backup |
| `DeleteBackup` | Removes a backup from storage |
| `VerifyBackup` | Checks backup integrity and validity |

---

## AI/ML Services

### Embedding Service

Semantic embedding generation and management.

**Source:** `pkg/service/embedding_service.go`

| Operation | Description |
|-----------|-------------|
| `GenerateEmbedding` | Generates an embedding vector for a single text |
| `BatchGenerateEmbeddings` | Generates embeddings for multiple texts |
| `ListProviders` | Returns all registered embedding providers |
| `GetProviderInfo` | Returns detailed information about a specific provider |
| `SetDefaultProvider` | Sets the default embedding provider |
| `GetCacheStats` | Returns statistics about the embedding cache |
| `ClearCache` | Invalidates all or specific embedding cache entries |

**Supported Providers:**
- Voyage AI
- OpenAI
- Ollama (local)
- LM Studio (local)

---

### Ollama Service

Ollama API operations for local LLM embeddings.

**Source:** `pkg/service/ollama_service.go`

| Operation | Description |
|-----------|-------------|
| `Embed` | Generates an embedding using the default model |
| `EmbedWithModel` | Generates an embedding using a specific model |
| `EmbedBatch` | Generates embeddings for multiple texts |
| `ListModels` | Returns all available models from Ollama |
| `GetModelInfo` | Returns detailed information about a specific model |
| `Health` | Checks Ollama connectivity and availability |
| `IsAvailable` | Returns true if Ollama is available and responding |
| `DefaultModel` | Returns the configured default embedding model |
| `BaseURL` | Returns the configured Ollama base URL |

---

## Transport Support Matrix

QuicBaaS services are accessible through multiple transport protocols. All 28 services support all transport protocols.

| Service | gRPC | REST | QUIC | Unix | Embedded |
|---------|:----:|:----:|:----:|:----:|:--------:|
| **Core Data Services** |
| KV | Yes | Yes | Yes | Yes | Yes |
| Batch | Yes | Yes | Yes | Yes | Yes |
| Transaction | Yes | Yes | Yes | Yes | Yes |
| Query | Yes | Yes | Yes | Yes | Yes |
| **Cluster Services** |
| Cluster | Yes | Yes | Yes | Yes | Yes |
| Health | Yes | Yes | Yes | Yes | Yes |
| Operations | Yes | Yes | Yes | Yes | Yes |
| Cache | Yes | Yes | Yes | Yes | Yes |
| **Security Services** |
| Auth | Yes | Yes | Yes | Yes | Yes |
| Seal | Yes | Yes | Yes | Yes | Yes |
| KMS | Yes | Yes | Yes | Yes | Yes |
| Secrets | Yes | Yes | Yes | Yes | Yes |
| Crypto | Yes | Yes | Yes | Yes | Yes |
| RBAC | Yes | Yes | Yes | Yes | Yes |
| DynamicSecrets | Yes | Yes | Yes | Yes | Yes |
| Certificate | Yes | Yes | Yes | Yes | Yes |
| ClusterDEK | Yes | Yes | Yes | Yes | Yes |
| **IAM Services** |
| IAM | Yes | Yes | Yes | Yes | Yes |
| **Storage Services** |
| ObjectStorage | Yes | Yes | Yes | Yes | Yes |
| ObjectStorageAdmin | Yes | Yes | Yes | Yes | Yes |
| ObjectTransfer | Yes | Yes | Yes | Yes | Yes |
| ParameterStore | Yes | Yes | Yes | Yes | Yes |
| **Scheduling Services** |
| Scheduler | Yes | Yes | Yes | Yes | Yes |
| **Specialized Data Services** |
| Vector | Yes | Yes | Yes | Yes | Yes |
| TimeSeries | Yes | Yes | Yes | Yes | Yes |
| Backup | Yes | Yes | Yes | Yes | Yes |
| **AI/ML Services** |
| Embedding | Yes | Yes | Yes | Yes | Yes |
| Ollama | Yes | Yes | Yes | Yes | Yes |

### Transport Details

**gRPC** - High-performance binary protocol with streaming support
- Port: Configurable (default 7000)
- Authentication: mTLS, token-based
- Best for: Service-to-service communication

**REST** - HTTP/JSON API for broad compatibility
- Port: Configurable (default 8080)
- Authentication: Bearer token, API key
- Best for: Web applications, debugging

**QUIC** - UDP-based multiplexed protocol
- Port: Configurable (default 9000)
- Authentication: mTLS
- Best for: High-latency networks, mobile clients

**Unix Socket** - Local IPC for same-host communication
- Path: Configurable (default `/var/run/quicbaas.sock`)
- Authentication: File permissions, credentials
- Best for: Sidecar patterns, local agents

**Embedded** - In-process API for direct integration
- No network overhead
- Authentication: Context-based
- Best for: Testing, embedded use cases

### MCP Protocol Support

QuicBaaS also supports the Model Context Protocol (MCP) for AI/LLM integration. The MCP server registers 520 tools on the default build (515 with `WITH_RAG=0`), counted from `pkg/api/mcp/tools.go`'s `registerTools`, enabling AI assistants and language models to interact with QuicBaaS programmatically. Forty-two of them are declared deliberately unmapped in `pkg/api/mcp/authz_qrdb_unmapped.go` and filtered out of `tools/list` by the authorization gate, so 478 are reachable — see the [README's category breakdown](../../README.md#mcp-tools-ai-agents).

**Source:** `pkg/api/mcp/`

**Features:**
- Full coverage of all 28 services
- Structured tool definitions for LLM consumption
- Context-aware operations
- Supports all authentication methods

---

## Source Files Reference

| Category | Source File |
|----------|-------------|
| Main Service Interface | `pkg/service/service.go` |
| SDK Client Interface | `sdk/go/quicbaas.go` |
| Service Types | `sdk/go/service/types.go` |
| QUIC Protocol | `sdk/go/transport/quic/protocol.go` |
| REST Transport | `sdk/go/transport/rest/transport.go` |
| IAM Client | `sdk/go/client/iam/client.go` |
| KMS Service | `pkg/service/kms_service.go` |
| Crypto Service | `pkg/service/crypto_service.go` |
| Secrets Service | `pkg/service/secrets_service.go` |
| Dynamic Secrets | `pkg/service/dynamic_secrets_service.go` |
| Cluster DEK | `pkg/service/cluster_dek_service.go` |
| Certificate Service | `pkg/service/certificate_service.go` |
| Vector Service | `pkg/service/vector_service.go` |
| Embedding Service | `pkg/service/embedding_service.go` |
| Ollama Service | `pkg/service/ollama_service.go` |
| Object Storage | `pkg/service/objectstorage_service.go` |
| Object Storage Admin | `pkg/service/objectstorage_admin_service.go` |
| Object Transfer | `pkg/service/object_transfer_service.go` |
| Parameter Store | `pkg/service/parameter_store_service.go` |
| Scheduler Service | `pkg/service/scheduler_service.go` |
| MCP Server | `pkg/api/mcp/` |

---

## Total Service Count

| Category | Services | Operations |
|----------|:--------:|:----------:|
| Core Data | 4 | 14 |
| Cluster | 4 | 19 |
| Security | 9 | 82 |
| IAM | 1 (11 sub-clients) | varies |
| Storage | 4 | 32 |
| Scheduling | 1 | 11 |
| Specialized Data | 3 | 30 |
| AI/ML | 2 | 16 |
| **Total** | **28** | **204+** |

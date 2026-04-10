# QuicBaaS Raft Migration Audit - Remaining Direct Raft Access Points

## Summary
Comprehensive audit of ALL remaining direct Raft/SyncPropose/SyncRead calls and other direct backend access that bypass the qrdb SDK in the go-quicbaas project.

---

## 1. DIRECT RAFT CALLS - SyncPropose/SyncRead/ProposeWithMode/StaleRead

### 1.1 pkg/service/quicbaas.go - timeSeriesService

**Component:** timeSeriesService (internal to QuicBaaS service)
**Location:** Lines 3656-3820+ (Aggregate method), Lines 3821-3920+ (TimeSeriesQuery method)
**Service Type:** Time Series Analytics

#### Method 1: Aggregate() - Line 3688
```go
result, err := t.svc.raftNode.SyncRead(ctx, shardID, key)
```
- **Operation:** Read entities from Raft state machine for time-series aggregation
- **What it does:** Queries Raft for entity data to perform aggregation operations (sum, avg, min, max, etc.)
- **Shard ID:** Fixed to shard 1
- **Production Code:** YES

#### Method 2: TimeSeriesQuery() - Line 3855
```go
result, err := t.svc.raftNode.SyncRead(ctx, shardID, key)
```
- **Operation:** Stream time-series data points matching query criteria
- **What it does:** Queries Raft for entities, then streams matching data points to client
- **Shard ID:** Fixed to shard 1
- **Production Code:** YES

**Migration Path:** These should delegate to qrdb's TimeSeriesService via SDK if available.

---

### 1.2 pkg/service/vector.go - vectorService

**Component:** vectorService (internal to QuicBaaS service)
**Location:** Line 490 (proposeToRaft method)
**Service Type:** Vector Search/HNSW Indexing

#### Method: proposeToRaft() - Line 490
```go
if _, err := v.raftNode.SyncPropose(ctx, v.shardID, cmdBytes); err != nil {
```
- **Operation:** Write vector operations to Raft for consensus
- **What it does:** Proposes vector commands (Create/DeleteNamespace, UpsertVector, etc.) to Raft
- **Shard ID:** v.shardID (usually 2 for vectors)
- **Fallback:** Has SDK delegation fallback via `useSDKDelegation()` and qrdbClient
- **Production Code:** YES

**Status:** Already has fallback to qrdb SDK via `WithQRDBClient()` option, but direct call remains.

---

### 1.3 pkg/secrets/cluster.go - ClusterConfig

**Component:** ClusterConfig (Secrets/DEK cluster replication)
**Location:** Lines 219, 240
**Service Type:** Data Encryption Keys (DEK)

#### Method 1: SetDEK() - Line 219
```go
if _, err := c.raftNode.SyncPropose(ctx, c.config.KMSShardID, cmd); err != nil {
```
- **Operation:** Write DEK to Raft for cluster replication
- **What it does:** Proposes DEK set operation to Raft consensus
- **Shard ID:** c.config.KMSShardID (typically 3)
- **Production Code:** YES

#### Method 2: RotateDEK() - Line 240
```go
if _, err := c.raftNode.SyncPropose(ctx, c.config.KMSShardID, cmd); err != nil {
```
- **Operation:** Rotate DEK in Raft state machine
- **What it does:** Proposes DEK rotation operation for all shards
- **Shard ID:** c.config.KMSShardID (typically 3)
- **Production Code:** YES

---

### 1.4 pkg/operations/rebalance.go - RebalanceManager

**Component:** RebalanceManager (Cluster Rebalancing)
**Location:** Line 437
**Service Type:** Cluster Operations / Rebalancing

#### Method: getShardStats() - Line 437
```go
result, err := r.raftNode.SyncRead(ctx, shardInfo.ShardID, &ShardStatsQuery{})
```
- **Operation:** Read shard statistics from Raft state machine
- **What it does:** Queries each shard for key count and byte count to calculate rebalancing plan
- **Shard ID:** Dynamic - each shard being rebalanced
- **Fallback:** Has error handling with fallback to estimates
- **Production Code:** YES

**Context:** Used in `getShardStats()` and `Plan()` methods for cluster rebalancing operations.

---

### 1.5 pkg/storage/mvcc/transaction.go - MVCCTransaction

**Component:** MVCCTransaction (Distributed transactions)
**Location:** Line 518
**Service Type:** MVCC/Transactions

#### Method: executeOpsViaRaft() - Line 518
```go
if _, err := t.storage.raftNode.ProposeWithMode(t.ctx, t.storage.shardID, cmdBytes, writemode.WALDurable); err != nil {
```
- **Operation:** Write transaction batch with WALDurable mode
- **What it does:** Proposes batch of write operations with durability guarantee
- **Write Mode:** WALDurable (batched fsync, 5-10x faster than Sync)
- **Shard ID:** t.storage.shardID
- **Production Code:** YES

**Note:** Uses `ProposeWithMode()` with explicit durability control for transaction atomicity.

---

## 2. ADAPTER PATTERNS - RaftKVStore WRAPPING

### 2.1 pkg/server/kv_store.go

**Component:** raftKVStore (Type adapter implementing interfaces.KVStore)
**Location:** Lines 83-174
**Purpose:** Wraps raftNode for Raft-based KV storage (cluster mode)

#### Direct Raft Calls in raftKVStore:
- **Line 99:** `s.raftNode.SyncPropose()` - Put operation
- **Line 104:** `s.raftNode.SyncRead()` - Get operation
- **Line 120:** `s.raftNode.SyncPropose()` - Delete operation
- **Line 130:** `s.raftNode.SyncRead()` - Scan operation
- **Line 155:** `s.raftNode.SyncRead()` - List operation

**Where Used:** 
- Returned by `Server.GetKVStore(shardID)` when in cluster/devcluster mode
- Used by services that need KVStore interface (IAM, Secrets, KMS, Dynamic Secrets)
- Indirect consumption through interfaces.KVStore

**Production Code:** YES - Critical path

#### Other Adapters in Same File:
- **localKVStore** (Lines 176-264): For pebble/local mode - still uses `localBackend.SyncPropose/SyncRead`
- **MemoryKVStore** (Lines 266-331): For memory mode - direct sync.Map access

---

### 2.2 pkg/server/local_backend.go

**Component:** LocalBackend (Pebble-based backend without Raft)
**Location:** Lines 1-250+
**Purpose:** Implements interfaces.RaftNode for local PebbleDB mode (bypasses Raft)

#### Method Signatures (implements RaftNode interface):
- `SyncPropose()` - Line 122: Delegates to internal state machine
- `SyncRead()` - Lines 156-163: Queries state machine

**Note:** These are implementations, not consumers of Raft. They're used as drop-in replacements for RaftNode in non-cluster mode.

---

### 2.3 pkg/server/memory_backend.go

**Component:** MemoryBackend (In-memory backend without Raft)
**Location:** Lines 1-200+
**Purpose:** Implements interfaces.RaftNode for standalone memory mode

**Note:** These are implementations, not consumers of Raft. Drop-in replacement for non-cluster mode.

---

## 3. WATCH SYSTEM

### 3.1 pkg/service/quicbaas.go - watch.Manager

**Component:** watch.Manager (Key change notifications)
**Location:** Lines 159, 480
**Service Type:** Watch/Notifications

#### Direct Instantiation:
- **Line 480:** `svc.watchManager = watch.NewManager()`

**Where Used:**
- Internal kvService watches (Watch method)
- Exposed via `WatchManager()` accessor at line 5197
- Used by watchbridge for key-to-messaging integration

**Production Code:** YES - Watch notifications

**Status:** This is a direct instantiation, not a Raft call, but it's an unmediated backend component.

---

## 4. STORAGE ADAPTER PATTERNS

### 4.1 pkg/service/dynamic_secrets.go

**Component:** dynamicSecretsHandler (Dynamic credential generation)
**Location:** Lines 50-58
**Dependencies:** storage.Storage, lease.LeaseManager

#### Direct Storage Access:
- **Line 197:** `s.store.NewIterator()` - Direct storage.Storage access
- **Line 441:** `s.store.Delete()` - Direct storage delete
- **Line 677, 692:** `s.store.Set()` - Direct storage writes

**Storage Type:** storage.Storage interface (can be any backend)
**Production Code:** YES

**Note:** Uses storage.Storage interface (not Raft-specific), but accesses directly without going through services.

---

### 4.2 Index Management

**Component:** index.IndexManager (Secondary indexes)
**Location:** pkg/service/quicbaas.go, line 134
**Status:** Stored as field, no direct Raft calls detected. Used for queryable indexes.

---

## 5. LEASE SYSTEM

### 5.1 pkg/lease/manager.go

**Component:** lease.Manager (Lease lifecycle management)
**Location:** Defined in pkg/lease/manager.go
**Used By:** dynamicSecretsHandler

**Status:** No direct Raft calls in lease manager itself. Used for credential TTL management.

---

## 6. SUMMARY TABLE

| Component | Location | Direct Raft Calls | Type | Shard | Needs Migration |
|-----------|----------|------------------|------|-------|-----------------|
| timeSeriesService.Aggregate() | quicbaas.go:3688 | SyncRead | Read | 1 | YES |
| timeSeriesService.TimeSeriesQuery() | quicbaas.go:3855 | SyncRead | Read | 1 | YES |
| vectorService.proposeToRaft() | vector.go:490 | SyncPropose | Write | 2 | PARTIAL (has SDK fallback) |
| ClusterConfig.SetDEK() | cluster.go:219 | SyncPropose | Write | 3 | YES |
| ClusterConfig.RotateDEK() | cluster.go:240 | SyncPropose | Write | 3 | YES |
| RebalanceManager.Plan() | rebalance.go:437 | SyncRead | Read | Dynamic | YES |
| MVCCTransaction.executeOpsViaRaft() | transaction.go:518 | ProposeWithMode | Write | - | YES |
| raftKVStore (5 methods) | kv_store.go:99-155 | SyncPropose/SyncRead | Both | 1 | Depends on caller |
| dynamicSecretsHandler | dynamic_secrets.go | Direct storage | Write | - | Potential |

---

## 7. MIGRATION PRIORITY

### HIGH PRIORITY (Core Path, Frequently Used)
1. **raftKVStore methods** - Used by all KV/IAM/Secrets/KMS operations
2. **vectorService.proposeToRaft()** - Vector indexing operations
3. **timeSeriesService methods** - Analytics queries

### MEDIUM PRIORITY (Important But Specialized)
4. **MVCCTransaction.executeOpsViaRaft()** - Transaction commits
5. **ClusterConfig DEK operations** - Secrets replication
6. **RebalanceManager** - Cluster maintenance

### LOW PRIORITY (Could Defer)
7. **dynamicSecretsHandler storage access** - Credential generation
8. **watch.Manager** - Not Raft, but unmediated

---

## 8. ARCHITECTURAL NOTES

### raftKVStore Criticality
The `raftKVStore` adapter in `pkg/server/kv_store.go` is the foundation for indirect Raft access:
- It's returned by `Server.GetKVStore()` in cluster mode
- Consumed by EncryptedBackend (Secrets), lease system, and other internal services
- Migrating this single component would eliminate most cluster-mode Raft calls

### Service Layer Abstraction
Services should NOT call raftNode directly. Instead:
- Use GetKVStore() for KV operations (abstracts storage backend)
- Use qrdb SDK for application-level operations
- Delegate analytics to qrdb TimeSeriesService
- Delegate vectors to qrdb VectorService

### Build Tag Caveat
- raftKVStore and localKVStore are in `//go:build raft` files
- Memory/file modes use non-Raft implementations
- These adapters are necessary for the dual-mode architecture

# Plan: Switch TimeSeries Shard 3 from quicbaas's TimeSeriesConsensusFSM to qrdb's FastStateMachine

## Executive Summary

Switching shard 3 to qrdb's FastStateMachine is **FEASIBLE** with proper setup. The FastStateMachine already has full TS operation support (handleTSWrite, handleTSWriteBatch, handleTSWritePoint, handleTSDeleteSeries, handleTSSetRetention) and TS query support (lookupTimeSeries). The main work is:

1. **Routing & Protocol**: Ensure clients use qrdb's protocol opcodes (OpTSWrite=60, OpTSWriteBatch=61, etc.) instead of quicbaas's msgpack-based encoding
2. **TimeSeriesStore Injection**: Attach the same `*timeseries.Storage` instance to FastStateMachine via `WithTimeSeriesStore()`
3. **Registry Swap**: Change the registry to map shard 3 to "fast" instead of "timeseries"

## Analysis: How Shards Are Created

### Shard 1 (KV) - Default Shard
- **Location**: `pkg/server/lifecycle_raft.go:379` — Default if no shards configured: `shardIDs = []uint64{1, ...}`
- **State Machine**: Automatically uses "fast" (FastStateMachine) via registry default
- **Storage**: Uses `s.storage` (quicbaas's `storage.Storage` wrapped as `dragonstorage.Storage`)
- **Registry Lookup**: Resolver.Resolve(1) → returns "fast" (default)

### Shard 3 (TimeSeries) - Current
- **Location**: `pkg/server/lifecycle_raft.go:147-153` — Explicitly configured if `s.smRegistry.Has("timeseries")`
- **ShardConfig**: `&raftsm.ShardConfig{ID: 3, StateMachine: "timeseries"}`
- **Resolver Lookup**: Resolver.Resolve(3) → returns "timeseries" (explicit config)
- **Factory Chain** (lifecycle_raft.go:301-305):
  ```
  smType = s.smResolver.Resolve(3) // Returns "timeseries"
  if smType == "timeseries" && s.timeSeriesStorage != nil:
      return createTimeSeriesStateMachine(shardID, replicaID, s.timeSeriesStorage, s.logger)
      // Calls NewTimeSeriesConsensusFSMWithStorage() (from smfactory_timeseries_raft.go)
  ```
- **Current FSM**: TimeSeriesConsensusFSM (from `pkg/raft/statemachine/timeseries_consensus.go`)

## Key Findings

### 1. How Shard 1 & Shard 3 Are Different

| Aspect | Shard 1 (KV) | Shard 3 (TimeSeries) |
|--------|---|---|
| Registry Entry | `"fast"` (default) | `"timeseries"` (explicit) |
| State Machine Type | FastStateMachine | TimeSeriesConsensusFSM |
| Storage Interface | `dragonstorage.Storage` | `*timeseries.Storage` |
| Factory | `qrdbsm.NewFastStateMachine()` | `NewTimeSeriesConsensusFSMWithStorage()` |
| Creation | `registry.Create("fast", ...)` | Special case in smFactory (line 301-305) |

**Key Detail**: Shard 1 does NOT use the registry factory! It's a special case in smFactory() that directly calls `createTimeSeriesStateMachine()` without going through the registry.

### 2. FastStateMachine Already Supports TS Operations

**qrdb's FastStateMachine** (pkg/statemachine/fast.go):

Dispatch Table (lines 146-176):
```go
f.handlers = map[byte]updateHandler{
    // KV ops
    kv.OpPut:    f.handlePut,
    kv.OpDelete: f.handleDelete,
    protocol.OpBatch: f.handleBatch,
    // ... MVCC, transactions, leases, indexes ...
    // Time series (NEW)
    protocol.OpTSWrite:        f.handleTSWrite,      // opcode 60
    protocol.OpTSWriteBatch:   f.handleTSWriteBatch, // opcode 61
    protocol.OpTSWritePoint:   f.handleTSWritePoint, // opcode 62
    protocol.OpTSDeleteSeries: f.handleTSDeleteSeries, // opcode 63
    protocol.OpTSSetRetention: f.handleTSSetRetention, // opcode 64
}
```

**Handlers** (fast_timeseries.go:31-143):
- `handleTSWrite()` — Decodes `protocol.DecodeTSWrite()`, delegates to `f.tsStore.WriteDataPoint()`
- `handleTSWriteBatch()` — Decodes batch, calls `f.tsStore.WriteDataPoints()`
- `handleTSWritePoint()` — Same as Write (op-code distinction only)
- `handleTSDeleteSeries()` — Calls `f.tsStore.DeleteSeries()`
- `handleTSSetRetention()` — Calls `f.tsStore.SetRetentionPolicy()`

**Query Support** (Lookup path, fast_timeseries.go:150-238):
- Handles `timeseries.TSQueryRequest` (range queries)
- Handles `timeseries.TSAggregateRequest` (aggregations)
- Handles `timeseries.TSRichQueryRequest` (rich filtering/grouping)
- Handles `timeseries.TSGetRetentionRequest` (get retention)
- Handles `timeseries.TSListMetricsRequest` (list metrics)

### 3. Wire Format Difference: CRITICAL

**quicbaas's TimeSeriesConsensusFSM**:
- Uses **msgpack** binary encoding
- Defines custom op types: `TSOpWritePoint=1, TSOpWriteBatch=2, TSOpDeleteSeries=3, TSOpSetRetention=4`
- Entry: `TimeSeriesEntry` struct marshaled with msgpack

**qrdb's FastStateMachine**:
- Uses **qrdb protocol** binary encoding (little-endian, length-prefixed)
- Op codes: `OpTSWrite=60, OpTSWriteBatch=61, OpTSWritePoint=62, OpTSDeleteSeries=63, OpTSSetRetention=64`
- Entries decoded via `protocol.DecodeTSWrite()`, `protocol.DecodeTSWriteBatch()`, etc.

**Impact**: Clients sending TS commands to shard 3 must use qrdb's protocol, NOT quicbaas's msgpack format.

### 4. Registry/Resolver Architecture

**File**: `pkg/raft/statemachine/registry.go` (lines 54-100)

Built-in factories registered:
```go
r.factories["fast"] = func(...) sm.StateMachine { ... FastStateMachine ... }
r.factories["minimal"] = func(...) sm.StateMachine { ... MinimalStateMachine ... }
r.factories["vector"] = func(...) sm.StateMachine { ... VectorConsensusFSM ... }
registerTimeSeriesBuiltin(r) // Registers "timeseries" if build tag enabled
```

**File**: `pkg/raft/statemachine/registry_timeseries.go` (lines 29-35)

Current registration:
```go
r.factories["timeseries"] = func(shardID, replicaID uint64, store storage.Storage, log *logger.Logger) sm.StateMachine {
    return NewTimeSeriesConsensusFSM(shardID, replicaID, log)
    // NOTE: store is ignored! TimeSeriesConsensusFSM has its own storage
}
```

**File**: `pkg/raft/statemachine/resolver.go` (lines 85-105)

Resolver.Resolve() logic:
```go
func (r *Resolver) Resolve(shardID uint64) string {
    if cfg, ok := r.shardConfigs[shardID]; ok && cfg.StateMachine != "" {
        return cfg.StateMachine  // Explicit config takes priority
    }
    switch shardID {
    case KMSShardID:
        return "fast"
    case TimeSeriesShardID:  // = 3
        if r.registry.Has("timeseries") {
            return "timeseries"  // Current behavior
        }
        return r.registry.Default()  // Fallback to "fast"
    default:
        return r.registry.Default()  // = "fast"
    }
}
```

**Key Insight**: The hardcoded switch case for TimeSeriesShardID is NOT the primary mechanism! The resolver prefers explicit `ShardConfig` entries (line 87). The server initializes explicit configs in `initStateMachineRegistry()` (line 147-152):

```go
if s.smRegistry.Has("timeseries") {
    shardConfigs = append(shardConfigs, &raftsm.ShardConfig{
        ID:           raftsm.TimeSeriesShardID,  // = 3
        StateMachine: "timeseries",  // Explicit
    })
}
```

### 5. How TS Storage is Injected Currently

**File**: `pkg/server/lifecycle_raft.go:301-305` (special case in smFactory)

```go
if smType == "timeseries" && s.timeSeriesStorage != nil {
    if tsSM := createTimeSeriesStateMachine(shardID, replicaID, s.timeSeriesStorage, s.logger); tsSM != nil {
        s.logger.Infof("Creating TimeSeriesConsensusFSM for shard %d...", shardID)
        return tsSM
    }
}
```

The special case:
1. Calls `createTimeSeriesStateMachine()` (from smfactory_timeseries_raft.go)
2. Passes `s.timeSeriesStorage` directly to the FSM

**File**: `pkg/server/smfactory_timeseries_raft.go:29-31`

```go
func createTimeSeriesStateMachine(shardID, replicaID uint64, storage *timeseries.Storage, log *logger.Logger) sm.StateMachine {
    return raftsm.NewTimeSeriesConsensusFSMWithStorage(shardID, replicaID, storage, log)
}
```

This returns a TimeSeriesConsensusFSM with the storage already attached. The same storage instance is used across all replicas.

### 6. TS Query Support Differences

**quicbaas's TimeSeriesConsensusFSM.Lookup()** (timeseries_consensus.go:335-365):
- Accepts only **string queries** (metric name) → returns retention policy
- Or **nil queries** → returns list of metrics
- No support for data range queries via Lookup

**qrdb's FastStateMachine.lookupTimeSeries()** (fast_timeseries.go:150-238):
- Accepts rich query types:
  - `timeseries.TSQueryRequest` / pointer → range query via `f.tsStore.QueryRange()`
  - `timeseries.TSAggregateRequest` / pointer → aggregate via `f.tsStore.QueryAggregate()`
  - `timeseries.TSRichQueryRequest` / pointer → rich query via `f.tsStore.QueryRich()`
  - `timeseries.TSGetRetentionRequest` / pointer → get policy via `f.tsStore.GetRetentionPolicy()`
  - `timeseries.TSListMetricsRequest` / pointer → list metrics via `f.tsStore.ListMetrics()`

**Impact**: Clients can perform full TS range queries via FastStateMachine, not just retention lookups.

---

## Migration Plan: 3 Options

### Option A: Direct Registry Change (SIMPLE, Recommended for MVP)

**Steps**:
1. Modify `pkg/raft/statemachine/registry_timeseries.go` (lines 29-35):
   ```go
   // Change from TimeSeriesConsensusFSM to FastStateMachine
   r.factories["timeseries"] = func(shardID, replicaID uint64, store storage.Storage, log *logger.Logger) sm.StateMachine {
       adapted := qrdbadapter.NewStorageAdapter(store)
       return qrdbsm.NewFastStateMachine(shardID, replicaID, adapted, qrdbsm.WithLogger(slog.Default()))
   }
   ```

2. Inject TimeSeriesStore via WithTimeSeriesStore():
   - Still need special case in `smfactory()` to pass `s.timeSeriesStorage` to FastStateMachine's options
   - Or: Lazy initialize TimeSeriesStore inside FastStateMachine via a factory function

3. Update `pkg/server/lifecycle_raft.go` smFactory():
   - Remove special case for "timeseries" (line 301-305)
   - Let the registry handle it normally
   - If TS storage injection needed, pass it via an Option

**Pros**:
- Minimal code changes
- Reuses existing registry pattern
- "timeseries" factory can still use custom logic

**Cons**:
- Registry factory signature `StateMachineFactoryFunc` expects `storage.Storage`, not `WithTimeSeriesStore` options
- Need to wire the TimeSeriesStore separately

### Option B: Resolver ShardConfig Override (FLEXIBLE)

**Steps**:
1. Keep registry unchanged
2. In `initStateMachineRegistry()` (line 144-152), change the ShardConfig:
   ```go
   if s.smRegistry.Has("timeseries") {
       shardConfigs = append(shardConfigs, &raftsm.ShardConfig{
           ID:           raftsm.TimeSeriesShardID,  // = 3
           StateMachine: "fast",  // CHANGE: use qrdb's FastStateMachine instead
       })
   }
   ```

3. Update the smFactory special case (line 301-305) to also handle when `smType == "fast" && shardID == 3`:
   ```go
   if (smType == "fast" && shardID == raftsm.TimeSeriesShardID) ||
      (smType == "timeseries" && shardID == raftsm.TimeSeriesShardID) {
       if s.timeSeriesStorage != nil {
           // Attach TimeSeriesStore to FastStateMachine
           adapted := qrdbadapter.NewStorageAdapter(store)
           fsm := qrdbsm.NewFastStateMachine(shardID, replicaID, adapted, qrdbsm.WithLogger(...))
           fsm.WithTimeSeriesStore(s.timeSeriesStorage)  // Needs method or option
           return fsm
       }
   }
   ```

**Pros**:
- Keeps registry and factory separation of concerns clean
- Clear override mechanism for shard-specific behavior

**Cons**:
- Harder to follow the flow (split between initStateMachineRegistry and smFactory)
- Still need to modify FastStateMachine to support late binding of TimeSeriesStore

### Option C: Configuration-Driven (FUTURE-PROOF)

**Steps**:
1. Add config option: `server.TimeSeries.StateMachine` (default: "timeseries", can be "fast")
2. In `initStateMachineRegistry()`, use config instead of hardcoded "timeseries"
3. Clients control which FSM to use via config

**Pros**:
- Zero-downtime migration path (can switch per environment)
- Most flexible for testing and rollout

**Cons**:
- Requires config schema changes
- More code overall

---

## Implementation Details: Protocol Compatibility Layer

### Problem: Wire Format Mismatch

Clients currently send **quicbaas msgpack** TS commands to shard 3. If we switch to FastStateMachine, clients must send **qrdb protocol** TS commands (opcodes 60-64).

### Solution Paths:

**Path 1: Update All Clients** (BREAKING CHANGE)
- Update all services that write to shard 3 to use qrdb's `protocol.EncodeTSWrite()`, etc.
- Replace msgpack with binary protocol encoding

**Path 2: Protocol Adapter FSM** (WRAPPER)
- Create a wrapper FSM that translates quicbaas msgpack to qrdb protocol
- Wraps FastStateMachine, intercepts Update() calls, decodes msgpack, re-encodes qrdb format, then delegates
- Example:
  ```go
  type ProtocolAdapterFSM struct {
      inner *FastStateMachine
  }
  func (fsm *ProtocolAdapterFSM) Update(ctx context.Context, entries []sm.Entry, results []sm.Result) error {
      for i := range entries {
          if entries[i].Cmd[0] is quicbaas TimeSeriesConsensusOp {
              // Decode msgpack TimeSeriesEntry
              // Re-encode as qrdb protocol
              // Forward to inner FSM
          }
      }
  }
  ```

**Path 3: Hybrid Factory** (GRADUAL)
- Keep TimeSeriesConsensusFSM in registry as-is
- Create a new "fast-timeseries" factory entry that uses FastStateMachine
- Route shard 3 to "fast-timeseries" or "timeseries" based on config/feature flag
- Eventually deprecate "timeseries"

---

## Recommended Path: Option A + Path 1 (Protocol Update)

### Why?

1. **Cleanest**: No adapter/wrapper complexity
2. **Aligned with qrdb**: Clients already use qrdb protocol for other shards
3. **Minimal code**: 3-4 file changes
4. **Performance**: Direct protocol, no wrapper overhead
5. **Testability**: Each shard independent

### Changes:

**File 1**: `pkg/server/lifecycle_raft.go` (3 changes)

1. **Line 147-152** (initStateMachineRegistry): Add TS injection option:
   ```go
   if s.smRegistry.Has("timeseries") {
       // Still register the ShardConfig, but now we'll handle the special case below
       // to wire up TimeSeriesStore to FastStateMachine
   }
   ```

2. **Line 301-306** (smFactory special case): Inject TS storage:
   ```go
   if smType == "timeseries" && s.timeSeriesStorage != nil {
       // CHANGE: Create FastStateMachine with WithTimeSeriesStore option
       adapted := qrdbadapter.NewStorageAdapter(store)
       fsm := qrdbsm.NewFastStateMachine(shardID, replicaID, adapted, 
           qrdbsm.WithLogger(slog.Default()),
           qrdbsm.WithTimeSeriesStore(s.timeSeriesStorage),  // Inject storage
       )
       s.logger.Infof("Creating FastStateMachine for shard %d (TS mode)...", shardID)
       return fsm
   }
   ```

3. **Line 147-152** (initStateMachineRegistry): CHANGE ShardConfig from "timeseries" to "fast":
   ```go
   if s.smRegistry.Has("timeseries") {  // Checks if build tag enabled, not factory
       shardConfigs = append(shardConfigs, &raftsm.ShardConfig{
           ID:           raftsm.TimeSeriesShardID,
           StateMachine: "fast",  // CHANGE from "timeseries" to "fast"
       })
   }
   ```

**File 2**: `pkg/raft/statemachine/smfactory_timeseries_raft.go` (DELETE or mark DEPRECATED)
- The special factory function becomes unused
- Can delete or keep as a deprecated stub

**File 3**: `pkg/raft/statemachine/registry_timeseries.go` (MARK DEPRECATED)
- The "timeseries" factory entry is no longer used
- Update comment: "Deprecated: Use 'fast' StateMachine with WithTimeSeriesStore() instead"
- Can keep for backward compatibility or delete

**File 4**: Client code that sends TS writes to shard 3
- Update from msgpack `TimeSeriesEntry` encoding to qrdb `protocol.EncodeTSWrite()` / `protocol.EncodeTSWriteBatch()`

### Backward Compatibility

- **Snapshot format**: FastStateMachine snapshot format differs from TimeSeriesConsensusFSM
  - Existing shard 3 snapshots will NOT be compatible
  - Requires either: snapshot migration tool OR clean restart with fresh shard 3 state
  - Cluster will auto-rebuild via Raft log replay and compaction

- **Raft log**: Old msgpack entries in the log will fail to decode
  - FastStateMachine's Update() will receive msgpack bytes
  - Protocol adapter wrapper (if using Path 2) can translate
  - Or: Fresh shard 3 (clean snapshot) → old log ignored

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Wire format incompatibility | Shard 3 crashes on msgpack entries | Use protocol adapter wrapper, or clean snapshot |
| Snapshot format change | Can't recover old snapshots | Accept data loss, or build migration tool |
| Query API differences | Client code breaks | Clients already use qrdb API (TSQueryRequest, etc.) |
| Build tag absence | No TS FSM available | Gracefully fall back to "fast" (already handles missing tsStore) |
| Cross-shard consistency | TS data split across shards | Use router + sharding strategy (same as current) |

---

## Summary: Switching Shard 3 to FastStateMachine

### Steps to Execute:

1. **Update ShardConfig** (lifecycle_raft.go:147-152):
   - Change `StateMachine: "timeseries"` → `StateMachine: "fast"`

2. **Update smFactory** (lifecycle_raft.go:301-306):
   - Detect shard == TimeSeriesShardID AND smType == "fast"
   - Create FastStateMachine with `WithTimeSeriesStore(s.timeSeriesStorage)`
   - Remove or adapt the old special case for "timeseries"

3. **Update Clients** (all TS write paths):
   - Replace msgpack encoding with qrdb protocol
   - Use `protocol.EncodeTSWrite()`, `protocol.EncodeTSWriteBatch()`, etc.

4. **Testing**:
   - New unit tests for FastStateMachine + TimeSeriesStore integration
   - Integration tests: write/read via qrdb protocol, verify TS storage updated
   - Cross-shard TS test: confirm TS reads still work

5. **Migration**:
   - Deploy to dev/staging first
   - Verify TS reads/writes work
   - Accept shard 3 state loss (clean rebuild from log)
   - Gradual rollout to production

### Timeline:
- Code changes: 2-3 hours (4-5 files)
- Testing: 4-6 hours (new tests + integration validation)
- Review & merge: 1-2 hours
- **Total: 1 day**

### Effort Complexity: LOW
- No new interfaces needed
- No new storage backends
- FastStateMachine already has full TS support
- Just wiring/routing changes

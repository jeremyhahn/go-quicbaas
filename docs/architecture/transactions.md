# Transaction Architecture

## Overview

Multi-Version Concurrency Control (MVCC) transaction system for QuicBaaS. Enables transactional and non-transactional operations with strong consistency guarantees.

## Architecture Layers

```
+-------------------------------------------------------------+
|  API Layer (gRPC, REST, CLI)                                |
|  - Txn(), Put(), Get(), Delete()                            |
+----------------------------+--------------------------------+
                             |
+----------------------------v--------------------------------+
|  Transaction Coordinator                                    |
|  - Transaction lifecycle management                         |
|  - Compare-and-swap validation                              |
|  - Success/failure operation execution                      |
|  - Conflict detection                                       |
+----------------------------+--------------------------------+
                             |
+----------------------------v--------------------------------+
|  MVCC Storage Layer                                         |
|  - Versioned key-value storage                              |
|  - Revision tracking (global counter)                       |
|  - Version history per key                                  |
|  - Garbage collection                                       |
+----------------------------+--------------------------------+
                             |
+----------------------------v--------------------------------+
|  PebbleDB Storage Engine                                    |
|  - Atomic batch operations                                  |
|  - Persistent storage                                       |
|  - Snapshots and iterators                                  |
+-------------------------------------------------------------+
```

## Key Concepts

### 1. Revision Numbers

Global revision: monotonically increasing counter for all modifications, incremented on every Put/Delete/Transaction, used for point-in-time reads and watches.

### 2. Key Versioning

Each key maintains: CreateRevision (first created), ModRevision (last modification), Version (modification count), Value (actual data).

### 3. Key Encoding

Keys are encoded with revisions to support MVCC:

```
Format: <user_key>/<revision>

Example: "mykey/123" for user key "mykey" at revision 123

Special metadata keys:
- "mvcc/rev" -> current global revision
- "mvcc/meta/<key>" -> {CreateRevision, ModRevision, Version}
```

### 4. Transaction Semantics

Transactions use a compare-and-swap model: compare phase (check conditions), execute phase (apply success or failure operations atomically), commit (all or nothing).

## Data Structures

### MVCCStorage Interface

```go
type MVCCStorage interface {
    // Core MVCC operations
    Put(ctx context.Context, key, value []byte) (*KeyValue, int64, error)
    Get(ctx context.Context, key []byte, rev int64) (*KeyValue, error)
    Delete(ctx context.Context, key []byte) (*KeyValue, int64, error)

    // Range operations
    Range(ctx context.Context, start, end []byte, rev int64, limit int64) ([]*KeyValue, error)

    // Transaction support
    BeginTxn(ctx context.Context) (Transaction, error)

    // Revision management
    CurrentRevision() int64
    Compact(ctx context.Context, rev int64) error
}

type KeyValue struct {
    Key            []byte
    Value          []byte
    CreateRevision int64
    ModRevision    int64
    Version        int64
    Lease          int64  // For lease support (future)
}

type Transaction interface {
    // Add operations to transaction
    Put(key, value []byte) error
    Delete(key []byte) error
    Get(key []byte) (*KeyValue, error)

    // Compare operations for conditional execution
    AddCompare(cmp Compare) error

    // Commit or rollback
    Commit() error
    Rollback() error
}

type Compare struct {
    Key      []byte
    Target   CompareTarget  // VERSION, VALUE, MOD, CREATE
    Result   CompareResult  // EQUAL, NOT_EQUAL, GREATER, LESS
    Value    interface{}    // For VALUE target
    Revision int64          // For VERSION/MOD/CREATE targets
}
```

### Storage Layout

PebbleDB stores three types of entries:

1. **Data entries**: `data/<key>/<revision>` -> `<value>`
2. **Metadata entries**: `meta/<key>` -> `{CreateRevision, ModRevision, Version}`
3. **System entries**: `sys/revision` -> `<current_revision>`

## Transaction Flow

### Non-Transactional Operations

Simple operations (Put, Get, Delete) bypass the transaction coordinator:

```
1. Put("key", "value")
   |-- Increment global revision (123 -> 124)
   |-- Write: data/key/124 -> "value"
   |-- Update: meta/key -> {Create: 120, Mod: 124, Ver: 5}
   +-- Update: sys/revision -> 124
```

### Transactional Operations

Complex operations use the transaction coordinator:

```
1. Txn.Compare(key="foo", mod_revision=120)
   |-- Read: meta/foo -> {Create: 110, Mod: 120, Ver: 3}
   +-- Compare: 120 == 120 (match)

2. Txn.Success: Put("foo", "new_value")
   |-- Increment revision (124 -> 125)
   |-- Batch:
   |   |-- data/foo/125 -> "new_value"
   |   |-- meta/foo -> {Create: 110, Mod: 125, Ver: 4}
   |   +-- sys/revision -> 125
   +-- Commit batch atomically
```

## Performance Considerations

### Optimization Strategies

Read path: use PebbleDB snapshots for consistent reads. Write path: batch all transaction operations. Metadata: cache frequently accessed metadata. Compaction: background GC of old revisions.

### Expected Performance

Non-transactional operations: no overhead. Single-key transactions: ~10-20% overhead. Multi-key transactions: ~20-30% overhead.

## Garbage Collection

Old revisions cleaned up via: compaction (remove old revisions), retention policy (keep last N revisions per key), background GC (periodic cleanup).

## Consistency Guarantees

Snapshot isolation: transactions see consistent snapshot. Serializable: compare operations detect conflicts.

Conflict detection via ModRevision checks: if ModRevision changed between read and commit, abort transaction (optimistic concurrency control).

## Transaction Types

QuicBaaS provides multiple transaction mechanisms for different use cases:

### DAO-Level Transactions

Entity-aware transactions for single-entity-type operations:

```go
tx, _ := userDAO.BeginTx(ctx)
tx.Save(user1)
tx.Save(user2)
tx.Delete(user3)
tx.Commit(ctx)
```

See [DAO Transactions](../transactions/dao_transactions.md) for details.

### Distributed Transactions (2PC)

Cross-shard atomic transactions using Two-Phase Commit:

```go
txnID, _ := coordinator.Begin(ctx, ops, conditions)
coordinator.Prepare(ctx, txnID)
coordinator.Commit(ctx, txnID)
```

See [Distributed Transactions](../transactions/distributed_transactions.md) for details.

### Optimistic Locking

Version-based concurrency control:

```go
tx.SaveWithCondition(entity, Condition{
    Target:   ConditionVersion,
    Operator: OpEqual,
    Value:    expectedVersion,
})
```

See [Optimistic Locking](../transactions/optimistic_locking.md) for details.

## Related Documentation

- [DAO Transactions](../transactions/dao_transactions.md) - Entity-level transactions
- [Distributed Transactions](../transactions/distributed_transactions.md) - 2PC coordination
- [Optimistic Locking](../transactions/optimistic_locking.md) - Version-based concurrency
- [Conditional Batch Operations](../batch/conditional_batch.md) - Batch preconditions
- [Batch Operations](batch-operations.md) - Atomic batch operations

## References

- [CockroachDB MVCC](https://www.cockroachlabs.com/docs/stable/architecture/transaction-layer.html)
- [Google Percolator](https://research.google/pubs/pub36726/)

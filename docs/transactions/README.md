# QuicBaaS Transactions

Transaction support for atomic operations with strong consistency guarantees.

## Overview

QuicBaaS provides multiple transaction mechanisms to handle different consistency and atomicity requirements:

| Type | Scope | Use Case |
|------|-------|----------|
| DAO Transactions | Single entity type | Application-level CRUD |
| Distributed Transactions | Cross-shard | Multi-partition atomicity |
| Optimistic Locking | Per-entity | Conflict detection |

## Documentation

### DAO Transactions

Entity-aware transactions with read-your-own-writes semantics.

- [DAO Transactions](dao_transactions.md) - Full documentation

```go
tx, _ := userDAO.BeginTx(ctx)
defer tx.Rollback()

tx.Save(user1)
tx.Save(user2)
tx.Delete(user3)

tx.Commit(ctx)
```

### Distributed Transactions (2PC)

Two-Phase Commit for atomic operations across multiple Raft shards.

- [Distributed Transactions](distributed_transactions.md) - Full documentation

```go
ops := map[uint64][]coordinator.TxnOp{
    shard1: {/* operations */},
    shard2: {/* operations */},
}

txnID, _ := coordinator.Begin(ctx, ops, conditions)
coordinator.Prepare(ctx, txnID)
coordinator.Commit(ctx, txnID)
```

### Optimistic Locking

Version-based concurrency control for conflict detection.

- [Optimistic Locking](optimistic_locking.md) - Full documentation

```go
err := tx.SaveWithCondition(entity, dao.Condition{
    Target:   dao.ConditionVersion,
    Operator: dao.OpEqual,
    Value:    expectedVersion,
})
```

## Choosing the Right Transaction Type

| Scenario | Recommended |
|----------|-------------|
| Single entity CRUD | DAO Transaction |
| Multiple related entities (same type) | DAO Transaction with batch |
| Cross-shard operations | 2PC Distributed Transaction |
| Concurrent updates | Optimistic Locking |
| High-contention writes | Optimistic Locking with retry |

## Related Documentation

- [Batch Operations](../architecture/batch-operations.md)
- [Conditional Batch Operations](../batch/conditional_batch.md)
- [MVCC Architecture](../architecture/transactions.md)

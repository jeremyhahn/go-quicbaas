# Distributed Transactions (2PC)

Two-Phase Commit (2PC) coordination for atomic distributed transactions across multiple Raft shards with strong consistency guarantees.

## Overview

The 2PC Coordinator enables atomic commit or abort of transactions spanning multiple partitions (shards). It ensures that either all participating shards commit the transaction or all abort, maintaining data consistency across the distributed system.

## Architecture

```
                        ┌─────────────────────┐
                        │   2PC Coordinator   │
                        │  - Begin            │
                        │  - Prepare          │
                        │  - Commit/Abort     │
                        │  - Recovery         │
                        └─────────┬───────────┘
                                  │
         ┌────────────────────────┼────────────────────────┐
         │                        │                        │
         v                        v                        v
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  Participant 1  │    │  Participant 2  │    │  Participant N  │
│    (Shard 1)    │    │    (Shard 2)    │    │    (Shard N)    │
│                 │    │                 │    │                 │
│  - Prepare      │    │  - Prepare      │    │  - Prepare      │
│  - Commit       │    │  - Commit       │    │  - Commit       │
│  - Abort        │    │  - Abort        │    │  - Abort        │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## Transaction States

```
   ┌──────────────┐
   │  INITIATED   │
   └──────┬───────┘
          │ Begin
          v
   ┌──────────────┐     Prepare Failed
   │  PREPARING   │─────────────────────┐
   └──────┬───────┘                     │
          │ All Prepared                │
          v                             v
   ┌──────────────┐              ┌──────────────┐
   │   PREPARED   │              │   ABORTING   │
   └──────┬───────┘              └──────┬───────┘
          │ Commit Decision             │
          v                             v
   ┌──────────────┐              ┌──────────────┐
   │  COMMITTING  │              │   ABORTED    │
   └──────┬───────┘              └──────────────┘
          │
          v
   ┌──────────────┐
   │  COMMITTED   │
   └──────────────┘
```

## Quick Start

```go
// Create coordinator
coordinator := coordinator.NewCoordinator(nodeHost, store, logger)

// Register participants for each shard
for shardID, participant := range participants {
    coordinator.RegisterParticipant(shardID, participant)
}

// Define operations per shard
ops := map[uint64][]coordinator.TxnOp{
    1: { // Shard 1 operations
        {Type: coordinator.TxnOpPut, Key: []byte("user:1"), Value: userBytes},
    },
    2: { // Shard 2 operations
        {Type: coordinator.TxnOpPut, Key: []byte("order:1"), Value: orderBytes},
    },
}

// Begin transaction
txnID, err := coordinator.Begin(ctx, ops, nil)
if err != nil {
    return err
}

// Execute 2PC
if err := coordinator.Prepare(ctx, txnID); err != nil {
    coordinator.Abort(ctx, txnID)
    return err
}

if err := coordinator.Commit(ctx, txnID); err != nil {
    return err
}
```

## API Reference

### Coordinator

```go
// NewCoordinator creates a new 2PC coordinator.
func NewCoordinator(
    nodeHost *quicraft.NodeHost,
    store storage.Storage,
    log *logger.Logger,
) *Coordinator

// Begin starts a new distributed transaction.
func (c *Coordinator) Begin(
    ctx context.Context,
    ops map[uint64][]TxnOp,
    conditions []BatchCondition,
) (TransactionID, error)

// Prepare executes the prepare phase.
func (c *Coordinator) Prepare(ctx context.Context, txnID TransactionID) error

// Commit executes the commit phase.
func (c *Coordinator) Commit(ctx context.Context, txnID TransactionID) error

// Abort aborts a transaction.
func (c *Coordinator) Abort(ctx context.Context, txnID TransactionID) error

// GetStatus returns the current transaction status.
func (c *Coordinator) GetStatus(txnID TransactionID) (*DistributedTransaction, error)

// RegisterParticipant registers a participant for a shard.
func (c *Coordinator) RegisterParticipant(shardID uint64, p *Participant)
```

### Transaction Operations

```go
type TxnOp struct {
    Type  TxnOpType  // TxnOpPut, TxnOpDelete, TxnOpGet
    Key   []byte
    Value []byte     // Only for TxnOpPut
}
```

### Batch Conditions

Apply preconditions that must be satisfied before commit:

```go
type BatchCondition struct {
    Target   CompareTarget  // Version, CreateRevision, ModRevision, Value
    Op       CompareOp      // Equal, NotEqual, Less, Greater
    Key      []byte
    Value    []byte         // For value comparisons
    Revision int64          // For revision comparisons
    Version  int64          // For version comparisons
}
```

## Participant Implementation

Each shard requires a Participant that handles the 2PC protocol:

```go
type Participant struct {
    shardID     uint64
    store       storage.Storage
    preparedTxn sync.Map // Stores prepared transactions
}

// Prepare validates and locks resources.
func (p *Participant) Prepare(
    ctx context.Context,
    txnID TransactionID,
    ops []TxnOp,
    conditions []BatchCondition,
) error

// Commit applies the prepared operations.
func (p *Participant) Commit(ctx context.Context, txnID TransactionID) error

// Abort releases locks and discards prepared operations.
func (p *Participant) Abort(ctx context.Context, txnID TransactionID) error
```

## Recovery

The coordinator persists transaction state for crash recovery:

```go
// On startup, recover pending transactions
recovery := coordinator.NewRecovery(store, logger)
pendingTxns, err := recovery.RecoverPendingTransactions()

for _, txn := range pendingTxns {
    switch txn.State() {
    case TxnStateCommitting:
        // Transaction was decided to commit - complete it
        coordinator.Commit(ctx, txn.ID)
    case TxnStatePreparing, TxnStatePrepared:
        // Uncertain state - abort to be safe
        coordinator.Abort(ctx, txn.ID)
    }
}
```

### Recovery States

| State | Recovery Action |
|-------|-----------------|
| Initiated | Abort |
| Preparing | Abort |
| Prepared | Abort (timeout expired) or retry prepare |
| Committing | Complete commit |
| Committed | Clean up state |
| Aborting | Complete abort |
| Aborted | Clean up state |

## Timeout Configuration

```go
const (
    DefaultPrepareTimeout     = 10 * time.Second
    DefaultCommitTimeout      = 30 * time.Second
    DefaultAbortTimeout       = 10 * time.Second
    DefaultTransactionTimeout = 60 * time.Second
    DefaultRecoveryInterval   = 5 * time.Second
)
```

## Use Cases

### Cross-Shard Transfer

```go
// Transfer funds between accounts on different shards
ops := map[uint64][]coordinator.TxnOp{
    1: { // Debit from account A
        {Type: TxnOpPut, Key: []byte("account:A"), Value: debitedBalance},
    },
    2: { // Credit to account B
        {Type: TxnOpPut, Key: []byte("account:B"), Value: creditedBalance},
    },
}

// Add condition: only if account A has sufficient balance
conditions := []BatchCondition{
    {
        Target: CompareValue,
        Op:     CompareGreater,
        Key:    []byte("account:A:balance"),
        Value:  minimumBalance,
    },
}

txnID, _ := coordinator.Begin(ctx, ops, conditions)
coordinator.Prepare(ctx, txnID)
coordinator.Commit(ctx, txnID)
```

### Multi-Entity Creation

```go
// Create user and associated data across shards
ops := map[uint64][]coordinator.TxnOp{
    userShard: {
        {Type: TxnOpPut, Key: userKey, Value: userData},
    },
    profileShard: {
        {Type: TxnOpPut, Key: profileKey, Value: profileData},
    },
    settingsShard: {
        {Type: TxnOpPut, Key: settingsKey, Value: settingsData},
    },
}

txnID, _ := coordinator.Begin(ctx, ops, nil)
if err := coordinator.Prepare(ctx, txnID); err != nil {
    coordinator.Abort(ctx, txnID)
    return err
}
coordinator.Commit(ctx, txnID)
```

## Metrics

The coordinator exports Prometheus metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_coordinator_transactions_total` | Counter | Total transactions by status |
| `quicbaas_coordinator_prepare_latency_seconds` | Histogram | Prepare phase latency |
| `quicbaas_coordinator_commit_latency_seconds` | Histogram | Commit phase latency |
| `quicbaas_coordinator_abort_latency_seconds` | Histogram | Abort phase latency |
| `quicbaas_coordinator_active_transactions` | Gauge | Currently active transactions |
| `quicbaas_coordinator_prepare_failures_total` | Counter | Prepare failures by reason |
| `quicbaas_coordinator_commit_failures_total` | Counter | Commit failures by reason |
| `quicbaas_coordinator_condition_failures_total` | Counter | Condition check failures |
| `quicbaas_coordinator_recovered_transactions_total` | Counter | Recovered transactions |

## Best Practices

1. **Keep Transactions Short** - Minimize the time between prepare and commit
2. **Handle Failures** - Always have a fallback plan for abort scenarios
3. **Use Conditions** - Leverage batch conditions for optimistic locking
4. **Monitor Metrics** - Track transaction latencies and failure rates
5. **Test Recovery** - Verify recovery works correctly for your scenarios

## Limitations

- Performance overhead compared to single-shard operations
- Blocking protocol - participants hold locks during 2PC
- Coordinator is a single point of coordination (but not failure with persistence)
- Network partitions can cause transaction timeouts

## Related Documentation

- [DAO Transactions](dao_transactions.md) - Single-entity transactions
- [Optimistic Locking](optimistic_locking.md) - Version-based concurrency
- [Conditional Batch Operations](../batch/conditional_batch.md) - Batch conditions
- [Raft Consensus](../architecture/raft-consensus.md) - Underlying consensus

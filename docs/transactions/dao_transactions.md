# DAO-Level Transactions

Entity-aware transaction API providing atomic operations, read-your-own-writes semantics, and optimistic locking for QuicBaaS's Data Access Object layer.

## Overview

The DAO transaction system extends GenericDAO with transaction capabilities, enabling atomic multi-operation commits across entities of the same type. It provides read-your-own-writes semantics within transaction boundaries and supports conditional saves for optimistic concurrency control.

## Key Features

- **Atomic Commits** - All queued operations succeed or all fail
- **Read-Your-Own-Writes** - Immediate visibility of uncommitted changes within transaction
- **Optimistic Locking** - Version-based conditional saves for conflict detection
- **Auto ID Generation** - Automatic timestamp-based IDs for new entities
- **Safe Rollback** - Rollback is always safe to call, even after commit

## Quick Start

```go
// Create a TransactionalDAO
userDAO, err := dao.NewTransactional[*User](
    kvStore,
    "users",
    func() *User { return &User{} },
)

// Begin transaction
tx, err := userDAO.BeginTx(ctx)
if err != nil {
    return err
}
defer tx.Rollback() // Safe to call even after commit

// Queue operations
tx.Save(user1)
tx.Save(user2)
tx.Delete(user3)

// Commit atomically
return tx.Commit(ctx)
```

## API Reference

### TransactionalDAO Interface

```go
type TransactionalDAO[E Entity] interface {
    GenericDAO[E]

    // BeginTx starts a new transaction.
    BeginTx(ctx context.Context) (DAOTransaction[E], error)
}
```

### DAOTransaction Interface

```go
type DAOTransaction[E Entity] interface {
    // Save queues an entity for insert/update.
    Save(entity E) error

    // Delete queues an entity for deletion.
    Delete(entity E) error

    // Get retrieves an entity within transaction snapshot.
    // Returns entities from write cache first (read-your-own-writes).
    Get(id uint64) (E, error)

    // SaveWithCondition saves only if condition is met.
    SaveWithCondition(entity E, cond Condition) error

    // Commit atomically applies all queued operations.
    Commit(ctx context.Context) error

    // Rollback discards all queued operations.
    Rollback() error

    // IsClosed returns true if transaction has been committed or rolled back.
    IsClosed() bool
}
```

## Read-Your-Own-Writes

The transaction maintains a write cache that provides immediate visibility of uncommitted changes:

```go
tx, _ := userDAO.BeginTx(ctx)
defer tx.Rollback()

// Create new user
user := &User{Name: "Alice", Email: "alice@example.com"}
tx.Save(user) // ID auto-generated

// Immediately visible within transaction (before commit)
retrieved, _ := tx.Get(user.EntityID())
fmt.Println(retrieved.Name) // "Alice"

// Deleted entities return NotFoundError
tx.Delete(user)
_, err := tx.Get(user.EntityID())
// err is NotFoundError

tx.Commit(ctx)
```

## Optimistic Locking

Use `SaveWithCondition` for optimistic concurrency control:

```go
// Read entity with version
user, _ := userDAO.Get(ctx, 123)
currentVersion := user.Version

// Modify locally
user.Name = "Updated Name"
user.Version++

// Save only if version hasn't changed
tx, _ := userDAO.BeginTx(ctx)
defer tx.Rollback()

err := tx.SaveWithCondition(user, dao.Condition{
    Target:   dao.ConditionVersion,
    Operator: dao.OpEqual,
    Value:    currentVersion,
})
if dao.IsConditionFailed(err) {
    // Another process modified the entity
    return handleConflict()
}

return tx.Commit(ctx)
```

### Condition Types

| Target | Description |
|--------|-------------|
| `ConditionVersion` | Check entity's version field |
| `ConditionCreate` | Check entity's creation timestamp |
| `ConditionMod` | Check entity's modification timestamp |
| `ConditionValue` | Check entity's serialized value |

### Condition Operators

| Operator | Description |
|----------|-------------|
| `OpEqual` | Equality check (==) |
| `OpNotEqual` | Inequality check (!=) |
| `OpGreater` | Greater than (>) |
| `OpLess` | Less than (<) |
| `OpGreaterEqual` | Greater or equal (>=) |
| `OpLessEqual` | Less or equal (<=) |

## Helper Functions

### WithTx - Automatic Transaction Management

```go
err := dao.WithTx(ctx, userDAO, func(tx dao.DAOTransaction[*User]) error {
    tx.Save(user1)
    tx.Save(user2)
    return nil // Commit on success
})
// Automatically commits or rolls back based on return value
```

### BatchSave - Atomic Batch Insert

```go
users := []*User{user1, user2, user3}
err := dao.BatchSave(ctx, userDAO, users)
```

### BatchDelete - Atomic Batch Deletion

```go
users := []*User{user1, user2, user3}
err := dao.BatchDelete(ctx, userDAO, users)
```

### SaveWithOptimisticLock - Version Check Shortcut

```go
err := dao.SaveWithOptimisticLock(ctx, userDAO, user, expectedVersion)
if dao.IsConditionFailed(err) {
    // Handle concurrent modification
}
```

## Entity Interfaces

### Versioned Interface

For entities that track version numbers:

```go
type Versioned interface {
    GetVersion() int64
    SetVersion(version int64)
}

// Embed VersionedEntity for automatic implementation
type User struct {
    dao.VersionedEntity
    ID    uint64 `json:"id"`
    Name  string `json:"name"`
}
```

### Timestamped Interface

For entities that track creation/modification times:

```go
type Timestamped interface {
    GetCreatedAt() time.Time
    GetUpdatedAt() time.Time
}

// Embed TimestampedEntity for automatic implementation
type Session struct {
    dao.TimestampedEntity
    ID     uint64 `json:"id"`
    UserID uint64 `json:"user_id"`
}
```

## Error Handling

The DAO transaction package uses typed errors:

```go
// Check for specific errors
if dao.IsConditionFailed(err) {
    // Optimistic lock failed - concurrent modification
}

if dao.IsNotFound(err) {
    // Entity doesn't exist
}

if dao.IsTransactionClosed(err) {
    // Transaction already committed or rolled back
}
```

### Error Types

| Error | Description |
|-------|-------------|
| `TransactionClosedError` | Operation attempted on closed transaction |
| `ConditionFailedError` | Precondition not satisfied |
| `InvalidEntityError` | Entity is nil or invalid |
| `InvalidEntityIDError` | Entity ID is invalid (e.g., 0 for delete) |
| `InvalidConditionError` | Condition is malformed |
| `TransactionCommitError` | Commit failed |

## Use Cases

### User Registration with Profile

```go
err := dao.WithTx(ctx, userDAO, func(tx dao.DAOTransaction[*User]) error {
    user := &User{Name: "Alice", Email: "alice@example.com"}
    if err := tx.Save(user); err != nil {
        return err
    }

    profile := &Profile{UserID: user.EntityID(), Bio: "New user"}
    return profileTx.Save(profile)
})
```

### Inventory Update with Stock Check

```go
tx, _ := inventoryDAO.BeginTx(ctx)
defer tx.Rollback()

item, _ := tx.Get(itemID)
if item.Stock < requiredQuantity {
    return ErrInsufficientStock
}

// Conditional update - only if stock hasn't changed
item.Stock -= requiredQuantity
err := tx.SaveWithCondition(item, dao.Condition{
    Target:   dao.ConditionVersion,
    Operator: dao.OpEqual,
    Value:    item.Version,
})
if err != nil {
    return err
}

return tx.Commit(ctx)
```

## Thread Safety

- TransactionalDAO instances are safe for concurrent use
- Individual DAOTransaction instances should be used by a single goroutine
- Multiple transactions can run concurrently against the same DAO

## Limitations

- Transactions are scoped to a single entity type
- Cross-shard transactions require the 2PC coordinator (see [Distributed Transactions](distributed_transactions.md))
- No automatic retry on condition failures

## Related Documentation

- [GenericDAO](../dao/README.md) - Base DAO functionality
- [Distributed Transactions](distributed_transactions.md) - Cross-shard 2PC
- [Optimistic Locking](optimistic_locking.md) - Detailed concurrency patterns

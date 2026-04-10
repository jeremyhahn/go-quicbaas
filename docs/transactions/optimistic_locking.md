# Optimistic Locking

Version-based concurrency control for conflict detection and resolution in QuicBaaS transactions.

## Overview

Optimistic locking allows concurrent access to data while detecting conflicts at commit time. Instead of acquiring locks upfront, the system tracks version information and rejects updates when concurrent modifications are detected.

## Concepts

### Version-Based Concurrency

Each entity maintains a version number that increments on modification:

```
Time T1: Read user (version=5)
Time T2: Another process modifies user (version=6)
Time T3: Save with condition version==5 -> FAILS (actual=6)
```

### Compare-And-Swap Semantics

Operations succeed only if the current state matches expected conditions:

```go
// Only save if version hasn't changed
tx.SaveWithCondition(entity, Condition{
    Target:   ConditionVersion,
    Operator: OpEqual,
    Value:    expectedVersion,
})
```

## Implementation Patterns

### Basic Optimistic Lock

```go
// Read current state
user, err := userDAO.Get(ctx, userID)
if err != nil {
    return err
}
originalVersion := user.Version

// Modify locally
user.Name = "Updated Name"
user.Version++

// Save with version check
err = dao.SaveWithOptimisticLock(ctx, userDAO, user, originalVersion)
if dao.IsConditionFailed(err) {
    return ErrConcurrentModification
}
```

### Retry Pattern

```go
const maxRetries = 3

func updateUserWithRetry(ctx context.Context, userID uint64, updateFn func(*User)) error {
    for attempt := 0; attempt < maxRetries; attempt++ {
        // Read current state
        user, err := userDAO.Get(ctx, userID)
        if err != nil {
            return err
        }
        originalVersion := user.Version

        // Apply update
        updateFn(user)
        user.Version++

        // Attempt save
        err = dao.SaveWithOptimisticLock(ctx, userDAO, user, originalVersion)
        if err == nil {
            return nil // Success
        }

        if !dao.IsConditionFailed(err) {
            return err // Non-retryable error
        }

        // Conflict detected - retry
        log.Printf("Retry %d: concurrent modification detected", attempt+1)
    }

    return ErrMaxRetriesExceeded
}
```

### Conditional Save Patterns

#### Must Not Exist (Create Only)

```go
// Only create if entity doesn't exist
tx.SaveWithCondition(newUser, Condition{
    Target:   ConditionVersion,
    Operator: OpEqual,
    Value:    int64(0), // Version 0 means non-existent
})
```

#### Must Exist (Update Only)

```go
// Only update if entity exists
tx.SaveWithCondition(user, Condition{
    Target:   ConditionVersion,
    Operator: OpGreater,
    Value:    int64(0), // Version > 0 means exists
})
```

#### Timestamp-Based Condition

```go
// Only update if not modified since lastReadTime
tx.SaveWithCondition(user, Condition{
    Target:   ConditionMod,
    Operator: OpLessEqual,
    Value:    lastReadTime.UnixNano(),
})
```

## Entity Setup

### Using VersionedEntity Helper

```go
type User struct {
    dao.VersionedEntity // Embeds Version field
    ID    uint64        `json:"id"`
    Name  string        `json:"name"`
    Email string        `json:"email"`
}

func (u *User) EntityID() uint64      { return u.ID }
func (u *User) SetEntityID(id uint64) { u.ID = id }

// VersionedEntity provides:
// - GetVersion() int64
// - SetVersion(int64)
```

### Using TimestampedEntity Helper

```go
type Session struct {
    dao.TimestampedEntity // Embeds CreatedAt, UpdatedAt
    ID     uint64         `json:"id"`
    UserID uint64         `json:"user_id"`
    Token  string         `json:"token"`
}

// TimestampedEntity provides:
// - GetCreatedAt() time.Time
// - GetUpdatedAt() time.Time
```

### Custom Implementation

```go
type Account struct {
    ID       uint64 `json:"id"`
    Balance  int64  `json:"balance"`
    Version  int64  `json:"version"`
    Modified int64  `json:"modified"` // Unix nano timestamp
}

func (a *Account) GetVersion() int64 { return a.Version }
func (a *Account) SetVersion(v int64) { a.Version = v }
```

## Condition Types Reference

| Target | Interface Required | Description |
|--------|-------------------|-------------|
| `ConditionVersion` | `Versioned` | Entity's version counter |
| `ConditionCreate` | `Timestamped` | Entity's creation time |
| `ConditionMod` | `Timestamped` | Entity's last modification time |
| `ConditionValue` | None | Entity's serialized value |

## Error Handling

```go
err := tx.SaveWithCondition(entity, condition)

switch {
case err == nil:
    // Success
case dao.IsConditionFailed(err):
    // Concurrent modification - handle conflict
    cfErr := err.(dao.ConditionFailedError)
    log.Printf("Condition failed: %s", cfErr.Reason)
case dao.IsInvalidCondition(err):
    // Malformed condition
    icErr := err.(dao.InvalidConditionError)
    log.Printf("Invalid condition: %s", icErr.Reason)
default:
    // Other error
    return err
}
```

## Conflict Resolution Strategies

### Last-Write-Wins

Simple but may lose updates:

```go
// Ignore conflicts, always save
err := userDAO.Save(ctx, user)
```

### Merge Strategy

Combine concurrent changes:

```go
func mergeUsers(base, current, updated *User) *User {
    merged := &User{ID: base.ID, Version: current.Version + 1}

    // Merge each field
    if updated.Name != base.Name {
        merged.Name = updated.Name
    } else {
        merged.Name = current.Name
    }

    // ... merge other fields

    return merged
}
```

### Application-Level Resolution

Let the user decide:

```go
if dao.IsConditionFailed(err) {
    current, _ := userDAO.Get(ctx, user.ID)
    return NewConflictError(user, current)
}
```

## Performance Considerations

### Advantages

- No lock contention on reads
- High throughput for read-heavy workloads
- No deadlock possibility
- Simple implementation

### Trade-offs

- Retries consume resources under high contention
- Read-modify-write cycle can be expensive
- May require application-level conflict resolution

### Recommendations

1. **Short Transaction Windows** - Minimize time between read and write
2. **Batch Updates** - Group related changes to reduce retry probability
3. **Exponential Backoff** - Add jitter to retry delays
4. **Monitor Conflict Rate** - High rates indicate design issues

## Use Cases

### Counter Increment

```go
func incrementCounter(ctx context.Context, counterID uint64) error {
    for attempt := 0; attempt < 3; attempt++ {
        counter, _ := counterDAO.Get(ctx, counterID)
        originalVersion := counter.Version

        counter.Value++
        counter.Version++

        err := dao.SaveWithOptimisticLock(ctx, counterDAO, counter, originalVersion)
        if err == nil {
            return nil
        }
        if !dao.IsConditionFailed(err) {
            return err
        }
        // Retry on conflict
    }
    return ErrTooManyRetries
}
```

### Inventory Reservation

```go
func reserveInventory(ctx context.Context, itemID uint64, quantity int) error {
    item, _ := inventoryDAO.Get(ctx, itemID)

    if item.Available < quantity {
        return ErrInsufficientStock
    }

    originalVersion := item.Version
    item.Available -= quantity
    item.Reserved += quantity
    item.Version++

    return dao.SaveWithOptimisticLock(ctx, inventoryDAO, item, originalVersion)
}
```

### Document Edit

```go
func saveDocument(ctx context.Context, doc *Document, baseVersion int64) error {
    err := dao.SaveWithOptimisticLock(ctx, docDAO, doc, baseVersion)
    if dao.IsConditionFailed(err) {
        // Load current version for conflict resolution UI
        current, _ := docDAO.Get(ctx, doc.ID)
        return NewEditConflictError(doc, current)
    }
    return err
}
```

## Related Documentation

- [DAO Transactions](dao_transactions.md) - Transaction API
- [Distributed Transactions](distributed_transactions.md) - Cross-shard transactions
- [Conditional Batch Operations](../batch/conditional_batch.md) - Batch conditions

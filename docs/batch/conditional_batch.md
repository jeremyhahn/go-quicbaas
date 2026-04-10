# Conditional Batch Operations

Precondition-based batch operations with if-match, if-none-match, and version check semantics for atomic multi-key updates.

## Overview

Conditional batch operations extend the basic batch API with preconditions that must be satisfied before operations execute. This enables atomic check-and-set patterns across multiple keys while maintaining the atomicity guarantees of batch operations.

## Key Features

- **MustExist** - Operations fail if key doesn't exist
- **MustNotExist** - Operations fail if key already exists
- **ExpectedVersion** - Operations fail if version doesn't match
- **Atomic Evaluation** - All conditions checked before any operation executes
- **Detailed Results** - Per-key success/failure information

## Quick Start

```go
// Create condition evaluator
reader := batch.NewStorageKeyValueReader(store)
evaluator := batch.NewConditionEvaluator(reader)

// Define conditions
conditions := []*proto.BatchCondition{
    {
        EntityType:      "user",
        Id:              123,
        MustExist:       true, // User must exist
    },
    {
        EntityType:      "order",
        Id:              456,
        MustNotExist:    true, // Order must not exist (create only)
    },
    {
        EntityType:      "account",
        Id:              789,
        ExpectedVersion: 5, // Account must be at version 5
    },
}

// Evaluate conditions
result, err := evaluator.Evaluate(ctx, conditions)
if err != nil {
    return err
}

if !result.Succeeded {
    log.Printf("Conditions failed: %v", result.FailedKeys)
    return ErrPreconditionFailed
}

// Proceed with batch operation
```

## API Reference

### ConditionEvaluator

```go
// NewConditionEvaluator creates a new evaluator with the given storage reader.
func NewConditionEvaluator(reader KeyValueReader) *ConditionEvaluator

// Evaluate checks all conditions against current storage state.
func (e *ConditionEvaluator) Evaluate(
    ctx context.Context,
    conditions []*proto.BatchCondition,
) (*EvaluationResult, error)
```

### BatchCondition (Protocol Buffer)

```protobuf
message BatchCondition {
    string entity_type = 1;
    uint64 id = 2;
    bool must_exist = 3;
    bool must_not_exist = 4;
    uint64 expected_version = 5;
}
```

### EvaluationResult

```go
type EvaluationResult struct {
    Succeeded    bool              // True if all conditions passed
    FailedKeys   []string          // Keys that failed conditions
    FailedReason string            // First failure reason
    Results      []ConditionResult // Per-condition results
}

type ConditionResult struct {
    Key           string
    EntityType    string
    ID            uint64
    Passed        bool
    Reason        string
    ActualVersion uint64
}
```

## Condition Types

### MustExist

Requires the key to exist before the operation:

```go
// Only update existing users
condition := &proto.BatchCondition{
    EntityType: "user",
    Id:         123,
    MustExist:  true,
}
```

Use cases:
- Update operations that should fail on missing entities
- Reference validation before creating dependent records
- Ensuring consistency in multi-step workflows

### MustNotExist

Requires the key to not exist:

```go
// Only create new orders (prevent duplicates)
condition := &proto.BatchCondition{
    EntityType:   "order",
    Id:           456,
    MustNotExist: true,
}
```

Use cases:
- Create-only operations (idempotent inserts)
- Preventing duplicate entries
- Reserving unique identifiers

### ExpectedVersion

Requires a specific version number:

```go
// Only update if at expected version (optimistic locking)
condition := &proto.BatchCondition{
    EntityType:      "account",
    Id:              789,
    ExpectedVersion: 5,
}
```

Use cases:
- Optimistic concurrency control
- Detecting concurrent modifications
- Ensuring read-modify-write consistency

## Integration with Batch Operations

### gRPC API

```protobuf
message BatchPutRequest {
    repeated PutRequest requests = 1;
    ConsistencyLevel consistency = 2;
    uint64 shard_id = 3;
    repeated BatchCondition conditions = 4; // NEW: Conditions
}
```

### Server-Side Processing

```go
func (s *Server) BatchPut(ctx context.Context, req *proto.BatchPutRequest) (*proto.BatchPutResponse, error) {
    // Evaluate conditions first
    if len(req.Conditions) > 0 {
        reader := batch.NewStorageKeyValueReader(s.storage)
        evaluator := batch.NewConditionEvaluator(reader)

        result, err := evaluator.Evaluate(ctx, req.Conditions)
        if err != nil {
            return nil, err
        }

        if !result.Succeeded {
            return &proto.BatchPutResponse{
                Success:      false,
                FailedKeys:   result.FailedKeys,
                ErrorMessage: result.FailedReason,
            }, nil
        }
    }

    // Proceed with batch put
    return s.executeBatchPut(ctx, req)
}
```

## Use Cases

### User Registration with Email Uniqueness

```go
conditions := []*proto.BatchCondition{
    {
        EntityType:   "user",
        Id:           newUserID,
        MustNotExist: true, // User ID must be unique
    },
    {
        EntityType:   "email_index",
        Id:           emailHash,
        MustNotExist: true, // Email must be unique
    },
}

result, _ := evaluator.Evaluate(ctx, conditions)
if !result.Succeeded {
    if containsKey(result.FailedKeys, "email_index") {
        return ErrEmailAlreadyExists
    }
    return ErrUserIDExists
}

// Create user and email index atomically
batchPut(ctx, user, emailIndex)
```

### Inventory Transfer

```go
conditions := []*proto.BatchCondition{
    {
        EntityType:      "inventory",
        Id:              sourceWarehouseID,
        ExpectedVersion: sourceVersion, // Prevent concurrent modifications
    },
    {
        EntityType: "inventory",
        Id:         destWarehouseID,
        MustExist:  true, // Destination must exist
    },
}

result, _ := evaluator.Evaluate(ctx, conditions)
if !result.Succeeded {
    return ErrTransferPreconditionFailed
}

// Transfer inventory atomically
batchPut(ctx, updatedSource, updatedDest)
```

### Order Creation with Customer Validation

```go
conditions := []*proto.BatchCondition{
    {
        EntityType: "customer",
        Id:         customerID,
        MustExist:  true, // Customer must exist
    },
    {
        EntityType:   "order",
        Id:           orderID,
        MustNotExist: true, // Prevent duplicate orders
    },
}

result, _ := evaluator.Evaluate(ctx, conditions)
if !result.Succeeded {
    for _, r := range result.Results {
        if !r.Passed && r.EntityType == "customer" {
            return ErrCustomerNotFound
        }
    }
    return ErrOrderAlreadyExists
}

// Create order
batchPut(ctx, order, orderItems)
```

## Error Handling

```go
result, err := evaluator.Evaluate(ctx, conditions)

// Check for system errors
if err != nil {
    if errors.IsCanceled(err) {
        return ErrOperationCanceled
    }
    return err
}

// Check condition failures
if !result.Succeeded {
    for _, r := range result.Results {
        if !r.Passed {
            log.Printf("Condition failed for %s:%d - %s",
                r.EntityType, r.ID, r.Reason)
        }
    }
    return &PreconditionFailedError{
        FailedKeys: result.FailedKeys,
        Reason:     result.FailedReason,
    }
}
```

## Performance Considerations

### Condition Handler Dispatch

Conditions use O(1) map-based dispatch for efficient evaluation:

```go
// Internal implementation
conditionHandlers map[conditionType]conditionHandler{
    conditionTypeMustExist:     handleMustExist,
    conditionTypeMustNotExist:  handleMustNotExist,
    conditionTypeVersionCheck:  handleVersionCheck,
}
```

### Batch Evaluation

All conditions are evaluated in a single pass before any operations execute, minimizing storage reads.

### Context Cancellation

Evaluation respects context cancellation for long-running checks:

```go
for _, cond := range conditions {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
        // Evaluate condition
    }
}
```

## Best Practices

1. **Order Conditions** - Put most likely to fail conditions first
2. **Minimize Conditions** - Only include necessary preconditions
3. **Use Specific Errors** - Map failed keys to meaningful error messages
4. **Handle Timeouts** - Set appropriate context timeouts for evaluation
5. **Monitor Failures** - Track condition failure rates for capacity planning

## Related Documentation

- [Atomic Batch Operations](../architecture/batch-operations.md) - Basic batch operations
- [Streaming Batch Operations](streaming_batch.md) - Large dataset batching
- [Optimistic Locking](../transactions/optimistic_locking.md) - Version-based concurrency
- [Distributed Transactions](../transactions/distributed_transactions.md) - Cross-shard transactions

# Atomic Batch Operations

## Overview

QuicBaaS supports atomic batch operations for multiple PUT and DELETE operations. All operations succeed or all fail, preventing orphaned or inconsistent data.

## Why Batch Operations?

Without batch operations, related entities can end up in inconsistent states:

```go
// WITHOUT batch operations - dangerous!
client.Put(ctx, &pb.PutRequest{
    EntityType: "user",
    Id:         123,
    Data:       userJSON,
})

// If this fails, we have a user without an order!
client.Put(ctx, &pb.PutRequest{
    EntityType: "order",
    Id:         456,
    Data:       orderJSON,
})
```

### Solution: Atomic Batches

```go
// WITH batch operations - safe!
client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "user", Id: 123, Data: userJSON},
        {EntityType: "order", Id: 456, Data: orderJSON},
    },
})
// Both succeed or both fail - no orphaned data!
```

## How It Works

Atomicity guarantee: all operations wrapped into one Raft proposal, state machine applies all operations together, automatic rollback if any operation fails.

Operations are grouped per shard; each shard group commits as its own atomic proposal. Batches whose encoded size exceeds the Raft entry limit (8 MB) are split into size-bounded sub-batches (~4 MB each); atomicity then holds per sub-batch, and operation order is preserved within each shard.

### Architecture

```
Client Request
    |
gRPC BatchPut
    |
Single Raft Proposal (atomic)
    |
State Machine Update
    |-- Operation 1 (success)
    |-- Operation 2 (success)
    +-- Operation 3 (FAIL!) -> Rollback all
```

## API Reference

### BatchPut - Atomic Multi-Insert

Stores multiple entities atomically.

**Request:**
```protobuf
message BatchPutRequest {
  repeated PutRequest requests = 1;
  ConsistencyLevel consistency = 2;
  uint64 shard_id = 3;
  repeated BatchCondition conditions = 4;  // Optional preconditions
}
```

**Response:**
```protobuf
message BatchPutResponse {
  repeated PutResponse responses = 1;
  int64 timestamp = 2;
}
```

**Example:**
```go
resp, err := client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {
            EntityType: "user",
            Id:         123,
            Data:       []byte(`{"name":"Alice","email":"alice@example.com"}`),
        },
        {
            EntityType: "profile",
            Id:         123,
            Data:       []byte(`{"bio":"Software Engineer","avatar":"avatar.png"}`),
        },
    },
    Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
})
```

### BatchDelete - Atomic Multi-Delete

Deletes multiple entities atomically.

**Request:**
```protobuf
message BatchDeleteRequest {
  repeated DeleteRequest requests = 1;
  ConsistencyLevel consistency = 2;
  uint64 shard_id = 3;
}
```

**Response:**
```protobuf
message BatchDeleteResponse {
  repeated DeleteResponse responses = 1;
  int64 timestamp = 2;
}
```

**Example:**
```go
resp, err := client.BatchDelete(ctx, &pb.BatchDeleteRequest{
    Requests: []*pb.DeleteRequest{
        {EntityType: "user", Id: 123},
        {EntityType: "profile", Id: 123},
        {EntityType: "settings", Id: 123},
    },
})
// All related entities deleted atomically
```

### BatchGet - Multi-Retrieve (Non-Atomic)

Retrieves multiple entities in a single request. Note: not atomic, convenience method for efficient bulk reads.

**Request:**
```protobuf
message BatchGetRequest {
  repeated GetRequest requests = 1;
}
```

**Response:**
```protobuf
message BatchGetResponse {
  repeated GetResponse responses = 1;
}
```

**Example:**
```go
resp, err := client.BatchGet(ctx, &pb.BatchGetRequest{
    Requests: []*pb.GetRequest{
        {EntityType: "user", Id: 123},
        {EntityType: "user", Id: 456},
        {EntityType: "user", Id: 789},
    },
})
```

## Use Cases

### 1. User Registration with Profile

Create user and profile together:

```go
userJSON := `{"email":"user@example.com","username":"user123"}`
profileJSON := `{"bio":"New user","preferences":{}}`

client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "user", Id: 123, Data: []byte(userJSON)},
        {EntityType: "profile", Id: 123, Data: []byte(profileJSON)},
    },
})
```

### 2. Order with Line Items

Create order and all line items atomically:

```go
orderJSON := `{"customer_id":456,"total":99.99,"status":"pending"}`
item1JSON := `{"product_id":789,"quantity":2,"price":29.99}`
item2JSON := `{"product_id":790,"quantity":1,"price":39.99}`

client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "order", Id: 1001, Data: []byte(orderJSON)},
        {EntityType: "order_item", Id: 2001, Data: []byte(item1JSON)},
        {EntityType: "order_item", Id: 2002, Data: []byte(item2JSON)},
    },
})
```

### 3. Account Deletion with Cleanup

Delete user and all related data:

```go
client.BatchDelete(ctx, &pb.BatchDeleteRequest{
    Requests: []*pb.DeleteRequest{
        {EntityType: "user", Id: 123},
        {EntityType: "profile", Id: 123},
        {EntityType: "settings", Id: 123},
        {EntityType: "preferences", Id: 123},
    },
})
```

## Best Practices

DO:
- Use batches for related entities (User + Profile, Order + Items)
- Keep batches small (2-10 operations)
- Use same shard (all entities must be on same shard)
- Handle errors (check individual responses)

DON'T:
- Batch unrelated entities
- Create huge batches (>100 operations)
- Batch cross-shard operations
- Assume BatchGet is atomic

## Error Handling

### Batch Failure Behavior

If any operation fails: all previous operations rolled back, subsequent operations not executed, entire batch returns error.

Example:
```go
resp, err := client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "user", Id: 123, Data: validJSON},    // Would succeed alone
        {EntityType: "user", Id: 456, Data: invalidJSON},  // FAILS HERE
        {EntityType: "user", Id: 789, Data: validJSON},    // Never executed
    },
})
// Result: err != nil, NO entities created (rollback)
```

### Checking Individual Responses

```go
resp, err := client.BatchPut(ctx, req)
if err != nil {
    log.Errorf("Batch failed: %v", err)
    return
}

for i, r := range resp.Responses {
    if !r.Created {
        log.Warnf("Operation %d failed: %s", i, r.Message)
    }
}
```

## Performance Considerations

Advantages: single network round-trip, single Raft consensus, atomic guarantee with no extra cost.

Limitations: same-shard only, rollback overhead on failures, large batches consume more memory.

### Benchmarks

| Operation | Latency | Throughput |
|-----------|---------|------------|
| Single Put | 5ms | 200 ops/sec |
| Batch Put (10 items) | 6ms | 1,600 ops/sec |
| Single Delete | 4ms | 250 ops/sec |
| Batch Delete (10 items) | 5ms | 2,000 ops/sec |

*8x-10x improvement for batch operations*

## Implementation Details

### State Machine

The batch operation is implemented in the qrdb engine's state machine:

1. **Backup Phase**: Save current values of all keys being modified
2. **Apply Phase**: Execute each operation sequentially
3. **Success**: Commit all changes
4. **Failure**: Restore all keys from backup (rollback)

```go
operation: "batch"
operations: [
    {operation: "put", entity_type: "user", id: 123, data: "..."},
    {operation: "put", entity_type: "profile", id: 123, data: "..."},
]
```

### Rollback Strategy

**Data Rollback:** Complete - all key-value changes are reversed

**Index Rollback:** Best-effort - indexes may be inconsistent after failed batch (noted in code comment)

For production systems requiring perfect index consistency after rollback, additional index change tracking would be needed.

## Advanced Features

The batch system has been extended with additional capabilities:

### Conditional Batch Operations

Add preconditions that must be satisfied before operations execute:

```go
client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "user", Id: 123, Data: userData},
    },
    Conditions: []*pb.BatchCondition{
        {
            EntityType:   "user",
            Id:           123,
            MustNotExist: true, // Only create if doesn't exist
        },
    },
})
```

See [Conditional Batch Operations](../batch/conditional_batch.md) for complete documentation.

### Streaming Batch Operations

For large datasets that exceed practical single-request limits:

```go
stream, _ := client.StreamBatchPut(ctx)

// Send header
stream.Send(&proto.StreamBatchPutRequest{...})

// Stream chunks
for _, chunk := range chunks {
    stream.Send(chunk)
    progress, _ := stream.Recv()
    log.Printf("Progress: %d/%d", progress.OperationsReceived, total)
}

// Complete
stream.Send(completeRequest)
result, _ := stream.Recv()
```

See [Streaming Batch Operations](../batch/streaming_batch.md) for complete documentation.

### Cross-Shard Transactions

For atomic operations across multiple shards, use the 2PC coordinator:

```go
ops := map[uint64][]coordinator.TxnOp{
    shard1: {/* shard 1 operations */},
    shard2: {/* shard 2 operations */},
}

txnID, _ := coordinator.Begin(ctx, ops, nil)
coordinator.Prepare(ctx, txnID)
coordinator.Commit(ctx, txnID)
```

See [Distributed Transactions](../transactions/distributed_transactions.md) for complete documentation.

## Examples

See `examples/batch_operations/` for complete working examples.

## Related Documentation

- [Conditional Batch Operations](../batch/conditional_batch.md) - Precondition-based batches
- [Streaming Batch Operations](../batch/streaming_batch.md) - Large dataset streaming
- [Distributed Transactions](../transactions/distributed_transactions.md) - Cross-shard 2PC
- [DAO Transactions](../transactions/dao_transactions.md) - Entity-level transactions
- [Raft Consensus](./raft-consensus.md) - Underlying consensus mechanism

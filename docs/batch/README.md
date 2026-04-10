# QuicBaaS Batch Operations

Atomic batch operations for efficient multi-key writes with strong consistency.

## Overview

QuicBaaS provides multiple batch operation modes for different throughput and consistency requirements:

| Mode | Best For | Features |
|------|----------|----------|
| Atomic Batch | Related entities | All-or-nothing semantics |
| Conditional Batch | Precondition checks | if-match, if-none-match, version checks |
| Streaming Batch | Large datasets | Progress tracking, resume capability |

## Documentation

### Atomic Batch Operations

Basic atomic multi-key operations with automatic rollback.

- [Atomic Batch Operations](../architecture/batch-operations.md) - Full documentation

```go
client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "user", Id: 123, Data: userData},
        {EntityType: "profile", Id: 123, Data: profileData},
    },
})
```

### Conditional Batch Operations

Precondition-based batch operations with version checks and existence requirements.

- [Conditional Batch Operations](conditional_batch.md) - Full documentation

```go
client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: requests,
    Conditions: []*pb.BatchCondition{
        {EntityType: "user", Id: 123, MustNotExist: true},
        {EntityType: "order", Id: 456, ExpectedVersion: 5},
    },
})
```

### Streaming Batch Operations

gRPC streaming for large dataset operations with progress tracking.

- [Streaming Batch Operations](streaming_batch.md) - Full documentation

```go
stream, _ := client.StreamBatchPut(ctx)

stream.Send(header)
for _, chunk := range chunks {
    stream.Send(chunk)
    progress, _ := stream.Recv()
}
stream.Send(complete)
```

## Choosing the Right Batch Mode

| Scenario | Recommended |
|----------|-------------|
| Small batches (< 100 ops) | Atomic Batch |
| Create-only operations | Conditional Batch (MustNotExist) |
| Update with conflict detection | Conditional Batch (ExpectedVersion) |
| Large imports (> 1000 ops) | Streaming Batch |
| Resume after failure | Streaming Batch |

## Performance Comparison

| Mode | Latency | Throughput | Memory |
|------|---------|------------|--------|
| Single Put | 5ms | 200 ops/sec | Low |
| Atomic Batch (10) | 6ms | 1,600 ops/sec | Low |
| Streaming Batch | Variable | 10,000+ ops/sec | Moderate |

## Related Documentation

- [Distributed Transactions](../transactions/distributed_transactions.md)
- [DAO Transactions](../transactions/dao_transactions.md)
- [Transaction Architecture](../architecture/transactions.md)

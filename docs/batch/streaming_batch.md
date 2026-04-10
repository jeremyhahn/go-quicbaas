# Streaming Batch Operations

gRPC streaming API for high-throughput batch operations on large datasets with progress tracking and resume capability.

## Overview

Streaming batch operations enable efficient processing of large datasets that exceed practical limits for single-request batches. The system supports chunked uploads, real-time progress tracking, validation feedback, and session resume after interruption.

## Key Features

- **Chunked Streaming** - Process large datasets in manageable chunks
- **Progress Tracking** - Real-time feedback on operation status
- **Resume Capability** - Continue interrupted sessions from last checkpoint
- **Backpressure Handling** - Automatic flow control for overload prevention
- **Condition Validation** - Per-chunk validation with deferred commit checks
- **Prometheus Metrics** - Comprehensive observability

## Architecture

```
Client                              Server
  │                                   │
  │─────── StreamBatchHeader ────────>│  Create session
  │<──────── SessionID ───────────────│
  │                                   │
  │─────── StreamBatchChunk[0] ──────>│  Process chunk
  │<──────── Progress ────────────────│
  │                                   │
  │─────── StreamBatchChunk[1] ──────>│  Process chunk
  │<──────── Progress ────────────────│
  │                                   │
  │           ... N chunks ...         │
  │                                   │
  │─────── StreamBatchComplete ──────>│  Commit/Abort
  │<──────── StreamBatchResult ───────│
```

## Protocol Messages

### StreamBatchHeader

Initiates a streaming batch session:

```protobuf
message StreamBatchHeader {
    uint64 shard_id = 1;
    int64 total_operations = 2;       // Expected total operations
    ConsistencyLevel consistency = 3;
    repeated BatchCondition conditions = 4;
}
```

### StreamBatchChunk

Contains a batch of operations:

```protobuf
message StreamBatchChunk {
    int32 chunk_index = 1;            // For ordering/resume
    repeated PutRequest operations = 2;
}
```

### StreamBatchProgress

Real-time progress feedback:

```protobuf
message StreamBatchProgress {
    int64 operations_received = 1;
    int64 operations_validated = 2;
    int32 chunk_index = 3;
    repeated string validation_errors = 4;
}
```

### StreamBatchComplete

Signals end of stream:

```protobuf
message StreamBatchComplete {
    bool commit = 1;                  // true=commit, false=abort
}
```

### StreamBatchResult

Final result after commit/abort:

```protobuf
message StreamBatchResult {
    bool succeeded = 1;
    int64 operations_committed = 2;
    repeated string failed_keys = 3;
    string error = 4;
    int64 timestamp = 5;
}
```

## Quick Start

### Client-Side

```go
// Create streaming client
stream, err := client.StreamBatchPut(ctx)
if err != nil {
    return err
}

// Send header
header := &proto.StreamBatchPutRequest{
    Request: &proto.StreamBatchPutRequest_Header{
        Header: &proto.StreamBatchHeader{
            ShardId:         1,
            TotalOperations: int64(len(allOperations)),
            Consistency:     proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
        },
    },
}
stream.Send(header)

// Send chunks
chunkSize := 100
for i := 0; i < len(allOperations); i += chunkSize {
    end := min(i+chunkSize, len(allOperations))
    chunk := &proto.StreamBatchPutRequest{
        Request: &proto.StreamBatchPutRequest_Chunk{
            Chunk: &proto.StreamBatchChunk{
                ChunkIndex: int32(i / chunkSize),
                Operations: allOperations[i:end],
            },
        },
    }
    stream.Send(chunk)

    // Receive progress
    progress, _ := stream.Recv()
    log.Printf("Progress: %d/%d", progress.OperationsReceived, len(allOperations))
}

// Complete the stream
complete := &proto.StreamBatchPutRequest{
    Request: &proto.StreamBatchPutRequest_Complete{
        Complete: &proto.StreamBatchComplete{Commit: true},
    },
}
stream.Send(complete)

// Get final result
result, _ := stream.Recv()
log.Printf("Committed: %d operations", result.OperationsCommitted)
```

### Server-Side Processing

```go
processor := batch.NewStreamingBatchProcessor(aggregator, logger)

func (s *Server) StreamBatchPut(stream proto.QuicBaaS_StreamBatchPutServer) error {
    var session *streamingSession
    var sessionID string

    for {
        req, err := stream.Recv()
        if err == io.EOF {
            break
        }

        switch r := req.Request.(type) {
        case *proto.StreamBatchPutRequest_Header:
            sessionID = generateSessionID()
            session, err = processor.NewSession(ctx, sessionID, r.Header)

        case *proto.StreamBatchPutRequest_Chunk:
            progress, err := processor.AddChunk(ctx, sessionID, r.Chunk)
            stream.Send(progress)

        case *proto.StreamBatchPutRequest_Complete:
            result, err := processor.Complete(ctx, sessionID, r.Complete)
            stream.Send(result)
            return nil
        }
    }
    return nil
}
```

## Session Management

### Creating Sessions

```go
session, err := processor.NewSession(ctx, sessionID, header)
if err != nil {
    // Handle duplicate session, invalid header, etc.
}
```

### Session Lifecycle

```
NewSession -> AddChunk* -> Complete(commit=true)  -> COMMITTED
                        -> Complete(commit=false) -> ABORTED
                        -> Abort(reason)          -> ABORTED
                        -> Timeout                -> ABORTED
```

### Resume Capability

Sessions track the last successfully received chunk index:

```go
// Get last chunk index for resume
lastChunk, _ := processor.GetLastChunkIndex(sessionID)

// Client resumes from lastChunk + 1
for i := lastChunk + 1; i < totalChunks; i++ {
    stream.Send(chunks[i])
}
```

## Validation

### Per-Chunk Validation

Basic validation occurs immediately:

- Entity type required
- Data payload required
- Conditions recorded for commit-time check

### Commit-Time Validation

Full condition evaluation at commit:

- MustExist/MustNotExist checks
- Version comparisons
- Cross-reference validation

### Handling Validation Errors

```go
result, _ := processor.Complete(ctx, sessionID, complete)
if !result.Succeeded {
    for _, key := range result.FailedKeys {
        log.Printf("Failed: %s", key)
    }
    // Validation errors returned in FailedKeys
}
```

## Backpressure Handling

The processor implements backpressure to prevent memory exhaustion:

```go
// Progress channel has limited buffer
progressChan: make(chan *pb.StreamBatchProgress, 100)

// Non-blocking send with backpressure metric
select {
case session.progressChan <- progress:
    // Sent successfully
default:
    metrics.backpressureEvents.Inc()
    // Channel full, client should slow down
}
```

## Configuration

Default values:

```go
const (
    DefaultStreamTimeout       = 5 * time.Minute
    DefaultMaxChunksPerSession = 10000
    DefaultProgressInterval    = 100 // Progress every N operations
)
```

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_streaming_batch_sessions_total` | Counter | Sessions by status |
| `quicbaas_streaming_batch_chunks_received_total` | Counter | Chunks by shard |
| `quicbaas_streaming_batch_operations_total` | Counter | Operations by shard/status |
| `quicbaas_streaming_batch_session_duration_seconds` | Histogram | Session duration |
| `quicbaas_streaming_batch_chunk_size` | Histogram | Operations per chunk |
| `quicbaas_streaming_batch_validation_errors_total` | Counter | Validation failures |
| `quicbaas_streaming_batch_active_sessions` | Gauge | Active session count |
| `quicbaas_streaming_batch_backpressure_events_total` | Counter | Backpressure events |

## Use Cases

### Bulk Data Import

```go
// Import 1 million records in chunks
records := loadRecordsFromFile("data.csv")
chunkSize := 1000

stream, _ := client.StreamBatchPut(ctx)

// Send header
stream.Send(&proto.StreamBatchPutRequest{
    Request: &proto.StreamBatchPutRequest_Header{
        Header: &proto.StreamBatchHeader{
            ShardId:         1,
            TotalOperations: int64(len(records)),
        },
    },
})

// Stream chunks
for i := 0; i < len(records); i += chunkSize {
    chunk := buildChunk(records[i:min(i+chunkSize, len(records))], i/chunkSize)
    stream.Send(chunk)

    // Handle progress
    progress, _ := stream.Recv()
    printProgress(progress)
}

// Commit
stream.Send(completeRequest(true))
result, _ := stream.Recv()
```

### ETL Pipeline

```go
// Transform and load with validation
transformer := NewDataTransformer(schema)
validator := NewDataValidator(rules)

stream, _ := client.StreamBatchPut(ctx)
stream.Send(headerRequest)

chunkIndex := 0
for batch := range dataSource.Batches() {
    // Transform
    transformed := transformer.Transform(batch)

    // Pre-validate
    if errors := validator.Validate(transformed); len(errors) > 0 {
        log.Printf("Validation errors in chunk %d", chunkIndex)
        continue
    }

    // Stream
    stream.Send(buildChunk(transformed, chunkIndex))
    chunkIndex++
}

stream.Send(completeRequest(true))
```

### Incremental Backup Restore

```go
// Restore with resume capability
lastProcessed := getCheckpoint(sessionID)

stream, _ := client.StreamBatchPut(ctx)
stream.Send(headerRequest)

for i, record := range backupRecords {
    if i <= lastProcessed {
        continue // Skip already processed
    }

    stream.Send(buildChunk([]*Record{record}, i))

    progress, _ := stream.Recv()
    saveCheckpoint(sessionID, progress.ChunkIndex)
}

stream.Send(completeRequest(true))
```

## Error Handling

### Session Errors

```go
session, err := processor.NewSession(ctx, sessionID, header)
switch err.(type) {
case *StreamingBatchError:
    // System error (closed, invalid input)
case *StreamingSessionNotFoundError:
    // Session doesn't exist
}
```

### Chunk Errors

```go
progress, err := processor.AddChunk(ctx, sessionID, chunk)
switch err.(type) {
case *StreamingSessionAbortedError:
    // Session was aborted
case *StreamingChunkOutOfOrderError:
    // Resume from correct position
}
```

### Commit Errors

```go
result, err := processor.Complete(ctx, sessionID, complete)
if !result.Succeeded {
    // Check FailedKeys and Error for details
}
```

## Best Practices

1. **Chunk Size** - Use 100-1000 operations per chunk for optimal throughput
2. **Progress Handling** - Process progress responses to detect issues early
3. **Resume Support** - Persist chunk index for crash recovery
4. **Timeout Handling** - Set appropriate context timeouts for long operations
5. **Validation** - Pre-validate data client-side when possible
6. **Backpressure** - Slow down if receiving backpressure signals

## Limitations

- Single shard per session (use 2PC for cross-shard)
- Memory usage scales with pending operations
- Session timeout aborts incomplete streams

## Related Documentation

- [Atomic Batch Operations](../architecture/batch-operations.md) - Basic batch API
- [Conditional Batch Operations](conditional_batch.md) - Batch conditions
- [gRPC API Reference](../api/grpc.md) - Full gRPC documentation

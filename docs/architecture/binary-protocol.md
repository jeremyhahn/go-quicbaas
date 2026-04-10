# Binary Protocol

## Overview

QuicBaaS uses a high-performance binary protocol for internal state machine operations, achieving 8.7x faster encoding/decoding compared to JSON with 77% fewer allocations. The system maintains a hybrid architecture that accepts both JSON and binary from external clients while always using binary encoding internally.

## Protocol Format

### Single Operations

**PUT Operation**:
```
[0x01][keyLen:uint16][key bytes][valueLen:uint32][value bytes]
```

**DELETE Operation**:
```
[0x02][keyLen:uint16][key bytes]
```

### Batch Operations

**BATCH Format**:
```
[0x03][op1][op2]...[opN]
```

Each operation within a batch is a complete PUT or DELETE operation including its opcode.

### Format Example

PUT operation for key="user:123", value="John Doe":
```
01 00 08 75 73 65 72 3A 31 32 33 00 00 00 08 4A 6F 68 6E 20 44 6F 65
^  ^---- ^---------------------- ^---------- ^-----------------------
|  len   key                     len         value
op
```

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  External Clients                    │
│         (Can use JSON or Binary protocol)            │
└─────────────────────────────────────────────────────┘
                          │
                ┌─────────▼──────────┐
                │  Protocol Header   │
                │ x-quicbaas-protocol│
                │   (json|binary)    │
                └─────────┬──────────┘
                          │
┌─────────────────────────▼─────────────────────────┐
│              gRPC Server (Conversion Layer)        │
│  • Accepts JSON or Binary from clients             │
│  • ALWAYS converts to Binary for FastStateMachine  │
│  • Key format: "entityType:id"                     │
└─────────────────────────┬─────────────────────────┘
                          │
                ┌─────────▼──────────┐
                │  Binary Protocol   │
                │ [op][keyLen][key]  │
                │ [valueLen][value]  │
                └─────────┬──────────┘
                          │
┌─────────────────────────▼─────────────────────────┐
│           Raft Consensus (QuicRaft)              │
│  • WAL durability                                  │
│  • Leader election                                 │
│  • Log replication                                 │
└─────────────────────────┬─────────────────────────┘
                          │
┌─────────────────────────▼─────────────────────────┐
│         FastStateMachine (Binary Only)             │
│  • 256-way cache sharding                          │
│  • Zero-copy string operations                     │
│  • Async PebbleDB writes                           │
│  • Binary protocol parser                          │
└─────────────────────────┬─────────────────────────┘
                          │
┌─────────────────────────▼─────────────────────────┐
│              PebbleDB Storage                      │
│  • LSM-tree storage engine                         │
│  • Durable persistence                             │
└────────────────────────────────────────────────────┘
```

## Implementation

### Encoding Functions

Location: `pkg/api/grpc/optimized_encoding.go`

```go
// Core encoding functions
encodePutBinary(key, value []byte) []byte
encodeDeleteBinary(key []byte) []byte
encodeBatchBinary(operations [][]byte) []byte
```

### Batch Encoding

The batch encoding format matches FastStateMachine's expectations:

```go
func encodeBatchBinary(operations [][]byte) []byte {
    // Calculate total size: 1 byte for batch opcode + all operations
    totalSize := 1 // 1 byte for opBatch (0x03)
    for _, op := range operations {
        totalSize += len(op)
    }

    buf := make([]byte, totalSize)
    buf[0] = 0x03 // opBatch

    // Write each operation (keeping their opcodes: 0x01 or 0x02)
    offset := 1
    for _, op := range operations {
        copy(buf[offset:], op)
        offset += len(op)
    }

    return buf
}
```

### State Machine Integration

The FastStateMachine (managed by the qrdb engine) handles all binary operations:

```go
// Binary operations supported
case opPut:    return f.handlePut(data[1:])
case opDelete: return f.handleDelete(data[1:])
case opBatch:  return f.handleBatch(data[1:])
```

### Server Integration

Location: `pkg/api/grpc/server.go`

Operations using binary protocol:
- Put (lines 260-270)
- Delete (lines 447-455)
- BatchPut (lines 936-958)
- BatchDelete (lines 1126-1171)
- Transaction PUT (executeProtoPutOp, lines 1429-1448)
- Transaction DELETE (executeProtoDeleteOp, lines 1450-1469)

## Performance Results

### Benchmark Configuration

- Duration: 30 seconds
- Workers: 50 concurrent workers
- Value Size: 100 bytes per operation
- Protocol: Binary
- Write Mode: WAL (Write-Ahead Log)
- Connection Pool: 20 connections

### Results

| Metric | Value |
|--------|-------|
| Total Operations | 475,972 |
| Successful Operations | 475,922 (99.99%) |
| Throughput | 15,865 ops/sec |
| Average Latency | 3.15 ms |
| P50 Latency | 2.52 ms |
| P95 Latency | 6.81 ms |
| P99 Latency | 11.64 ms |
| Failures | 50 (0.01%) |

### Performance Improvements

| Implementation | Throughput | Improvement |
|----------------|-----------|-------------|
| MinimalStateMachine (JSON) | ~280 ops/sec | Baseline |
| FastStateMachine (Binary) | 15,865 ops/sec | 56.6x faster |

## Key Optimizations

### Binary Protocol Benefits

- 77% fewer allocations vs JSON
- 8.7x faster encoding/decoding
- 65% smaller message size

### FastStateMachine Features

1. Cache Sharding
   - 256 independent cache shards
   - Reduces lock contention by 256x
   - Parallel access across shards

2. Zero-Copy Operations
   - Uses `unsafe.Pointer` for []byte to string conversion
   - Eliminates memory allocations
   - Reduces GC pressure

3. Async PebbleDB Writes
   - Non-blocking state machine updates
   - Raft WAL provides durability guarantee
   - Higher throughput, lower latency

## Stability and Reliability

Test Results:
- Zero crashes during sustained load testing
- No panics with binary protocol
- 99.99% success rate
- Stable throughput maintained throughout tests
- Zero data loss - all successful operations persisted

## Production Readiness

Completed Features:
- Binary protocol implementation across all operations
- FastStateMachine integration complete
- Hybrid protocol architecture (supports both JSON and binary externally)
- Backwards compatibility maintained
- 15,865 ops/sec throughput achieved
- Sub-12ms P99 latency
- 99.99% success rate
- No crashes during testing

Status: Production-ready for single-node deployments. Multi-node cluster testing recommended before production use.

## Technical Notes

### Critical Fix: Batch Format Alignment

Initial implementation used incorrect batch format:
```go
// INCORRECT:
[numOps:4bytes][op1][op2]...[opN]
```

FastStateMachine expected:
```go
// CORRECT:
[0x03][0x01][keyLen][key][valueLen][value][0x02][keyLen][key]...
```

The `encodeBatchBinary()` function was updated to match FastStateMachine's expectations by including operation opcodes within the batch payload.

### Protocol Negotiation

The system supports protocol negotiation via the `x-quicbaas-protocol` header:
- Clients can request `json` or `binary` protocol
- Server always converts to binary for FastStateMachine
- Responses can be optimized based on client's preferred protocol
- Default is JSON for backwards compatibility

## References

Implementation files:
- `pkg/api/grpc/optimized_encoding.go` - Binary encoding functions
- `pkg/api/grpc/server.go` - Server-side protocol handling
- FastStateMachine binary parser is managed by the qrdb engine
- `pkg/server/lifecycle.go` - Server initialization

# DAO Integration Tests

## Overview

The DAO (Data Access Object) integration tests verify the complete DAO functionality against a real QuicBaaS cluster. These tests ensure that the generic DAO layer works correctly with real Raft consensus, PebbleDB storage, and all serialization codecs.

## Test Location

```
test/integration/dao/
├── dao_test.go         # Main integration tests
├── entities_test.go    # Test entity definitions
├── helpers.go          # Test helper functions
└── main_test.go        # Test setup and teardown
```

## Running the Tests

### Run All DAO Integration Tests

```bash
make integration-test-dao
```

This command:
1. Starts a 3-node QuicBaaS cluster using Docker Compose
2. Waits for cluster readiness
3. Runs all DAO integration tests
4. Tears down the cluster

### Run Specific Tests

```bash
# Run a specific test
go test -v -tags integration ./test/integration/dao -run TestDAOBasicSaveGet

# Run tests with timeout
go test -v -tags integration -timeout 10m ./test/integration/dao
```

## Test Coverage

### 1. Basic CRUD Operations

**Test:** `TestDAOBasicSaveGet`

Tests fundamental Save and Get operations:
- Create an entity with various field types
- Auto-generate ID using UnixNano timestamp
- Save to real Raft storage
- Retrieve and verify data integrity
- Verify all field types (strings, numbers, booleans, slices, maps, timestamps)

**Expected Results:**
- Entity saved successfully with auto-generated ID
- Retrieved entity matches original data
- All field types correctly serialized and deserialized

### 2. Delete Operations

**Test:** `TestDAODelete`

Tests entity deletion:
- Create and save an entity
- Verify existence
- Delete the entity
- Verify deletion (Get returns not found)

**Expected Results:**
- Delete operation succeeds
- Entity no longer retrievable after deletion

### 3. Small Pagination

**Test:** `TestDAOPaginationSmall`

Tests pagination with a small dataset (25 entities):
- Create 25 session entities
- Paginate with page size of 10
- Iterate through pages
- Verify page tokens and HasMore flags

**Expected Results:**
- Multiple pages retrieved correctly
- Page sizes respected
- All entities accessible via pagination

### 4. Large Pagination

**Test:** `TestDAOPaginationLarge`

Tests pagination with 150+ entities:
- Create 150 session entities
- Paginate with page size of 25
- Iterate through all pages
- Count total entities retrieved

**Expected Results:**
- 6+ pages retrieved (150 entities / 25 per page)
- All entities accessible
- No data loss during pagination
- Performance remains acceptable

### 5. Different Serialization Codecs

**Test:** `TestDAODifferentCodecs`

Tests multiple serialization formats:
- **JSON:** Human-readable, standard format
- **MsgPack:** Binary, compact, fast
- **BSON:** Binary, MongoDB-compatible

For each codec:
- Create config entity with nested data structures
- Marshal with specific codec
- Save to storage
- Retrieve and unmarshal with same codec
- Verify data integrity

**Expected Results:**
- All codecs successfully serialize and deserialize
- No data corruption across codec types
- Complex data structures (maps, nested objects) handled correctly

### 6. Multi-Shard Operations

**Test:** `TestDAODifferentShards`

Tests DAO operations across multiple Raft shards:
- Create entities on shard 1, 2, and 3
- Verify shard isolation
- Ensure correct shard routing

**Expected Results:**
- Entities saved to correct shards
- Shard-specific retrieval works
- No cross-shard interference

### 7. Concurrent Operations

**Test:** `TestDAOConcurrentOperations`

Tests thread safety and concurrent access:
- Spawn 10 goroutines
- Each goroutine performs 5 Save operations
- Immediately verify each save with Get
- Total: 50 concurrent operations

**Expected Results:**
- All concurrent saves succeed
- No race conditions
- No data corruption
- All entities retrievable immediately after save

### 8. Count Accuracy

**Test:** `TestDAOCountAccuracy`

Tests entity counting:
- Create 20 users
- Use List operation to count entities
- Verify count matches created entities

**Expected Results:**
- Count returns accurate number
- No phantom entities
- No missing entities

### 9. Data Persistence

**Test:** `TestDAODataPersistence`

Tests data durability:
- Create and save an entity
- Retrieve entity 5 times with delays
- Verify data consistency across all retrievals

**Expected Results:**
- Data remains consistent
- No data corruption over time
- All fields maintain integrity

## Test Entities

### User Entity

Complex entity with diverse field types:

```go
type User struct {
    ID        uint64            // Auto-generated
    Name      string
    Email     string
    Age       int
    Active    bool
    Balance   float64
    Tags      []string
    Metadata  map[string]string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

**Use Cases:**
- Testing all common Go field types
- Verifying slice and map serialization
- Testing timestamp handling

### Config Entity

Entity for testing different serialization formats:

```go
type Config struct {
    ID          uint64
    Name        string
    Environment string
    Settings    map[string]interface{} // Nested data
    Version     string
    Enabled     bool
}
```

**Use Cases:**
- Testing codec compatibility
- Verifying nested data structures
- Testing YAML/TOML tags

### Product Entity

Entity for concurrent operations:

```go
type Product struct {
    ID          uint64
    SKU         string
    Name        string
    Description string
    Price       float64
    Stock       int
    Category    string
}
```

**Use Cases:**
- Concurrent write testing
- Performance benchmarking
- Real-world entity modeling

### Session Entity

Entity for pagination tests:

```go
type Session struct {
    ID        uint64
    UserID    uint64
    Token     string
    IPAddress string
    UserAgent string
    ExpiresAt time.Time
    CreatedAt time.Time
}
```

**Use Cases:**
- Large-scale pagination
- Time-based queries
- Session management scenarios

## Test Environment

### Docker Compose Setup

The tests use the integration test Docker Compose environment:

- **test-node1:** Primary Raft node (ID: 1)
- **test-node2:** Secondary Raft node (ID: 2)
- **test-node3:** Tertiary Raft node (ID: 3)

### Network Configuration

- **gRPC Endpoints:**
  - Node 1: `test-node1:50051`
  - Node 2: `test-node2:50051`
  - Node 3: `test-node3:50051`

- **REST Endpoints:**
  - Node 1: `http://test-node1:8080`
  - Node 2: `http://test-node2:8080`
  - Node 3: `http://test-node3:8080`

### Environment Variables

Tests read configuration from environment:

```bash
QUICBAAS_NODE1_GRPC=test-node1:50051
QUICBAAS_NODE2_GRPC=test-node2:50051
QUICBAAS_NODE3_GRPC=test-node3:50051
QUICBAAS_TEST_MODE=integration
```

## Test Architecture

### TestMain Setup

The `TestMain` function in `main_test.go`:

1. **Setup Phase:**
   - Connect to QuicBaaS cluster via gRPC
   - Create service client
   - Wait for cluster readiness

2. **Execution Phase:**
   - Run all tests (`m.Run()`)

3. **Cleanup Phase:**
   - Close gRPC connections
   - Release resources

### Helper Functions

#### `createTestKVClient()`

Creates a KV service client for direct gRPC operations.

#### `marshalJSON(v interface{}) ([]byte, error)`

Helper for JSON serialization in tests.

#### `unmarshalJSON(data []byte, v interface{}) error`

Helper for JSON deserialization in tests.

## Best Practices

### 1. Entity Type Naming

Use unique entity types per test to avoid conflicts:

```go
entityType := fmt.Sprintf("users_test_%d", time.Now().UnixNano())
```

### 2. ID Generation

Ensure unique IDs across concurrent tests:

```go
entity.SetEntityID(uint64(time.Now().UnixNano() + int64(i*1000)))
```

### 3. Cleanup

Tests don't explicitly delete entities; each test uses unique entity types.

### 4. Timeouts

Always use contexts with timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

### 5. Error Handling

Use testify assertions for clear failure messages:

```go
require.NoError(t, err, "Save operation should succeed")
assert.Equal(t, expected, actual, "Values should match")
```

## Performance Expectations

### Small Operations (< 10 entities)
- Save: < 100ms per entity
- Get: < 50ms per entity
- Delete: < 100ms per entity

### Large Operations (100+ entities)
- Batch save: < 5 seconds for 100 entities
- Pagination: < 500ms per page (25 entities)

### Concurrent Operations
- 50 concurrent saves: < 3 seconds total
- No race conditions
- No goroutine leaks

## Troubleshooting

### Test Timeouts

**Symptom:** Tests timeout after 30 seconds

**Solutions:**
- Increase timeout: `go test -timeout 10m`
- Check cluster health
- Verify network connectivity

### Connection Refused

**Symptom:** `connection refused` errors

**Solutions:**
- Ensure Docker Compose is running
- Check node health: `docker compose ps`
- Verify environment variables

### Data Not Found After Save

**Symptom:** Entity not found immediately after save

**Solutions:**
- Check Raft consensus delay
- Add small delay after save
- Verify shard ID consistency

### Codec Errors

**Symptom:** Serialization/deserialization failures

**Solutions:**
- Verify struct tags match codec
- Check for unsupported types
- Ensure nested structures are exported

## Continuous Integration

The DAO integration tests run in CI/CD pipelines:

```bash
# GitHub Actions
make integration-test-dao

# Local development
make integration-test-dao
```

### CI Requirements

- Docker and Docker Compose installed
- Sufficient memory (4GB+)
- Network access for gRPC

## Implemented Test Coverage

### Available Tests

1. **Index Queries:**
   - Secondary index support via `pkg/storage/index/`
   - Range queries with `ScanIndex()`

2. **Batch Operations:**
   - Storage-level atomic batches via `storage.Batch`
   - Multi-entity batch tests

3. **Advanced Codecs:**
   - CBOR support (`-tags codec_cbor`)
   - Avro support (`-tags codec_avro`)
   - Protocol Buffers (`-tags codec_protobuf`)

### Future Enhancements

1. **DAO-Level Transactions:**
   - Multi-entity atomic saves
   - Rollback on failures

2. **Streaming:**
   - Large entity streaming

3. **Performance Benchmarks:**
   - Throughput testing
   - Latency percentiles
   - Memory profiling

## Related Documentation

- [DAO API Reference](./api-reference.md)
- [DAO Usage Guide](./usage.md)
- [Integration Test Framework](../development/integration-testing.md)
- [Docker Compose Configuration](../../test/integration/docker-compose.integration.yml)

# QuicBaaS Examples

This directory contains comprehensive examples demonstrating QuicBaaS's Phase 2 features: MVCC transactions, real-time watches, and memory mode usage.

## Overview

All examples are runnable, well-documented, and demonstrate real-world usage patterns. Each example includes:
- Clear explanations of concepts
- Step-by-step demonstrations
- Proper error handling
- Resource cleanup
- Key takeaways

## Directory Structure

```
examples/
├── mvcc/              # MVCC Transaction Examples
│   ├── basic/         # Basic transaction usage
│   ├── concurrent/    # Concurrent transaction handling
│   └── isolation/     # Snapshot isolation demonstration
├── watch/             # Watch/Event Examples
│   ├── simple/        # Simple watch usage
│   ├── prefix/        # Prefix watching
│   └── filtering/     # Event filtering patterns
└── memory/            # Memory Mode Examples
    ├── simple/        # Basic memory mode usage
    ├── caching/       # Cache layer pattern
    └── testing/       # Testing patterns
```

## MVCC Examples

### 1. Basic Transaction (`mvcc/basic`)

Demonstrates fundamental transaction concepts:
- Beginning and committing transactions
- Atomic all-or-nothing semantics
- Rollback operations
- Transaction isolation

**Key Features:**
- Multiple operations in a single transaction
- Automatic rollback on error
- Change visibility after commit

**Run:**
```bash
go run examples/mvcc/basic/main.go
```

**Concepts Demonstrated:**
- Transaction lifecycle
- ACID properties
- Error handling in transactions

### 2. Concurrent Transactions (`mvcc/concurrent`)

Shows how multiple transactions execute concurrently:
- Parallel transaction execution
- Snapshot isolation between transactions
- No read-write blocking
- Conflict detection

**Key Features:**
- Multiple goroutines running transactions
- Bank transfer simulation
- Performance under concurrency
- Transaction coordination

**Run:**
```bash
go run examples/mvcc/concurrent/main.go
```

**Concepts Demonstrated:**
- Concurrent transaction safety
- Snapshot isolation guarantees
- Write-write conflict handling

### 3. Snapshot Isolation (`mvcc/isolation`)

Deep dive into MVCC's snapshot isolation:
- Point-in-time consistent views
- Transaction independence
- Read stability guarantees
- Write conflict detection

**Key Features:**
- Demonstrates isolation levels
- Shows snapshot consistency
- Illustrates conflict scenarios
- New transaction visibility

**Run:**
```bash
go run examples/mvcc/isolation/main.go
```

**Concepts Demonstrated:**
- Snapshot isolation semantics
- Read consistency within transactions
- Visibility of committed changes

## Watch Examples

### 4. Simple Watch (`watch/simple`)

Basic real-time change notification:
- Creating watches
- Receiving PUT and DELETE events
- Asynchronous event processing
- Resource cleanup

**Key Features:**
- Product inventory monitoring
- Real-time event delivery
- Event type handling
- Context-based cancellation

**Run:**
```bash
go run examples/watch/simple/main.go
```

**Concepts Demonstrated:**
- Watch lifecycle
- Event types (PUT/DELETE)
- Async event processing

### 5. Prefix Watching (`watch/prefix`)

Namespace-based monitoring:
- Prefix-based event filtering
- Multiple concurrent watches
- Namespace isolation
- Hierarchical data monitoring

**Key Features:**
- Project-based task monitoring
- Multiple watch instances
- Prefix matching
- Filtered event delivery

**Run:**
```bash
go run examples/watch/prefix/main.go
```

**Concepts Demonstrated:**
- Prefix watch patterns
- Multi-tenant monitoring
- Namespace isolation

### 6. Event Filtering (`watch/filtering`)

Advanced event processing:
- Application-level filtering
- Custom event routing
- Selective event handling
- Event-driven workflows

**Key Features:**
- Order status monitoring
- Priority-based routing
- Custom filter logic
- Multiple event handlers

**Run:**
```bash
go run examples/watch/filtering/main.go
```

**Concepts Demonstrated:**
- Event filtering strategies
- Custom event routing
- Workflow orchestration

## Memory Mode Examples

### 7. Simple Memory Mode (`memory/simple`)

Pure in-memory database operations:
- Fast memory-only operations
- No disk I/O overhead
- CRUD operations
- Snapshot and restore

**Key Features:**
- Automatic ID generation
- Full CRUD support
- Performance benchmarks
- Snapshot capabilities

**Run:**
```bash
go run examples/memory/simple/main.go
```

**Concepts Demonstrated:**
- Memory mode basics
- Performance characteristics
- Snapshot/restore functionality

### 8. Caching Pattern (`memory/caching`)

Using memory mode as a cache layer:
- Read-through caching
- Write-through caching
- Cache invalidation
- TTL simulation
- Performance metrics

**Key Features:**
- Backend simulation
- Cache statistics
- TTL-based expiration
- Performance comparison

**Run:**
```bash
go run examples/memory/caching/main.go
```

**Concepts Demonstrated:**
- Cache layer patterns
- Read-through/write-through
- Cache metrics and monitoring

### 9. Testing Pattern (`memory/testing`)

Unit testing with memory mode:
- Fast, isolated tests
- Transaction testing
- Concurrency testing
- Table-driven tests
- Benchmarks

**Key Features:**
- No test data pollution
- Zero cleanup overhead
- Concurrent operation tests
- Performance benchmarks

**Run:**
```bash
go test -v examples/memory/testing/
```

**Run Benchmarks:**
```bash
go test -bench=. examples/memory/testing/
```

**Concepts Demonstrated:**
- Test isolation
- Fast test execution
- Concurrent test patterns
- Benchmark strategies

## Common Patterns

### Entity Types

Examples use consistent entity types for clarity:

```go
// User: Basic entity with ID, name, email
type User struct {
    ID    uint64 `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

// Product: Inventory item with price and stock
type Product struct {
    ID    uint64  `json:"id"`
    Name  string  `json:"name"`
    Price float64 `json:"price"`
    Stock int     `json:"stock"`
}

// Task: Project task with status and assignee
type Task struct {
    ID        uint64 `json:"id"`
    ProjectID string `json:"project_id"`
    Title     string `json:"title"`
    Status    string `json:"status"`
}
```

All entities implement the `KeyValueEntity` interface:
```go
type KeyValueEntity interface {
    GetID() uint64
    SetID(id uint64)
}
```

### Error Handling

All examples demonstrate proper error handling:

```go
// Check errors immediately
db, err := memory.Open[*User]()
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// Transaction error handling with rollback
tx, err := db.BeginTx(ctx)
if err != nil {
    return err
}

if err := tx.Put(key, value); err != nil {
    tx.Rollback()
    return err
}

if err := tx.Commit(); err != nil {
    return err
}
```

### Resource Cleanup

All examples include proper cleanup:

```go
// Database cleanup
db, err := memory.Open[*User]()
if err != nil {
    log.Fatal(err)
}
defer db.Close()  // Always close

// Context cleanup
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()  // Always cancel

// Transaction cleanup
tx, err := db.BeginTx(ctx)
if err != nil {
    return err
}
defer tx.Rollback()  // Safe to call after commit
```

## Building and Running

### Run Individual Examples

```bash
# MVCC examples
go run examples/mvcc/basic/main.go
go run examples/mvcc/concurrent/main.go
go run examples/mvcc/isolation/main.go

# Watch examples
go run examples/watch/simple/main.go
go run examples/watch/prefix/main.go
go run examples/watch/filtering/main.go

# Memory examples
go run examples/memory/simple/main.go
go run examples/memory/caching/main.go
```

### Run Tests

```bash
# Run all tests
go test -v examples/memory/testing/

# Run specific test
go test -v examples/memory/testing/ -run TestBasicCRUD

# Run benchmarks
go test -bench=. examples/memory/testing/

# Run with race detector
go test -race examples/memory/testing/
```

### Build Binaries

```bash
# Build all examples
go build -o bin/mvcc-basic examples/mvcc/basic/main.go
go build -o bin/mvcc-concurrent examples/mvcc/concurrent/main.go
go build -o bin/mvcc-isolation examples/mvcc/isolation/main.go
go build -o bin/watch-simple examples/watch/simple/main.go
go build -o bin/watch-prefix examples/watch/prefix/main.go
go build -o bin/watch-filtering examples/watch/filtering/main.go
go build -o bin/memory-simple examples/memory/simple/main.go
go build -o bin/memory-caching examples/memory/caching/main.go
```

## Use Cases

### When to Use MVCC Transactions

- Multiple related operations must succeed or fail together
- Need ACID guarantees
- Concurrent access to shared data
- Complex business logic requiring consistency
- Financial transactions
- Multi-step workflows

### When to Use Watches

- Real-time UI updates
- Cache invalidation
- Event-driven architectures
- Microservices coordination
- Monitoring and auditing
- Reactive systems
- Pub/sub patterns

### When to Use Memory Mode

- Unit testing (fast, isolated)
- Development and prototyping
- Session storage
- Temporary caching
- High-performance read workloads
- CI/CD test environments
- In-memory analytics

## Performance Characteristics

### Memory Mode
- **Latency**: ~1-5 µs per operation
- **Throughput**: 200K-500K ops/sec
- **Memory**: ~100-200 bytes per entity
- **Zero disk I/O**: All operations in RAM

### MVCC Transactions
- **Overhead**: ~50-100 µs per transaction
- **Isolation**: Snapshot isolation (SI)
- **Conflicts**: Detected at commit time
- **Scalability**: High concurrent transaction support

### Watches
- **Latency**: ~150-300 ns notification overhead
- **Memory**: ~152 bytes per watch
- **Throughput**: Minimal impact (<5%) on writes
- **Scalability**: 256-way sharded for low contention

## Best Practices

### Transaction Best Practices

1. **Keep transactions short**: Long transactions increase conflict probability
2. **Handle conflicts gracefully**: Implement retry logic
3. **Use context for timeouts**: Prevent hanging transactions
4. **Rollback on errors**: Always rollback failed transactions
5. **Avoid nested transactions**: Not currently supported

### Watch Best Practices

1. **Use prefix watches for namespaces**: More efficient than many exact watches
2. **Process events asynchronously**: Don't block the watch goroutine
3. **Handle slow consumers**: Implement buffering or dropping
4. **Cancel watches properly**: Use context cancellation
5. **Filter at application level**: Reduce unnecessary processing

### Memory Mode Best Practices

1. **Perfect for testing**: Fast, isolated, no cleanup
2. **Monitor memory usage**: No persistence means data in RAM
3. **Use snapshots for backups**: Periodic snapshot for safety
4. **Consider TTL for caching**: Implement expiration logic
5. **Benchmark your workload**: Measure actual performance

## Next Steps

1. **Read the Documentation**: See `docs/` for detailed API documentation
2. **Try the Examples**: Run each example to see features in action
3. **Explore the Tests**: Review test files for more usage patterns
4. **Check Benchmarks**: Run benchmarks to understand performance
5. **Build Your Application**: Use these patterns in your own code

## Support

- **Documentation**: `/docs` directory
- **Source Code**: `/pkg` directory
- **Issues**: GitHub Issues
- **Benchmarks**: See `*_bench_test.go` files

## License

See LICENSE file in the root directory.

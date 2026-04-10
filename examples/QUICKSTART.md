# QuicBaaS Phase 2 Examples - Quick Start Guide

This guide helps you quickly get started with QuicBaaS's Phase 2 features through runnable examples.

## Getting Started

### Prerequisites

```bash
# Ensure you have Go 1.21+ installed
go version

# Clone or navigate to the QuicBaaS repository
cd go-quicbaas
```

## 30-Second Quick Start

### Run Your First Example

```bash
# Run the simplest example - memory mode
go run examples/memory/simple/main.go
```

You'll see output demonstrating:
- Creating a database in memory
- CRUD operations (Create, Read, Update, Delete)
- Automatic ID generation
- Performance benchmarks
- ~900K ops/sec throughput

## SDK Client Examples

The SDK client examples demonstrate connecting to a running QuicBaaS server using the Go SDK with multiprotocol support (gRPC, REST, QUIC).

### Prerequisites

A running QuicBaaS server is required for these examples:

```bash
# Start QuicBaaS server (default: localhost:50051)
./quicbaasd
```

### Key Management Service (KMS)

```bash
# Run with gRPC (default)
go run examples/client/kms/main.go

# Run with REST transport
go run examples/client/kms/main.go -transport rest -address http://localhost:8080

# Run with QUIC transport
go run examples/client/kms/main.go -transport quic -address localhost:50052
```

**Demonstrates**: Key generation, rotation, encryption/decryption, signing/verification.

### Secrets Management

```bash
# Run with gRPC (default)
go run examples/client/secrets/main.go

# Run with REST transport
go run examples/client/secrets/main.go -transport rest -address http://localhost:8080
```

**Demonstrates**: Storing secrets, versioning, retrieval, listing, deletion.

### Identity and Access Management (IAM)

```bash
# Run with gRPC (default)
go run examples/client/iam/main.go

# Run with REST transport
go run examples/client/iam/main.go -transport rest -address http://localhost:8080
```

**Demonstrates**: Multi-tenant architecture, users, groups, roles, permissions.

For detailed CLI documentation, see `/docs/cli/`.

## 5-Minute Feature Tour

### 1. MVCC Transactions (2 minutes)

```bash
# Basic transaction usage
go run examples/mvcc/basic/main.go
```

**What you'll learn:**
- Beginning and committing transactions
- Rollback on error
- Atomic all-or-nothing semantics

### 2. Real-time Watches (2 minutes)

```bash
# Watch for real-time changes
go run examples/watch/simple/main.go
```

**What you'll learn:**
- Creating watches for change notifications
- Handling PUT and DELETE events
- Asynchronous event processing

### 3. Memory Mode for Testing (1 minute)

```bash
# Run fast unit tests
go test -v ./examples/memory/testing/ -run TestBasicCRUD
```

**What you'll learn:**
- Fast, isolated unit tests
- No database setup/teardown
- Zero cleanup overhead

## Feature-by-Feature Guide

### MVCC Transactions

#### When to Use
- Multiple operations must succeed/fail together
- Need ACID guarantees
- Concurrent access to shared data
- Financial transactions

#### Quick Examples

**Basic Transaction:**
```bash
go run examples/mvcc/basic/main.go
```
Duration: ~3 seconds | Lines of output: ~50

**Concurrent Transactions:**
```bash
go run examples/mvcc/concurrent/main.go
```
Duration: ~8 seconds | Lines of output: ~80

**Snapshot Isolation:**
```bash
go run examples/mvcc/isolation/main.go
```
Duration: ~5 seconds | Lines of output: ~70

### Real-time Watches

#### When to Use
- Real-time UI updates
- Cache invalidation
- Event-driven architectures
- Monitoring and auditing

#### Quick Examples

**Simple Watch:**
```bash
go run examples/watch/simple/main.go
```
Duration: ~5 seconds | Lines of output: ~30

**Prefix Watch:**
```bash
go run examples/watch/prefix/main.go
```
Duration: ~10 seconds | Lines of output: ~50

**Event Filtering:**
```bash
go run examples/watch/filtering/main.go
```
Duration: ~10 seconds | Lines of output: ~60

### Memory Mode

#### When to Use
- Unit testing (fast, isolated)
- Development and prototyping
- Session storage
- High-performance caching

#### Quick Examples

**Simple Memory Mode:**
```bash
go run examples/memory/simple/main.go
```
Duration: ~2 seconds | Lines of output: ~60

**Caching Pattern:**
```bash
go run examples/memory/caching/main.go
```
Duration: ~15 seconds | Lines of output: ~100

**Testing Patterns:**
```bash
# Run all tests
go test -v ./examples/memory/testing/

# Run specific test
go test -v ./examples/memory/testing/ -run TestBasicCRUD

# Run benchmarks
go test -bench=. ./examples/memory/testing/
```

## Common Tasks

### Build All Examples

```bash
# Create bin directory
mkdir -p bin

# Build MVCC examples
go build -o bin/mvcc-basic examples/mvcc/basic/main.go
go build -o bin/mvcc-concurrent examples/mvcc/concurrent/main.go
go build -o bin/mvcc-isolation examples/mvcc/isolation/main.go

# Build watch examples
go build -o bin/watch-simple examples/watch/simple/main.go
go build -o bin/watch-prefix examples/watch/prefix/main.go
go build -o bin/watch-filtering examples/watch/filtering/main.go

# Build memory examples
go build -o bin/memory-simple examples/memory/simple/main.go
go build -o bin/memory-caching examples/memory/caching/main.go
```

### Run All Examples

```bash
# MVCC examples
for ex in basic concurrent isolation; do
    echo "=== Running MVCC $ex example ==="
    go run examples/mvcc/$ex/main.go
    echo ""
done

# Watch examples
for ex in simple prefix filtering; do
    echo "=== Running Watch $ex example ==="
    go run examples/watch/$ex/main.go
    echo ""
done

# Memory examples
for ex in simple caching; do
    echo "=== Running Memory $ex example ==="
    go run examples/memory/$ex/main.go
    echo ""
done
```

### Run Tests and Benchmarks

```bash
# Run all tests
go test -v ./examples/memory/testing/

# Run tests with race detector
go test -race ./examples/memory/testing/

# Run benchmarks
go test -bench=. ./examples/memory/testing/

# Run specific benchmark
go test -bench=BenchmarkMemoryOperations/Set ./examples/memory/testing/
```

## Code Snippets

### Quick Start: Memory Database

```go
package main

import (
    "log"
    "github.com/jeremyhahn/go-quicbaas/pkg/memory"
)

type User struct {
    ID    uint64 `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

func (u *User) GetID() uint64   { return u.ID }
func (u *User) SetID(id uint64) { u.ID = id }

func main() {
    // Open database
    db, err := memory.Open[*User]()
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Create user
    user := &User{Name: "Alice", Email: "alice@example.com"}
    db.Set(user)

    // Read user
    retrieved, _ := db.Get(user.ID)
    println(retrieved.Name) // "Alice"
}
```

### Quick Start: MVCC Transaction

```go
package main

import (
    "context"
    "log"
    "github.com/jeremyhahn/go-quicbaas/pkg/memory"
)

func main() {
    // Open with MVCC enabled
    db, err := memory.Open[*User](memory.WithMVCC(true))
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Begin transaction
    ctx := context.Background()
    tx, _ := db.BeginTx(ctx)

    // Perform operations
    tx.Put([]byte("key1"), []byte("value1"))
    tx.Put([]byte("key2"), []byte("value2"))

    // Commit atomically
    if err := tx.Commit(); err != nil {
        tx.Rollback()
        log.Fatal(err)
    }
}
```

### Quick Start: Watch for Changes

```go
package main

import (
    "context"
    "log"
    "github.com/jeremyhahn/go-quicbaas/pkg/memory"
)

func main() {
    // Open with watches enabled
    db, err := memory.Open[*User](memory.WithWatches(true))
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    ctx := context.Background()

    // Create watch
    eventCh, _, _ := db.Watch(ctx, "", memory.WithPrefix())

    // Process events
    go func() {
        for event := range eventCh {
            println("Changed:", event.Key)
        }
    }()

    // Trigger events
    user := &User{Name: "Alice", Email: "alice@example.com"}
    db.Set(user) // Will trigger watch event
}
```

## Performance Expectations

### Memory Mode
- **Create**: 1-5 µs per operation
- **Read**: 0.5-2 µs per operation
- **Update**: 1-5 µs per operation
- **Delete**: 1-3 µs per operation
- **Throughput**: 200K-500K ops/sec

### MVCC Transactions
- **Transaction overhead**: 50-100 µs
- **Commit time**: 100-500 µs
- **Concurrent transactions**: High scalability

### Watches
- **Notification latency**: 150-300 ns
- **Memory per watch**: ~152 bytes
- **Write overhead**: <5%

## Troubleshooting

### Example Doesn't Compile

```bash
# Ensure you're in the project root
cd /path/to/go-quicbaas

# Update dependencies
go mod tidy

# Try building again
go build examples/memory/simple/main.go
```

### Test Failures

```bash
# Run with verbose output
go test -v ./examples/memory/testing/

# Run with race detector
go test -race ./examples/memory/testing/

# Check for version conflicts
go mod verify
```

### Performance Issues

```bash
# Run benchmarks to establish baseline
go test -bench=. ./examples/memory/testing/

# Profile CPU usage
go test -bench=. -cpuprofile=cpu.prof ./examples/memory/testing/

# Analyze profile
go tool pprof cpu.prof
```

## Next Steps

1. **Explore Examples**: Run each example to understand features
2. **Read Code**: Review example source code for implementation details
3. **Run Tests**: Execute test suite to see testing patterns
4. **Build Your App**: Use patterns in your own application
5. **Check Documentation**: See `/docs` for detailed API reference

## Example Output Samples

### Memory Simple Example
```
=== Simple Memory Mode Example ===
Step 1: Opening memory database...
  Database opened successfully

Step 2: Creating users...
  Created: alice (ID: 1) - alice@example.com
  Created: bob (ID: 2) - bob@example.com

Step 10: Performance test...
  Created 1000 users in 1.088ms
  Average: 1.09 µs per operation
  Throughput: 918K ops/sec
```

### MVCC Basic Example
```
=== Basic MVCC Transaction Example ===
Step 1: Creating transaction...
Transaction started successfully

Step 2: Creating users in transaction...
  - Staged: Alice Johnson <alice@example.com>
  - Staged: Bob Smith <bob@example.com>

Step 3: Committing transaction...
Transaction committed successfully!
```

### Watch Simple Example
```
=== Simple Watch Example ===
Step 1: Creating watch for all products...
  Watch created (ID: 1)

Step 2: Creating products...
[EVENT] Product Updated: Laptop - $999.99 (Stock: 10)
[EVENT] Product Updated: Mouse - $29.99 (Stock: 50)
```

## Quick Reference

| Feature | Example | Duration | Complexity |
|---------|---------|----------|------------|
| Memory Mode | `memory/simple` | 2s | Easy |
| MVCC Basic | `mvcc/basic` | 3s | Easy |
| MVCC Concurrent | `mvcc/concurrent` | 8s | Medium |
| MVCC Isolation | `mvcc/isolation` | 5s | Medium |
| Watch Simple | `watch/simple` | 5s | Easy |
| Watch Prefix | `watch/prefix` | 10s | Medium |
| Watch Filtering | `watch/filtering` | 10s | Medium |
| Memory Caching | `memory/caching` | 15s | Medium |
| Testing | `memory/testing` | <1s | Easy |
| KMS Client | `client/kms` | 5s | Medium |
| Secrets Client | `client/secrets` | 5s | Medium |
| IAM Client | `client/iam` | 10s | Medium |

## Support

- **Full README**: `examples/README.md`
- **API Documentation**: `/docs` directory
- **Source Code**: `/pkg` directory
- **Issues**: GitHub Issues

## License

See LICENSE file in the root directory.

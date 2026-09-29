# Go SDK — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/go-sdk/`.

Official Go SDK for QuicBaaS with support for gRPC, REST, QUIC, MCP, and Embedded protocols.

## Overview

The QuicBaaS Go SDK provides a type-safe, high-performance client library for interacting with QuicBaaS servers. It supports multiple transport protocols, connection pooling, automatic retries, and all QuicBaaS services including KV storage, KMS, secrets management, vector search, and time-series data.

## Key Features

| Feature | Description |
|---------|-------------|
| **Unified Client** | Single client with access to all services via `client.KV()`, `client.KMS()`, etc. |
| **Standalone Clients** | Use individual clients like `kv.NewClient(conn)` for specific needs |
| **Multi-Protocol** | gRPC (default), REST, QUIC, MCP, and Embedded transport support |
| **Connection Pooling** | High-performance connection management for gRPC |
| **Automatic Retry** | Configurable retry with exponential backoff |
| **mTLS Authentication** | Secure mutual TLS authentication (no API keys) |
| **Unix Socket** | High-performance local connections |
| **Watch Support** | Real-time key change notifications |
| **Type Safety** | Fully typed requests and responses |

## Installation

```bash
go get github.com/jeremyhahn/go-quicbaas/sdk/go
```

## Quick Start

### Unified Client

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
)

func main() {
    ctx := context.Background()

    // Connect via Unix socket (default for local)
    client, err := quicbaas.NewClient("/var/run/quicbaas.sock")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Put a value
    _, err = client.KV().Put(ctx, &kv.PutRequest{
        EntityType: "users",
        Key:        "user-123",
        Value:      []byte(`{"name": "Alice", "email": "alice@example.com"}`),
    })
    if err != nil {
        log.Fatal(err)
    }

    // Get the value
    resp, err := client.KV().Get(ctx, &kv.GetRequest{
        EntityType: "users",
        Key:        "user-123",
    })
    if err != nil {
        log.Fatal(err)
    }

    if resp.Found {
        log.Printf("User: %s", string(resp.Value))
    }
}
```

### With mTLS

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile: "/path/to/client.crt",
        KeyFile:  "/path/to/client.key",
        CAFile:   "/path/to/ca.crt",
    }),
)
```

### Watch for Changes

```go
// Watch a key prefix for changes
watcher, err := client.KV().WatchPrefix(ctx, "users/",
    kv.WithPrevKV(),  // Include previous value in events
)
if err != nil {
    log.Fatal(err)
}
defer watcher.Close()

for event := range watcher.Events() {
    switch event.Type {
    case kv.EventPut:
        log.Printf("PUT %s: %s", event.Key, string(event.Value))
    case kv.EventDelete:
        log.Printf("DELETE %s", event.Key)
    }
}
```

## Transports

| Transport | Use Case | Address Format |
|-----------|----------|----------------|
| gRPC | Default, high throughput | `localhost:50051` |
| Unix Socket | Local, lowest latency | `/var/run/quicbaas.sock` |
| REST | Web integration | `http://localhost:8080` |
| QUIC | Ultra-low latency | `localhost:50052` |
| MCP | AI/LLM agent tool integration | `http://localhost:8080` |
| Embedded | In-process, zero network overhead | N/A (direct service reference) |

### Connection Examples

```go
// gRPC (default)
client, _ := quicbaas.NewClient("localhost:50051")

// Unix socket (auto-detected)
client, _ := quicbaas.NewClient("/var/run/quicbaas.sock")

// Unix socket (explicit)
client, _ := quicbaas.NewClient("unix:///var/run/quicbaas.sock")

// REST
client, _ := quicbaas.NewClient("http://localhost:8080", quicbaas.WithREST())

// QUIC
client, _ := quicbaas.NewClient("localhost:50052", quicbaas.WithQUIC())

// MCP (Model Context Protocol)
client, _ := quicbaas.NewClient("http://localhost:8080", quicbaas.WithMCP())

// MCP with auto-authentication
client, _ := quicbaas.NewClient("http://localhost:8080",
    quicbaas.WithMCP(),
    quicbaas.WithMCPCredentials("admin", "admin"),
)

// MCP with pre-existing token
client, _ := quicbaas.NewClient("http://localhost:8080",
    quicbaas.WithMCP(),
    quicbaas.WithMCPAuthToken(token),
)

// Embedded (in-process, no network)
client, _ := quicbaas.NewClient("", quicbaas.WithEmbedded(svc))
```

## Services

| Service | Description |
|---------|-------------|
| `KV()` | Key-value storage with CRUD and Watch |
| `Batch()` | Atomic batch operations |
| `Transaction()` | Compare-and-swap transactions |
| `Query()` | Secondary index queries |
| `Cluster()` | Cluster status and management |
| `Discovery()` | Service discovery registry (Register/Renew/Deregister/Resolve/Watch) on qrdb lease+watch+KV |
| `Health()` | Health checks and probes |
| `Seal()` | Seal/unseal operations |
| `KMS()` | Key management service |
| `Secrets()` | Secret storage with versioning |
| `Crypto()` | Encryption/decryption as a service |
| `Vector()` | Vector similarity search |
| `TimeSeries()` | Time-series data storage |
| `Backup()` | Backup and restore |

## Configuration Options

| Option | Description |
|--------|-------------|
| `WithTimeout(d)` | Request timeout |
| `WithTLS(cfg)` | mTLS configuration |
| `WithTLSConfig(cfg)` | Custom `*tls.Config` |
| `WithInsecure()` | Disable TLS (development only) |
| `WithConnectionPool(min, max)` | Connection pool size |
| `WithRetry(max, backoff)` | Retry configuration |
| `WithREST()` | Use REST transport |
| `WithQUIC()` | Use QUIC transport |
| `WithMCP()` | Use MCP transport (JSON-RPC 2.0 over HTTP) |
| `WithMCPCredentials(user, pass)` | Auto-authenticate MCP transport |
| `WithMCPAuthToken(token)` | Pre-existing token for MCP transport |
| `WithEmbedded(svc)` | Use embedded in-process transport |

## Examples

The SDK includes runnable examples demonstrating common use cases:

| Example | Description | Location |
|---------|-------------|----------|
| **Vector Search** | Semantic search, hybrid queries, k-nearest neighbors | `examples/vector/main.go` |
| **Time-Series** | Metrics storage, aggregations, downsampling | `examples/timeseries/main.go` |
| **KMS** | Key generation, rotation, encryption | `examples/client/kms/main.go` |
| **Secrets** | Secret storage with versioning | `examples/client/secrets/main.go` |
| **IAM** | User, role, and policy management | `examples/client/iam/main.go` |
| **ABAC** | Attribute-based access control | `examples/client/abac/main.go` |
| **Watch** | Real-time key change notifications | `examples/watch/simple/main.go` |
| **MVCC** | Multi-version concurrency control | `examples/mvcc/basic/main.go` |

### Running Examples

```bash
# Vector search example
go run examples/vector/main.go

# With different transport
go run examples/vector/main.go -transport rest -address http://localhost:8080

# Time-series example
go run examples/timeseries/main.go

# With QUIC transport
go run examples/timeseries/main.go -transport quic -address localhost:50052
```

See examples.md for complete code examples and detailed documentation.

## Documentation

- [Architecture](architecture.md) - Transport abstraction and client design
- Authentication - mTLS setup and configuration
- API Reference - Complete API documentation
- Configuration - All configuration options
- Examples - Code examples for common use cases
- Roadmap - Development progress and status

## See Also

- CLI Documentation - Command-line interface
- KMS Documentation - Key Management Service
- Secrets Documentation - Secrets Engine


---

## QuicBaaS Go SDK Examples

This document provides complete, working code examples for common use cases with the QuicBaaS Go SDK.

### Runnable Examples

The SDK includes fully runnable example applications located in the `examples/` directory:

| Example | Description | Location |
|---------|-------------|----------|
| **Vector Search** | Semantic search, hybrid queries, k-nearest neighbors | `examples/vector/main.go` |
| **Time-Series** | Metrics storage, aggregations, downsampling | `examples/timeseries/main.go` |
| **KMS** | Key generation, rotation, encryption | `examples/client/kms/main.go` |
| **Secrets** | Secret storage with versioning | `examples/client/secrets/main.go` |
| **IAM** | User, role, and policy management | `examples/client/iam/main.go` |
| **ABAC** | Attribute-based access control | `examples/client/abac/main.go` |
| **Watch** | Real-time key change notifications | `examples/watch/simple/main.go` |
| **MVCC** | Multi-version concurrency control | `examples/mvcc/basic/main.go` |

Run any example with:

```bash
# Default gRPC transport
go run examples/vector/main.go

# REST transport
go run examples/vector/main.go -transport rest -address http://localhost:8080

# QUIC transport
go run examples/timeseries/main.go -transport quic -address localhost:50052
```

### Table of Contents

1. [Basic KV Operations](#basic-kv-operations)
2. [Watch Operations](#watch-operations)
3. [Batch Operations](#batch-operations)
4. [Transactions](#transactions)
5. [KMS Operations](#kms-operations)
6. [Secrets Management](#secrets-management)
7. [Vector Search](#vector-search)
8. [Time Series](#time-series)
9. [Backup and Restore](#backup-and-restore)

---

### Basic KV Operations

The KV client provides fundamental key-value storage operations including Put, Get, Delete, and List.

#### Connect, Put, Get, Delete

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
)

func main() {
    ctx := context.Background()

    // Create a client connection
    client, err := quicbaas.NewClient("localhost:50051",
        quicbaas.WithTimeout(30*time.Second),
        quicbaas.WithRetry(3, 100*time.Millisecond),
    )
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Store a key-value pair
    putResp, err := client.KV().Put(ctx, &kv.PutRequest{
        EntityType: "users",
        Key:        "user:1001",
        Value:      []byte(`{"name": "Alice", "email": "alice@example.com"}`),
        TTL:        24 * time.Hour, // Optional: auto-expire after 24 hours
    })
    if err != nil {
        log.Fatalf("Put failed: %v", err)
    }
    fmt.Printf("Stored at revision: %d\n", putResp.Revision)

    // Retrieve the value
    getResp, err := client.KV().Get(ctx, &kv.GetRequest{
        EntityType: "users",
        Key:        "user:1001",
    })
    if err != nil {
        log.Fatalf("Get failed: %v", err)
    }
    if getResp.Found {
        fmt.Printf("Value: %s (revision: %d)\n", string(getResp.Value), getResp.Revision)
    } else {
        fmt.Println("Key not found")
    }

    // Delete the key
    delResp, err := client.KV().Delete(ctx, &kv.DeleteRequest{
        EntityType: "users",
        Key:        "user:1001",
    })
    if err != nil {
        log.Fatalf("Delete failed: %v", err)
    }
    fmt.Printf("Deleted: %v (revision: %d)\n", delResp.Deleted, delResp.Revision)
}
```

#### List Keys with Pagination

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // List keys with a prefix, paginated
    cursor := ""
    for {
        listResp, err := client.KV().List(ctx, &kv.ListRequest{
            EntityType: "users",
            Prefix:     "user:",
            Limit:      100,
            Cursor:     cursor,
        })
        if err != nil {
            log.Fatalf("List failed: %v", err)
        }

        for _, key := range listResp.Keys {
            fmt.Printf("Key: %s\n", key)
        }

        if !listResp.HasMore {
            break
        }
        cursor = listResp.NextCursor
    }
}
```

---

### Watch Operations

The Watch API provides real-time notifications when keys change. This is useful for building reactive applications and cache invalidation.

#### Watch a Single Key

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Watch a specific key for changes
    watcher, err := client.KV().Watch(ctx, "config/app-settings",
        kv.WithPrevKV(), // Include previous value in events
    )
    if err != nil {
        log.Fatalf("Watch failed: %v", err)
    }
    defer watcher.Close()

    // Handle graceful shutdown
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

    fmt.Println("Watching for changes on 'config/app-settings'...")
    for {
        select {
        case event, ok := <-watcher.Events():
            if !ok {
                if err := watcher.Err(); err != nil {
                    log.Printf("Watch error: %v", err)
                }
                return
            }
            fmt.Printf("Event: %s\n", event.Type.String())
            fmt.Printf("  Key: %s\n", event.Key)
            fmt.Printf("  Value: %s\n", string(event.Value))
            if len(event.PrevValue) > 0 {
                fmt.Printf("  Previous: %s\n", string(event.PrevValue))
            }
            fmt.Printf("  Revision: %d\n", event.Revision)

        case <-sigCh:
            fmt.Println("Shutting down...")
            return
        }
    }
}
```

#### Watch a Prefix

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Watch all keys under a prefix
    watcher, err := client.KV().WatchPrefix(ctx, "events/",
        kv.WithStartRevision(1000), // Start from a specific revision
        kv.WithFilterDelete(),       // Only receive PUT events
    )
    if err != nil {
        log.Fatalf("Watch failed: %v", err)
    }
    defer watcher.Close()

    fmt.Println("Watching for new events under 'events/'...")
    for event := range watcher.Events() {
        fmt.Printf("[%s] %s = %s\n",
            event.Type.String(),
            event.Key,
            string(event.Value),
        )
    }

    if err := watcher.Err(); err != nil {
        log.Printf("Watch ended with error: %v", err)
    }
}
```

---

### Batch Operations

Batch operations allow executing multiple Put and Delete operations atomically in a single request.

#### Multiple Puts in a Batch

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Execute multiple operations atomically
    resp, err := client.Batch().Execute(ctx, &batch.BatchRequest{
        Operations: []batch.BatchOperation{
            {
                Type:       batch.OpPut,
                EntityType: "products",
                Key:        "product:001",
                Value:      []byte(`{"name": "Widget", "price": 9.99}`),
            },
            {
                Type:       batch.OpPut,
                EntityType: "products",
                Key:        "product:002",
                Value:      []byte(`{"name": "Gadget", "price": 19.99}`),
            },
            {
                Type:       batch.OpPut,
                EntityType: "products",
                Key:        "product:003",
                Value:      []byte(`{"name": "Gizmo", "price": 29.99}`),
            },
            {
                Type:       batch.OpDelete,
                EntityType: "products",
                Key:        "product:deprecated",
            },
        },
    })
    if err != nil {
        log.Fatalf("Batch failed: %v", err)
    }

    fmt.Printf("Batch completed at revision: %d\n", resp.Revision)
    for i, result := range resp.Results {
        if result.Success {
            fmt.Printf("  Operation %d: success\n", i)
        } else {
            fmt.Printf("  Operation %d: failed - %s\n", i, result.Error)
        }
    }
}
```

---

### Transactions

Transactions provide compare-and-swap (CAS) semantics for implementing optimistic concurrency control.

#### Compare-and-Swap Pattern

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/transaction"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Optimistic locking: update only if key hasn't changed
    resp, err := client.Transaction().Execute(ctx, &transaction.TxnRequest{
        Compare: []transaction.Compare{
            {
                Key:      "counter",
                Target:   transaction.CompareModRevision,
                Result:   transaction.CompareEqual,
                Revision: 5, // Expected revision
            },
        },
        Success: []transaction.TxnOp{
            {
                Type:       transaction.TxnOpPut,
                EntityType: "counters",
                Key:        "counter",
                Value:      []byte("6"), // New value
            },
        },
        Failure: []transaction.TxnOp{
            {
                Type:       transaction.TxnOpGet,
                EntityType: "counters",
                Key:        "counter",
            },
        },
    })
    if err != nil {
        log.Fatalf("Transaction failed: %v", err)
    }

    if resp.Succeeded {
        fmt.Printf("Update succeeded at revision: %d\n", resp.Revision)
    } else {
        fmt.Println("Conflict detected - key was modified by another client")
        if len(resp.Responses) > 0 {
            fmt.Printf("Current value: %s\n", string(resp.Responses[0].Value))
        }
    }
}
```

#### Create If Not Exists

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/transaction"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Create a key only if it doesn't exist (version == 0)
    resp, err := client.Transaction().Execute(ctx, &transaction.TxnRequest{
        Compare: []transaction.Compare{
            {
                Key:      "lock/resource-1",
                Target:   transaction.CompareVersion,
                Result:   transaction.CompareEqual,
                Revision: 0, // Key doesn't exist
            },
        },
        Success: []transaction.TxnOp{
            {
                Type:       transaction.TxnOpPut,
                EntityType: "locks",
                Key:        "lock/resource-1",
                Value:      []byte(`{"owner": "worker-1", "acquired": "2025-01-15T10:00:00Z"}`),
            },
        },
        Failure: []transaction.TxnOp{}, // Do nothing if already exists
    })
    if err != nil {
        log.Fatalf("Transaction failed: %v", err)
    }

    if resp.Succeeded {
        fmt.Println("Lock acquired successfully")
    } else {
        fmt.Println("Lock already held by another owner")
    }
}
```

---

### KMS Operations

The KMS (Key Management Service) client provides cryptographic key management including generation, rotation, and lifecycle management.

#### Generate a Key

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Generate a new encryption key
    genResp, err := client.KMS().GenerateKey(ctx, &kms.GenerateKeyRequest{
        Name:      "my-encryption-key",
        Algorithm: kms.AlgorithmAES256GCM,
        Usage: []kms.KeyUsage{
            kms.KeyUsageEncrypt,
            kms.KeyUsageDecrypt,
        },
    })
    if err != nil {
        log.Fatalf("Key generation failed: %v", err)
    }

    fmt.Printf("Generated key:\n")
    fmt.Printf("  ID: %s\n", genResp.KeyID)
    fmt.Printf("  Version: %d\n", genResp.Version)
    fmt.Printf("  Algorithm: %s\n", genResp.Algorithm.String())

    // List all keys
    listResp, err := client.KMS().ListKeys(ctx)
    if err != nil {
        log.Fatalf("List keys failed: %v", err)
    }

    fmt.Printf("\nAll keys:\n")
    for _, key := range listResp.Keys {
        fmt.Printf("  - %s (%s, v%d)\n", key.Name, key.Algorithm.String(), key.Version)
    }

    // Rotate the key
    rotateResp, err := client.KMS().RotateKey(ctx, genResp.KeyID)
    if err != nil {
        log.Fatalf("Key rotation failed: %v", err)
    }
    fmt.Printf("\nRotated key to version: %d\n", rotateResp.NewVersion)
}
```

#### Encrypt and Decrypt Data

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/crypto"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Generate an encryption key
    keyResp, err := client.KMS().GenerateKey(ctx, &kms.GenerateKeyRequest{
        Name:      "data-key",
        Algorithm: kms.AlgorithmAES256GCM,
        Usage:     []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt},
    })
    if err != nil {
        log.Fatalf("Key generation failed: %v", err)
    }

    plaintext := []byte("Sensitive data to encrypt")
    context := []byte("additional-authenticated-data") // Optional AAD

    // Encrypt data
    encResp, err := client.Crypto().Encrypt(ctx, &crypto.EncryptRequest{
        KeyID:     keyResp.KeyID,
        Plaintext: plaintext,
        Context:   context,
    })
    if err != nil {
        log.Fatalf("Encryption failed: %v", err)
    }
    fmt.Printf("Encrypted with key version: %d\n", encResp.KeyVersion)
    fmt.Printf("Ciphertext length: %d bytes\n", len(encResp.Ciphertext))

    // Decrypt data
    decResp, err := client.Crypto().Decrypt(ctx, &crypto.DecryptRequest{
        KeyID:      keyResp.KeyID,
        Ciphertext: encResp.Ciphertext,
        Context:    context,
    })
    if err != nil {
        log.Fatalf("Decryption failed: %v", err)
    }
    fmt.Printf("Decrypted: %s\n", string(decResp.Plaintext))
}
```

---

### Secrets Management

The Secrets client provides secure storage and retrieval of sensitive configuration data with versioning support.

#### Store and Retrieve Secrets

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Store a secret
    storeResp, err := client.Secrets().Store(ctx, &secrets.StoreSecretRequest{
        Path: "database/production",
        Data: map[string]interface{}{
            "host":     "db.example.com",
            "port":     5432,
            "username": "app_user",
            "password": "super-secret-password",
        },
        Metadata: map[string]string{
            "owner":       "platform-team",
            "environment": "production",
        },
    })
    if err != nil {
        log.Fatalf("Store secret failed: %v", err)
    }
    fmt.Printf("Stored secret version: %d\n", storeResp.Version)

    // Update the secret (creates a new version)
    storeResp, err = client.Secrets().Store(ctx, &secrets.StoreSecretRequest{
        Path: "database/production",
        Data: map[string]interface{}{
            "host":     "db.example.com",
            "port":     5432,
            "username": "app_user",
            "password": "new-rotated-password",
        },
        Metadata: map[string]string{
            "owner":       "platform-team",
            "environment": "production",
            "rotated":     "true",
        },
    })
    if err != nil {
        log.Fatalf("Update secret failed: %v", err)
    }
    fmt.Printf("Updated secret version: %d\n", storeResp.Version)

    // Retrieve the latest version
    secret, err := client.Secrets().Get(ctx, "database/production")
    if err != nil {
        log.Fatalf("Get secret failed: %v", err)
    }
    fmt.Printf("\nLatest secret (version %d):\n", secret.Version)
    fmt.Printf("  Host: %v\n", secret.Data["host"])
    fmt.Printf("  Port: %v\n", secret.Data["port"])
    fmt.Printf("  Created: %v\n", secret.CreatedAt)

    // Borrow secret material without it ever becoming a Go string. Get
    // returns fields as ordinary strings that cannot be wiped; GetWithBytes
    // holds the value in protected storage, lends it to the callback, and
    // destroys it on return. fn must not retain the slice. An empty field
    // name lends the whole stored payload. (Refused by the MCP transport,
    // whose tool results are text strings.)
    err = client.Secrets().GetWithBytes(ctx, "database/production", "password",
        func(password []byte) error {
            return connectDatabase(password)
        },
        secrets.WithVersion(1),
    )
    if err != nil {
        log.Fatalf("Borrow secret failed: %v", err)
    }

    // List all secrets under a path
    listResp, err := client.Secrets().List(ctx, "database/")
    if err != nil {
        log.Fatalf("List secrets failed: %v", err)
    }
    fmt.Printf("\nSecrets under 'database/':\n")
    for _, path := range listResp.Paths {
        fmt.Printf("  - %s\n", path)
    }

    // Delete a secret
    err = client.Secrets().Delete(ctx, "database/production")
    if err != nil {
        log.Fatalf("Delete secret failed: %v", err)
    }
    fmt.Println("\nSecret deleted")
}
```

---

### Vector Search

The Vector client provides similarity search capabilities for building semantic search, recommendation systems, and RAG applications.

For a comprehensive runnable example demonstrating all vector operations, see: `examples/vector/main.go`

**Run the example:**
```bash
go run examples/vector/main.go
go run examples/vector/main.go -transport rest -address http://localhost:8080
```

The example demonstrates:
- Inserting vectors with metadata into collections
- K-nearest neighbor similarity search
- Hybrid search combining vector similarity with metadata filters
- Topic-specific searches
- Finding documents similar to a known document
- Deleting vectors from collections

#### Insert Vectors and Search

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Insert vectors with metadata
    documents := []struct {
        id       string
        vector   []float32
        metadata map[string]interface{}
    }{
        {
            id:     "doc-001",
            vector: []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
            metadata: map[string]interface{}{
                "title":    "Introduction to Go",
                "category": "programming",
                "year":     2023,
            },
        },
        {
            id:     "doc-002",
            vector: []float32{0.15, 0.22, 0.31, 0.42, 0.51, 0.62, 0.71, 0.82},
            metadata: map[string]interface{}{
                "title":    "Advanced Go Patterns",
                "category": "programming",
                "year":     2024,
            },
        },
        {
            id:     "doc-003",
            vector: []float32{0.9, 0.8, 0.7, 0.6, 0.5, 0.4, 0.3, 0.2},
            metadata: map[string]interface{}{
                "title":    "Machine Learning Basics",
                "category": "ml",
                "year":     2024,
            },
        },
    }

    for _, doc := range documents {
        resp, err := client.Vector().Insert(ctx, &vector.InsertRequest{
            Collection: "documents",
            ID:         doc.id,
            Vector:     doc.vector,
            Metadata:   doc.metadata,
        })
        if err != nil {
            log.Fatalf("Insert failed: %v", err)
        }
        fmt.Printf("Inserted: %s\n", resp.ID)
    }

    // Search for similar vectors
    queryVector := []float32{0.12, 0.21, 0.32, 0.41, 0.52, 0.61, 0.72, 0.81}

    searchResp, err := client.Vector().Search(ctx, &vector.SearchRequest{
        Collection: "documents",
        Vector:     queryVector,
        TopK:       5,
        Filter: map[string]interface{}{
            "category": "programming", // Filter by metadata
        },
    })
    if err != nil {
        log.Fatalf("Search failed: %v", err)
    }

    fmt.Printf("\nSearch results (top %d):\n", len(searchResp.Results))
    for i, result := range searchResp.Results {
        fmt.Printf("%d. ID: %s\n", i+1, result.ID)
        fmt.Printf("   Score: %.4f\n", result.Score)
        fmt.Printf("   Title: %v\n", result.Metadata["title"])
        fmt.Printf("   Year: %v\n", result.Metadata["year"])
    }

    // Delete a vector
    err = client.Vector().Delete(ctx, &vector.DeleteRequest{
        Collection: "documents",
        ID:         "doc-001",
    })
    if err != nil {
        log.Fatalf("Delete failed: %v", err)
    }
    fmt.Println("\nDeleted doc-001")
}
```

---

### Time Series

The TimeSeries client provides efficient storage and querying of time-stamped metrics with aggregation support.

For a comprehensive runnable example demonstrating all time-series operations, see: `examples/timeseries/main.go`

**Run the example:**
```bash
go run examples/timeseries/main.go
go run examples/timeseries/main.go -transport quic -address localhost:50052
```

The example demonstrates:
- Writing time-series data points with tags
- Querying raw data by time range
- Aggregation queries (AVG, SUM, COUNT, MIN, MAX)
- Downsampling high-resolution data
- Complex queries with multiple tag filters
- Statistical analysis using aggregations
- Trend analysis comparing time periods

#### Write and Query Metrics

```go
package main

import (
    "context"
    "fmt"
    "log"
    "math/rand"
    "time"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Write metrics for the last hour
    now := time.Now()
    for i := 60; i >= 0; i-- {
        timestamp := now.Add(-time.Duration(i) * time.Minute)

        // CPU usage
        err := client.TimeSeries().Write(ctx, &timeseries.WriteRequest{
            Metric: "cpu.usage",
            Tags: map[string]string{
                "host":   "server-01",
                "region": "us-east-1",
            },
            Timestamp: timestamp,
            Value:     30.0 + rand.Float64()*50.0, // 30-80%
        })
        if err != nil {
            log.Fatalf("Write failed: %v", err)
        }

        // Memory usage
        err = client.TimeSeries().Write(ctx, &timeseries.WriteRequest{
            Metric: "memory.usage",
            Tags: map[string]string{
                "host":   "server-01",
                "region": "us-east-1",
            },
            Timestamp: timestamp,
            Value:     60.0 + rand.Float64()*20.0, // 60-80%
        })
        if err != nil {
            log.Fatalf("Write failed: %v", err)
        }
    }
    fmt.Println("Wrote 61 data points for CPU and memory")

    // Query raw data for the last 10 minutes
    queryResp, err := client.TimeSeries().Query(ctx, &timeseries.QueryRequest{
        Metric: "cpu.usage",
        Tags: map[string]string{
            "host": "server-01",
        },
        Start:     now.Add(-10 * time.Minute),
        End:       now,
        Aggregate: timeseries.AggregateNone, // Raw data
    })
    if err != nil {
        log.Fatalf("Query failed: %v", err)
    }

    fmt.Printf("\nRaw CPU usage (last 10 minutes, %d points):\n", len(queryResp.Series))
    for _, point := range queryResp.Series[:min(5, len(queryResp.Series))] {
        fmt.Printf("  %s: %.2f%%\n",
            point.Timestamp.Format("15:04:05"),
            point.Value,
        )
    }
    if len(queryResp.Series) > 5 {
        fmt.Printf("  ... and %d more points\n", len(queryResp.Series)-5)
    }

    // Query with aggregation - average per 5-minute bucket
    aggResp, err := client.TimeSeries().Query(ctx, &timeseries.QueryRequest{
        Metric: "cpu.usage",
        Tags: map[string]string{
            "host": "server-01",
        },
        Start:     now.Add(-60 * time.Minute),
        End:       now,
        Aggregate: timeseries.AggregateAvg,
        Interval:  5 * time.Minute,
    })
    if err != nil {
        log.Fatalf("Aggregated query failed: %v", err)
    }

    fmt.Printf("\nAverage CPU usage (5-minute buckets):\n")
    for _, point := range aggResp.Series {
        fmt.Printf("  %s: %.2f%%\n",
            point.Timestamp.Format("15:04"),
            point.Value,
        )
    }

    // Query with max aggregation
    maxResp, err := client.TimeSeries().Query(ctx, &timeseries.QueryRequest{
        Metric: "cpu.usage",
        Tags: map[string]string{
            "host": "server-01",
        },
        Start:     now.Add(-60 * time.Minute),
        End:       now,
        Aggregate: timeseries.AggregateMax,
        Interval:  15 * time.Minute,
    })
    if err != nil {
        log.Fatalf("Max query failed: %v", err)
    }

    fmt.Printf("\nMax CPU usage (15-minute buckets):\n")
    for _, point := range maxResp.Series {
        fmt.Printf("  %s: %.2f%%\n",
            point.Timestamp.Format("15:04"),
            point.Value,
        )
    }
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}
```

---

### Backup and Restore

The Backup client provides data protection capabilities including full backups with optional compression and encryption.

#### Create Backup and Restore

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Create a compressed and encrypted backup
    backupResp, err := client.Backup().Create(ctx, &backup.BackupRequest{
        Destination: "s3://my-bucket/quicbaas-backups/",
        Compress:    true,
        Encrypt:     true,
    })
    if err != nil {
        log.Fatalf("Backup failed: %v", err)
    }

    fmt.Printf("Backup created:\n")
    fmt.Printf("  ID: %s\n", backupResp.BackupID)
    fmt.Printf("  Size: %d bytes\n", backupResp.Size)
    fmt.Printf("  Duration: %v\n", backupResp.Duration)
    fmt.Printf("  Created: %v\n", backupResp.CreatedAt)

    // List all available backups
    listResp, err := client.Backup().List(ctx)
    if err != nil {
        log.Fatalf("List backups failed: %v", err)
    }

    fmt.Printf("\nAvailable backups:\n")
    for _, b := range listResp.Backups {
        encrypted := ""
        if b.Encrypted {
            encrypted = " (encrypted)"
        }
        fmt.Printf("  - %s: %d bytes, %v%s\n",
            b.ID, b.Size, b.CreatedAt.Format("2006-01-02 15:04:05"), encrypted)
    }

    // Restore from a backup
    restoreResp, err := client.Backup().Restore(ctx, &backup.RestoreRequest{
        BackupID: backupResp.BackupID,
    })
    if err != nil {
        log.Fatalf("Restore failed: %v", err)
    }

    fmt.Printf("\nRestore completed:\n")
    fmt.Printf("  Success: %v\n", restoreResp.Restored)
    fmt.Printf("  Duration: %v\n", restoreResp.Duration)
}
```

#### Restore from External Source

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Restore from an external backup source
    restoreResp, err := client.Backup().Restore(ctx, &backup.RestoreRequest{
        Source: "s3://disaster-recovery/quicbaas-backup-20250115.tar.gz",
    })
    if err != nil {
        log.Fatalf("Restore failed: %v", err)
    }

    if restoreResp.Restored {
        fmt.Printf("Successfully restored in %v\n", restoreResp.Duration)
    } else {
        fmt.Println("Restore did not complete")
    }
}
```

---

### Connection Options

The SDK supports various connection options for different deployment scenarios.

#### TLS/mTLS Configuration

```go
package main

import (
    "log"
    "time"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
    // Connect with mTLS
    client, err := quicbaas.NewClient("localhost:50051",
        quicbaas.WithTLS(quicbaas.TLSConfig{
            CertFile:   "/path/to/client.crt",
            KeyFile:    "/path/to/client.key",
            CAFile:     "/path/to/ca.crt",
            ServerName: "quicbaas.example.com",
        }),
        quicbaas.WithTimeout(30*time.Second),
    )
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Use the client...
}
```

#### Connection Pooling

```go
package main

import (
    "log"
    "time"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
    // Enable connection pooling for high-throughput scenarios
    client, err := quicbaas.NewClient("localhost:50051",
        quicbaas.WithConnectionPool(5, 50),      // Min 5, max 50 connections
        quicbaas.WithPoolMaxAge(time.Hour),       // Recycle connections after 1 hour
        quicbaas.WithPoolHealthInterval(30*time.Second),
    )
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    // Use the client...
}
```

#### Different Transports

```go
package main

import (
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
    // REST transport (auto-detected from URL scheme)
    restClient, err := quicbaas.NewClient("https://quicbaas.example.com:8443")
    if err != nil {
        log.Fatalf("Failed to connect via REST: %v", err)
    }
    defer restClient.Close()

    // Explicit REST transport
    restClient2, err := quicbaas.NewClient("quicbaas.example.com:8443",
        quicbaas.WithREST(),
    )
    if err != nil {
        log.Fatalf("Failed to connect via REST: %v", err)
    }
    defer restClient2.Close()

    // QUIC transport for low-latency scenarios
    quicClient, err := quicbaas.NewClient("quicbaas.example.com:50051",
        quicbaas.WithQUIC(),
    )
    if err != nil {
        log.Fatalf("Failed to connect via QUIC: %v", err)
    }
    defer quicClient.Close()

    // Unix socket connection
    unixClient, err := quicbaas.NewClient("/var/run/quicbaas.sock")
    if err != nil {
        log.Fatalf("Failed to connect via Unix socket: %v", err)
    }
    defer unixClient.Close()
}
```

---

### Error Handling

The SDK provides structured error handling with error codes and context.

```go
package main

import (
    "context"
    "fmt"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func main() {
    ctx := context.Background()

    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer client.Close()

    _, err = client.KV().Get(ctx, &kv.GetRequest{
        EntityType: "users",
        Key:        "nonexistent-key",
    })
    if err != nil {
        // Check error type using errors package
        if errors.IsNotFound(err) {
            fmt.Println("Key not found - this is expected")
        } else if errors.IsInvalidInput(err) {
            fmt.Println("Invalid input provided")
        } else if errors.IsConnectionError(err) {
            fmt.Println("Connection error - server may be unavailable")
        } else {
            // Get detailed error information
            if dragonErr, ok := err.(*errors.Error); ok {
                fmt.Printf("Error code: %d\n", dragonErr.Code)
                fmt.Printf("Operation: %s\n", dragonErr.Operation)
                fmt.Printf("Message: %s\n", dragonErr.Message)
            }
        }
    }
}
```

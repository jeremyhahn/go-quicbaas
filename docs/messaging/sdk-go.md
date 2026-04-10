# Go SDK

The QuicBaaS Go SDK provides a unified client for accessing all QuicBaaS services including key-value storage, messaging, secrets management, and more.

## Installation

```bash
go get github.com/jeremyhahn/go-quicbaas/sdk/go
```

## Quick Start

```go
package main

import (
    "context"
    "log"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
    // Create client
    client, err := quicbaas.NewClient("/var/run/quicbaas.sock")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()

    // Store a value
    _, err = client.KV().Put(ctx, &quicbaas.PutRequest{
        EntityType: "users",
        Key:        "user-123",
        Value:      []byte(`{"name": "Alice", "email": "alice@example.com"}`),
    })
    if err != nil {
        log.Fatal(err)
    }

    // Retrieve the value
    resp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
        EntityType: "users",
        Key:        "user-123",
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("User: %s", string(resp.Value))
}
```

## Connection Options

### Unix Socket (Recommended for Local)

```go
client, err := quicbaas.NewClient("/var/run/quicbaas.sock")
```

### TCP with gRPC

```go
client, err := quicbaas.NewClient("localhost:50051")
```

### TCP with mTLS

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile: "client.crt",
        KeyFile:  "client.key",
        CAFile:   "ca.crt",
    }),
)
```

### REST Transport

```go
// Auto-detected from HTTP URL
client, err := quicbaas.NewClient("http://localhost:8080")

// Or explicitly
client, err := quicbaas.NewClient("localhost:8080", quicbaas.WithREST())
```

### QUIC Transport

```go
client, err := quicbaas.NewClient("localhost:50052", quicbaas.WithQUIC())
```

### Connection Options

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTimeout(10 * time.Second),
    quicbaas.WithKeepalive(30 * time.Second, 10 * time.Second),
    quicbaas.WithMaxRetries(3),
    quicbaas.WithRetryBackoff(100 * time.Millisecond, 5 * time.Second),
    quicbaas.WithInsecure(), // Skip TLS (development only)
)
```

## Available Services

The unified client provides access to all QuicBaaS services:

| Service | Method | Description |
|---------|--------|-------------|
| KV | `client.KV()` | Key-value storage with Watch |
| Batch | `client.Batch()` | Atomic batch operations |
| Transaction | `client.Transaction()` | Compare-and-swap transactions |
| Query | `client.Query()` | Secondary index queries |
| Cluster | `client.Cluster()` | Cluster status and management |
| Health | `client.Health()` | Health checks and probes |
| Seal | `client.Seal()` | Seal/unseal operations |
| KMS | `client.KMS()` | Key management service |
| Secrets | `client.Secrets()` | Secret storage with versioning |
| Crypto | `client.Crypto()` | Encryption/decryption as a service |
| Vector | `client.Vector()` | Vector similarity search |
| TimeSeries | `client.TimeSeries()` | Time-series data storage |
| Backup | `client.Backup()` | Backup and restore |
| IAM | `client.IAM()` | Identity and access management |
| Messaging | `client.Messaging()` | Pub/sub and push notifications |

## KV Operations

### Put

```go
resp, err := client.KV().Put(ctx, &quicbaas.PutRequest{
    EntityType: "users",
    Key:        "user-123",
    Value:      []byte(`{"name": "Alice"}`),
})
if err != nil {
    return err
}
log.Printf("Stored at revision %d", resp.Revision)
```

### Get

```go
resp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
    EntityType: "users",
    Key:        "user-123",
})
if err != nil {
    if quicbaas.IsNotFound(err) {
        log.Println("User not found")
        return nil
    }
    return err
}
log.Printf("Value: %s", string(resp.Value))
```

### Delete

```go
resp, err := client.KV().Delete(ctx, &quicbaas.DeleteRequest{
    EntityType: "users",
    Key:        "user-123",
})
if err != nil {
    return err
}
log.Printf("Deleted: %v", resp.Deleted)
```

### List

```go
resp, err := client.KV().List(ctx, &quicbaas.ListRequest{
    EntityType: "users",
    Prefix:     "user-",
    Limit:      100,
})
if err != nil {
    return err
}
for _, kv := range resp.Kvs {
    log.Printf("%s: %s", kv.Key, string(kv.Value))
}
```

## Watch for Changes

```go
watcher, err := client.KV().WatchPrefix(ctx, "users/",
    kv.WithPrevKV(),           // Include previous value
    kv.WithStartRevision(100), // Start from revision 100
)
if err != nil {
    return err
}
defer watcher.Close()

for event := range watcher.Events() {
    switch event.Type {
    case quicbaas.EventPut:
        log.Printf("PUT %s: %s", event.Key, string(event.Value))
        if event.PrevKV != nil {
            log.Printf("  Previous: %s", string(event.PrevKV.Value))
        }
    case quicbaas.EventDelete:
        log.Printf("DELETE %s", event.Key)
    }
}
```

### Watch Options

| Option | Description |
|--------|-------------|
| `WithStartRevision(rev)` | Start watching from specific MVCC revision |
| `WithPrevKV()` | Include previous value in watch events |
| `WithProgressNotify()` | Receive periodic progress notifications |
| `WithFilterPut()` | Only receive DELETE events |
| `WithFilterDelete()` | Only receive PUT events |

## Batch Operations

```go
resp, err := client.Batch().Execute(ctx, &quicbaas.BatchRequest{
    Operations: []quicbaas.BatchOperation{
        {Type: quicbaas.BatchOpPut, EntityType: "users", Key: "user-1", Value: []byte(`{"name":"Alice"}`)},
        {Type: quicbaas.BatchOpPut, EntityType: "users", Key: "user-2", Value: []byte(`{"name":"Bob"}`)},
        {Type: quicbaas.BatchOpDelete, EntityType: "users", Key: "user-old"},
    },
})
if err != nil {
    return err
}

for i, result := range resp.Results {
    if result.Error != nil {
        log.Printf("Operation %d failed: %v", i, result.Error)
    }
}
```

## Transactions

```go
resp, err := client.Transaction().Txn(ctx, &quicbaas.TxnRequest{
    Compare: []quicbaas.Compare{
        {
            Target: quicbaas.CompareVersion,
            Key:    "users/user-123",
            Result: quicbaas.CompareEqual,
            Value:  5, // Expected version
        },
    },
    Success: []quicbaas.TxnOp{
        {Type: quicbaas.TxnOpPut, EntityType: "users", Key: "user-123", Value: []byte(`{"name":"Alice Updated"}`)},
    },
    Failure: []quicbaas.TxnOp{
        {Type: quicbaas.TxnOpGet, EntityType: "users", Key: "user-123"},
    },
})
if err != nil {
    return err
}

if resp.Succeeded {
    log.Println("Transaction succeeded")
} else {
    log.Println("Transaction failed, condition not met")
}
```

## Messaging (Pub/Sub)

### Create Channel

```go
channel, err := client.Messaging().CreateChannel(ctx, &quicbaas.MessagingCreateChannelRequest{
    Name:           "events",
    BufferSize:     256,
    MaxSubscribers: 1000,
    Persistent:     true,
    TTL:            time.Hour,
})
if err != nil {
    return err
}
log.Printf("Created channel: %s", channel.Name)
```

### Subscribe

```go
sub, err := client.Messaging().Subscribe(ctx, &quicbaas.MessagingSubscribeRequest{
    Channel: "events",
    Filter: &quicbaas.MessagingSubscriptionFilter{
        EntityTypes: []string{"orders"},
    },
})
if err != nil {
    return err
}
defer sub.Close()

for msg := range sub.Messages() {
    log.Printf("Received: %s", string(msg.Payload))
    msg.Ack()
}
```

### Publish

```go
err := client.Messaging().Publish(ctx, &quicbaas.MessagingPublishRequest{
    Channel: "events",
    Payload: []byte(`{"event":"order_created","orderId":"12345"}`),
})
if err != nil {
    return err
}
```

### Publish Delta

```go
err := client.Messaging().PublishDelta(ctx, &quicbaas.MessagingPublishDeltaRequest{
    Channel:    "users",
    EntityID:   "user-123",
    EntityType: "User",
    Delta: &quicbaas.MessagingDeltaPayload{
        Version:     42,
        PrevVersion: 41,
        Changes: []quicbaas.MessagingFieldChange{
            {Path: "email", Operation: quicbaas.MessagingOpSet, Value: "new@example.com"},
            {Path: "profile.lastLogin", Operation: quicbaas.MessagingOpSet, Value: time.Now()},
        },
    },
})
```

## DeltaApplier Usage

The `DeltaApplier` generic interface enables efficient client-side delta application:

```go
package main

import (
    "log"

    "github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// User entity
type User struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    Email     string `json:"email"`
    Version   uint64 `json:"version"`
}

// UserDeltaApplier applies deltas to User entities
type UserDeltaApplier struct{}

func (a *UserDeltaApplier) Apply(entity *User, delta *messaging.DeltaPayload) error {
    for _, change := range delta.Changes {
        switch change.Path {
        case "name":
            entity.Name = change.Value.(string)
        case "email":
            entity.Email = change.Value.(string)
        }
    }
    entity.Version = delta.Version
    return nil
}

func main() {
    // Local user cache
    user := &User{
        ID:      "user-123",
        Name:    "Alice",
        Email:   "alice@old.com",
        Version: 41,
    }

    applier := &UserDeltaApplier{}

    // Received delta from server
    delta := &messaging.DeltaPayload{
        EntityID:    "user-123",
        Version:     42,
        PrevVersion: 41,
        Changes: []messaging.FieldChange{
            {Path: "email", Operation: messaging.OpSet, Value: "alice@new.com"},
        },
    }

    // Apply delta to local entity
    if err := applier.Apply(user, delta); err != nil {
        log.Fatal(err)
    }

    log.Printf("Updated user: %+v", user)
    // Output: Updated user: &{ID:user-123 Name:Alice Email:alice@new.com Version:42}
}
```

### Generic DeltaApplier Interface

```go
type DeltaApplier[E any] interface {
    Apply(entity *E, delta *DeltaPayload) error
}
```

### Reflection-Based Applier

For simple cases, use the built-in reflection-based applier:

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/messaging"

computer := messaging.NewDeltaComputer(logger)

// Compute delta between two states
oldUser := &User{ID: "123", Name: "Alice", Email: "old@example.com"}
newUser := &User{ID: "123", Name: "Alice", Email: "new@example.com"}

delta, err := computer.Compute(oldUser, newUser)
if err != nil {
    return err
}

// Apply delta to another instance
targetUser := &User{ID: "123", Name: "Alice", Email: "old@example.com"}
if err := delta.Apply(targetUser); err != nil {
    return err
}
```

## Error Handling

The SDK provides typed errors for precise error handling:

```go
import "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"

resp, err := client.KV().Get(ctx, req)
if err != nil {
    switch {
    case errors.IsNotFound(err):
        log.Println("Key not found")
    case errors.IsConnectionFailed(err):
        log.Println("Connection failed, retrying...")
    case errors.IsTimeout(err):
        log.Println("Request timed out")
    case errors.IsInvalidInput(err):
        log.Println("Invalid request:", err)
    case errors.IsUnavailable(err):
        log.Println("Service unavailable")
    default:
        log.Printf("Unknown error: %v", err)
    }
    return err
}
```

### Error Codes

| Code | Function | Description |
|------|----------|-------------|
| `NotFound` | `IsNotFound(err)` | Resource not found |
| `AlreadyExists` | `IsAlreadyExists(err)` | Resource already exists |
| `InvalidInput` | `IsInvalidInput(err)` | Invalid request parameters |
| `Unauthorized` | `IsUnauthorized(err)` | Authentication required |
| `Forbidden` | `IsForbidden(err)` | Permission denied |
| `Timeout` | `IsTimeout(err)` | Request timed out |
| `ConnectionFailed` | `IsConnectionFailed(err)` | Connection failed |
| `Unavailable` | `IsUnavailable(err)` | Service unavailable |
| `Internal` | `IsInternal(err)` | Internal server error |

### Error Details

```go
if err != nil {
    code := errors.Code(err)
    message := errors.Message(err)
    details := errors.Details(err)

    log.Printf("Error [%d]: %s - %v", code, message, details)
}
```

## Testing

### Mock Transport

```go
import "github.com/jeremyhahn/go-quicbaas/sdk/go/test/mock"

func TestMyFunction(t *testing.T) {
    mockTransport := mock.NewTransport()
    mockTransport.OnRequest("KV", "Put", func(ctx context.Context, req interface{}) (interface{}, error) {
        return &quicbaas.PutResponse{Revision: 1}, nil
    })

    client := quicbaas.NewClientWithTransport(mockTransport)
    // ... test code
}
```

### In-Memory Server

```go
import "github.com/jeremyhahn/go-quicbaas/sdk/go/test/mock"

func TestIntegration(t *testing.T) {
    server := mock.NewServer()
    defer server.Close()

    client, err := quicbaas.NewClient(server.Address())
    if err != nil {
        t.Fatal(err)
    }
    defer client.Close()

    // ... integration test code
}
```

## Best Practices

1. **Reuse the client**: Create one client and reuse it across your application
2. **Always close**: Use `defer client.Close()` to ensure cleanup
3. **Handle errors**: Check for specific error types for appropriate handling
4. **Use context**: Pass context for cancellation and timeouts
5. **Watch cleanup**: Always close watchers with `defer watcher.Close()`
6. **Batch operations**: Use batch operations for multiple writes
7. **Delta publishing**: Use delta mode for efficient real-time updates

## Package Structure

```
sdk/go/
    quicbaas.go          # Unified client interface
    options.go           # Connection options
    types.go             # Type aliases
    doc.go               # Package documentation
    client/
        kv/              # KV operations
        batch/           # Batch operations
        transaction/     # Transaction operations
        query/           # Query operations
        cluster/         # Cluster management
        health/          # Health checks
        messaging/       # Pub/sub operations
        ...
    transport/
        transport.go     # Transport interface
        grpc/            # gRPC transport
        rest/            # REST transport
        quic/            # QUIC transport
        unix/            # Unix socket transport
    errors/
        errors.go        # Error types
        codes.go         # Error codes
    test/
        mock/            # Testing utilities
```

## Related Documentation

- [Delta Publishing](delta-publishing.md) - Delta computation and wire format
- [Architecture](architecture.md) - System design overview
- [API Reference](api-reference.md) - Core operations
- [CLI Reference](cli-reference.md) - Command-line usage

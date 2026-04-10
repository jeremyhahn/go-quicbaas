# Selective Indexing

## Overview

QuicBaaS supports selective field indexing. Only fields marked with `index:"true"` struct tags are indexed, conserving resources by avoiding unnecessary index creation.

## Why Selective Indexing?

Indexing every field has costs: storage overhead, write performance impact, and memory usage. Selective indexing balances query performance with resource efficiency.

## How to Use Selective Indexing

### 1. Define Your Entity Models

Create struct types with `index:"true"` tags on fields you want to query:

```go
package models

type User struct {
    // ID is always stored, but you control if it's indexed
    ID uint64 `json:"id"`

    // Indexed fields (marked with index:"true")
    Email     string `json:"email" index:"true"`
    Username  string `json:"username" index:"true"`
    Status    string `json:"status" index:"true"`
    CreatedAt int64  `json:"created_at" index:"true"`

    // NOT indexed - saves resources
    FullName          string `json:"full_name"`
    Bio               string `json:"bio"`
    PasswordHash      string `json:"password_hash"`
    ProfilePictureURL string `json:"profile_picture_url"`
}
```

### 2. Register Entity Types

Before storing entities, register their types with the QuicBaaS service layer. Entity type registration and indexing are managed by the qrdb database engine via the qrdb SDK.

```go
import (
    "reflect"
    "github.com/jeremyhahn/go-quicbaas/examples/models"
    "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func main() {
    // Register entity types via the service layer
    svc := service.New(/* ... */)

    if err := svc.RegisterEntityType("user", reflect.TypeOf(models.User{})); err != nil {
        panic(fmt.Sprintf("Failed to register User model: %v", err))
    }
}
```

### 3. Query Indexed Fields

Once registered, you can query entities by their indexed fields:

```go
// Query users by status
results, err := client.Query(ctx, &pb.QueryRequest{
    EntityType: "user",
    IndexName:  "status",
    Value:      []byte("active"),
})

// Query users by email
results, err := client.Query(ctx, &pb.QueryRequest{
    EntityType: "user",
    IndexName:  "email",
    Value:      []byte("user@example.com"),
})
```

## Complete Example

```go
package main

import (
    "fmt"
    "reflect"

    "github.com/jeremyhahn/go-quicbaas/examples/models"
    "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func main() {
    // Create service (entity types are registered via the service layer;
    // indexing is handled by the qrdb engine)
    svc := service.New(/* ... */)

    // Register entity types with their models
    if err := svc.RegisterEntityType("user", reflect.TypeOf(models.User{})); err != nil {
        panic(fmt.Sprintf("Failed to register User model: %v", err))
    }

    if err := svc.RegisterEntityType("product", reflect.TypeOf(models.Product{})); err != nil {
        panic(fmt.Sprintf("Failed to register Product model: %v", err))
    }

    if err := svc.RegisterEntityType("order", reflect.TypeOf(models.Order{})); err != nil {
        panic(fmt.Sprintf("Failed to register Order model: %v", err))
    }

    fmt.Println("Entity types registered successfully!")
    fmt.Println("\nIndexed fields:")
    fmt.Println("  User: Email, Username, Status, CreatedAt")
    fmt.Println("  Product: SKU, Category, Status, Price, StockQuantity")
    fmt.Println("  Order: CustomerID, Status, OrderDate, TotalAmount")
    fmt.Println("\nNon-indexed fields will NOT use extra storage or processing.")
}
```

## Best Practices

DO index: fields used in queries (status, category, email), foreign keys, date/time fields, enum-like fields.

DON'T index: large text fields, binary data, sensitive data not queried, fields with unique values per entity, nested objects or arrays.

### Example: Product Model

```go
type Product struct {
    ID uint64 `json:"id"`

    // Index these - commonly queried
    SKU           string  `json:"sku" index:"true"`
    Category      string  `json:"category" index:"true"`
    Status        string  `json:"status" index:"true"`
    Price         float64 `json:"price" index:"true"`
    StockQuantity int     `json:"stock_quantity" index:"true"`

    // Don't index these - not queried or too large
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    Metadata    map[string]interface{} `json:"metadata"`
}
```

## Backward Compatibility

For unregistered entity types, QuicBaaS falls back to indexing all JSON fields automatically. This ensures existing code continues to work without changes.

```go
// Without registration - all fields are indexed (legacy behavior)
client.Put(ctx, &pb.PutRequest{
    EntityType: "unregistered_type",
    Data:       jsonData,
})

// With registration - only tagged fields are indexed
minimalSM.RegisterEntityType("user", reflect.TypeOf(models.User{}))
client.Put(ctx, &pb.PutRequest{
    EntityType: "user",
    Data:       jsonData,
})
```

## Performance Considerations

Memory usage: each indexed field creates an in-memory map, size grows with unique values × entities.

Write performance: each indexed field requires one map insertion per write, non-indexed fields have zero overhead.

Query performance: indexed queries are O(1) lookup + O(n) iteration, non-indexed queries require full scan (not supported).

## Running the Example

```bash
# Build and run the selective indexing example
cd examples/selective_indexing
go run main.go

# Output:
# ✓ Entity types registered successfully!
#
# Indexed fields:
#   User: Email, Username, Status, CreatedAt
#   Product: SKU, Category, Status, Price, StockQuantity
#   Order: CustomerID, Status, OrderDate, TotalAmount
#
# Non-indexed fields will NOT use extra storage or processing.
```

## Implementation Details

The selective indexing system uses Go's reflection to parse struct tags at registration time. `RegisterEntityType()` scans struct tags, `IndexEntityWithType()` only indexes registered fields, unregistered types index all fields for backward compatibility.

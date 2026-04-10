# QuicBaaS DAO (Data Access Object)

The GenericDAO package provides a type-safe, storage-agnostic data access layer for QuicBaaS. It abstracts CRUD operations and pagination over any storage backend (Raft, PebbleDB, Memory), offering a clean, consistent API for persisting and querying entities.

## Overview

GenericDAO is QuicBaaS's unified data access pattern that eliminates boilerplate code while providing:

- **Type Safety**: Full compile-time type checking with Go generics
- **Storage Agnostic**: Works seamlessly with all QuicBaaS backends
- **Pagination**: Built-in cursor-based pagination with `Page()` and `ForEachPage()`
- **Multi-Codec Support**: Choose your serialization format (JSON, YAML, TOML, MsgPack, BSON)
- **Auto ID Generation**: Automatic timestamp-based ID assignment
- **Context Aware**: All operations support context cancellation and timeouts

## Key Features

### Type-Safe CRUD Operations

```go
type User struct {
    ID    uint64 `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

func (u *User) EntityID() uint64      { return u.ID }
func (u *User) SetEntityID(id uint64) { u.ID = id }

// Create DAO
userDAO, err := service.CreateDAO[*User](svc, "users", func() *User { return &User{} })

// Save (ID auto-generated if 0)
user := &User{Name: "Alice", Email: "alice@example.com"}
err = userDAO.Save(ctx, user)

// Get by ID
retrieved, err := userDAO.Get(ctx, user.EntityID())

// Delete
err = userDAO.Delete(ctx, user)

// Count
total, err := userDAO.Count(ctx)
```

### Built-in Pagination

```go
// Retrieve a single page
result, err := userDAO.Page(ctx, dao.PageQuery{
    Page:      1,
    PageSize:  25,
    SortOrder: dao.SortAscending,
})

// Process all pages with callback
err = userDAO.ForEachPage(ctx, dao.NewPageQuery(), func(users []*User) error {
    for _, user := range users {
        fmt.Printf("User: %s <%s>\n", user.Name, user.Email)
    }
    return nil
})
```

### Multi-Codec Support

```go
// Use MsgPack for high performance
userDAO, err := service.CreateDAO[*User](svc, "users",
    func() *User { return &User{} },
    dao.WithCodec(dao.CodecMsgPack))

// Use JSON for debugging
debugDAO, err := service.CreateDAO[*User](svc, "users",
    func() *User { return &User{} },
    dao.WithCodec(dao.CodecJSON))
```

### Works with All Backends

The same DAO code works transparently across:

- **Memory Mode**: In-memory storage for testing
- **Local Mode**: Direct PebbleDB access for single-node deployments
- **Cluster Mode**: Raft-replicated storage across multiple nodes

## Quick Start

### 1. Define Your Entity

Implement the `dao.Entity` interface:

```go
type Session struct {
    ID        uint64    `json:"id"`
    UserID    uint64    `json:"user_id"`
    Token     string    `json:"token"`
    ExpiresAt time.Time `json:"expires_at"`
}

func (s *Session) EntityID() uint64      { return s.ID }
func (s *Session) SetEntityID(id uint64) { s.ID = id }
```

### 2. Create a DAO

```go
// Using service.CreateDAO (recommended)
sessionDAO, err := service.CreateDAO[*Session](
    svc,
    "sessions",
    func() *Session { return &Session{} },
    dao.WithShard(1),
    dao.WithCodec(dao.CodecMsgPack),
    dao.WithTimeout(30*time.Second),
)
```

### 3. Perform CRUD Operations

```go
// Create
session := &Session{
    UserID:    1001,
    Token:     "abc123",
    ExpiresAt: time.Now().Add(24 * time.Hour),
}
err = sessionDAO.Save(ctx, session)
// session.ID now contains auto-generated ID

// Read
found, err := sessionDAO.Get(ctx, session.ID)

// Update (just save again)
found.ExpiresAt = time.Now().Add(48 * time.Hour)
err = sessionDAO.Save(ctx, found)

// Delete
err = sessionDAO.Delete(ctx, found)
```

## Architecture

```
┌─────────────────────────────────────┐
│       Application Layer              │
│   (Your business logic)              │
└──────────────┬──────────────────────┘
               │
               v
┌─────────────────────────────────────┐
│         GenericDAO[E]                │
│  - Type-safe CRUD operations         │
│  - Pagination (Page, ForEachPage)    │
│  - Auto ID generation                │
└──────────────┬──────────────────────┘
               │
               v
┌─────────────────────────────────────┐
│       Codec Layer                    │
│  JSON | YAML | TOML | MsgPack | BSON│
└──────────────┬──────────────────────┘
               │
               v
┌─────────────────────────────────────┐
│      interfaces.KVStore              │
│  - Put, Get, Delete, Scan            │
└──────────────┬──────────────────────┘
               │
       ┌───────┴───────┬───────────┐
       v               v           v
┌──────────┐   ┌──────────┐   ┌──────────┐
│  Memory  │   │ PebbleDB │   │   Raft   │
│ KVStore  │   │ KVStore  │   │ KVStore  │
└──────────┘   └──────────┘   └──────────┘
```

## Key Encoding Scheme

All entities are stored with a consistent key format:

```
<entityType>/<id>
```

Examples:
- `users/12345`
- `sessions/98765`
- `roles/1001`

This scheme enables:
- Efficient prefix scanning for pagination
- Isolation between entity types
- Simple ID-based lookups

## Performance Considerations

### Codec Selection

| Codec     | Speed       | Size    | Human-Readable | Use Case                    |
|-----------|-------------|---------|----------------|-----------------------------|
| JSON      | Moderate    | Large   | Yes            | Debugging, API responses    |
| YAML      | Slow        | Large   | Yes            | Configuration, documentation|
| TOML      | Slow        | Large   | Yes            | Configuration files         |
| MsgPack   | Very Fast   | Small   | No             | Production, high performance|
| BSON      | Fast        | Medium  | No             | MongoDB compatibility       |

**Recommendation**: Use `CodecMsgPack` for production workloads and `CodecJSON` for development/debugging.

### Pagination Best Practices

- Use reasonable page sizes (25-100 records)
- Process large datasets with `ForEachPage()` instead of loading all at once
- Consider the total dataset size when setting `PageSize`

## Documentation

- [Getting Started Guide](./getting-started.md) - Step-by-step tutorial
- [Configuration Options](./configuration.md) - All configuration options explained
- [Architecture Details](./architecture.md) - Deep dive into design and implementation

## Error Handling

The DAO package uses typed errors for precise error handling:

```go
user, err := userDAO.Get(ctx, 999)
if dao.IsNotFound(err) {
    // Handle not found case
    return nil, fmt.Errorf("user not found")
}
if err != nil {
    // Handle other errors
    return nil, err
}
```

Common error types:
- `ErrNotFound` - Entity doesn't exist
- `ErrInvalidEntityID` - Invalid or zero ID
- `ErrSerializationFailed` - Failed to encode entity
- `ErrDeserializationFailed` - Failed to decode entity
- `ErrInvalidConfig` - Invalid DAO configuration

## Thread Safety

GenericDAO instances are safe for concurrent use. Multiple goroutines can share a single DAO instance and perform operations simultaneously.

## Examples

See the [getting-started.md](./getting-started.md) guide for complete working examples including:
- User management system
- Session storage
- Multi-tenant data isolation
- Batch processing with pagination

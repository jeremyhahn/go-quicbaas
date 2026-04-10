# Getting Started with GenericDAO

This guide walks you through creating your first GenericDAO-powered application with QuicBaaS.

## Prerequisites

- QuicBaaS service instance running
- Go 1.21 or later
- Basic understanding of Go generics

## Step 1: Define Your Entity

Every entity must implement the `dao.Entity` interface with two methods:

```go
package main

import (
    "time"
    "github.com/jeremyhahn/go-quicbaas/pkg/dao"
)

type User struct {
    ID        uint64    `json:"id"`
    Username  string    `json:"username"`
    Email     string    `json:"email"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

// EntityID returns the unique identifier for this user
func (u *User) EntityID() uint64 {
    return u.ID
}

// SetEntityID sets the user's unique identifier
// Called automatically by Save() when ID is 0
func (u *User) SetEntityID(id uint64) {
    u.ID = id
}
```

**Key Points**:
- `EntityID()` must return 0 for new entities (not yet persisted)
- `SetEntityID()` is called automatically by `Save()` to assign IDs
- IDs are generated using `time.Now().UnixNano()` for guaranteed uniqueness

## Step 2: Create a DAO

### Method A: Using service.CreateDAO (Recommended)

```go
import (
    "context"
    "github.com/jeremyhahn/go-quicbaas/pkg/dao"
    "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func main() {
    // Assume you have a QuicBaaS service instance
    // svc := service.New(...)

    // Create a type-safe DAO for User entities
    userDAO, err := service.CreateDAO[*User](
        svc,
        "users",                        // Entity type namespace
        func() *User { return &User{} }, // Factory function
        dao.WithShard(1),               // Raft shard ID (cluster mode)
        dao.WithCodec(dao.CodecJSON),   // Serialization format
        dao.WithTimeout(30*time.Second), // Operation timeout
    )
    if err != nil {
        panic(err)
    }

    // Now you can use the DAO
    ctx := context.Background()
    user := &User{
        Username:  "alice",
        Email:     "alice@example.com",
        CreatedAt: time.Now(),
    }

    if err := userDAO.Save(ctx, user); err != nil {
        panic(err)
    }

    println("User saved with ID:", user.ID)
}
```

### Method B: Using dao.New Directly

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/dao"
)

func main() {
    // Get KVStore from your server instance
    kvStore := server.GetKVStore(1) // Shard ID 1

    userDAO, err := dao.New[*User](
        kvStore,
        "users",
        func() *User { return &User{} },
        dao.WithCodec(dao.CodecMsgPack),
    )
    if err != nil {
        panic(err)
    }
}
```

### Method C: Using Factory Pattern

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/dao"
)

func main() {
    // Create a factory from server's KVStore provider
    factory := dao.NewFactory(server.GetKVStore)

    // Create DAOs as needed
    userDAO, err := dao.NewDAO[*User](
        factory,
        "users",
        func() *User { return &User{} },
    )
    if err != nil {
        panic(err)
    }
}
```

## Step 3: CRUD Operations

### Create (Save)

```go
func createUser(userDAO dao.GenericDAO[*User]) error {
    ctx := context.Background()

    user := &User{
        Username:  "bob",
        Email:     "bob@example.com",
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }

    // Save will auto-generate ID if user.ID == 0
    if err := userDAO.Save(ctx, user); err != nil {
        return fmt.Errorf("failed to save user: %w", err)
    }

    fmt.Printf("Created user with ID: %d\n", user.ID)
    return nil
}
```

### Read (Get)

```go
func getUser(userDAO dao.GenericDAO[*User], id uint64) (*User, error) {
    ctx := context.Background()

    user, err := userDAO.Get(ctx, id)
    if err != nil {
        if dao.IsNotFound(err) {
            return nil, fmt.Errorf("user %d not found", id)
        }
        return nil, fmt.Errorf("failed to get user: %w", err)
    }

    fmt.Printf("Found user: %s <%s>\n", user.Username, user.Email)
    return user, nil
}
```

### Update

```go
func updateUser(userDAO dao.GenericDAO[*User], id uint64) error {
    ctx := context.Background()

    // Fetch existing user
    user, err := userDAO.Get(ctx, id)
    if err != nil {
        return err
    }

    // Modify fields
    user.Email = "newemail@example.com"
    user.UpdatedAt = time.Now()

    // Save updates (same ID, so it overwrites)
    if err := userDAO.Save(ctx, user); err != nil {
        return fmt.Errorf("failed to update user: %w", err)
    }

    fmt.Println("User updated successfully")
    return nil
}
```

### Delete

```go
func deleteUser(userDAO dao.GenericDAO[*User], id uint64) error {
    ctx := context.Background()

    // Fetch the user
    user, err := userDAO.Get(ctx, id)
    if err != nil {
        return err
    }

    // Delete it
    if err := userDAO.Delete(ctx, user); err != nil {
        return fmt.Errorf("failed to delete user: %w", err)
    }

    fmt.Println("User deleted successfully")
    return nil
}
```

### Count

```go
func countUsers(userDAO dao.GenericDAO[*User]) (int, error) {
    ctx := context.Background()

    count, err := userDAO.Count(ctx)
    if err != nil {
        return 0, fmt.Errorf("failed to count users: %w", err)
    }

    fmt.Printf("Total users: %d\n", count)
    return count, nil
}
```

## Step 4: Pagination

### Retrieve a Single Page

```go
func listUsersPage(userDAO dao.GenericDAO[*User], pageNum int) error {
    ctx := context.Background()

    query := dao.PageQuery{
        Page:      pageNum,
        PageSize:  25,
        SortOrder: dao.SortAscending,
    }

    result, err := userDAO.Page(ctx, query)
    if err != nil {
        return fmt.Errorf("failed to get page: %w", err)
    }

    fmt.Printf("Page %d of users (showing %d results):\n",
        result.Page, len(result.Entities))

    for _, user := range result.Entities {
        fmt.Printf("  - %s <%s>\n", user.Username, user.Email)
    }

    if result.HasMore {
        fmt.Println("More results available...")
    }

    return nil
}
```

### Process All Pages

```go
func processAllUsers(userDAO dao.GenericDAO[*User]) error {
    ctx := context.Background()

    query := dao.NewPageQuery() // Defaults: Page=1, PageSize=25, SortAscending

    totalProcessed := 0

    err := userDAO.ForEachPage(ctx, query, func(users []*User) error {
        for _, user := range users {
            totalProcessed++

            // Process each user
            fmt.Printf("[%d] Processing user: %s\n", totalProcessed, user.Username)

            // Simulate some work
            time.Sleep(10 * time.Millisecond)
        }
        return nil
    })

    if err != nil {
        return fmt.Errorf("failed to process users: %w", err)
    }

    fmt.Printf("Processed %d total users\n", totalProcessed)
    return nil
}
```

### Early Termination

```go
func findFirstInactiveUser(userDAO dao.GenericDAO[*User]) (*User, error) {
    ctx := context.Background()

    var found *User

    err := userDAO.ForEachPage(ctx, dao.NewPageQuery(), func(users []*User) error {
        for _, user := range users {
            if isInactive(user) {
                found = user
                return dao.ErrStopIteration // Stop processing
            }
        }
        return nil // Continue to next page
    })

    if err != nil && err != dao.ErrStopIteration {
        return nil, err
    }

    if found == nil {
        return nil, fmt.Errorf("no inactive users found")
    }

    return found, nil
}

func isInactive(user *User) bool {
    // Example: inactive if not updated in 90 days
    return time.Since(user.UpdatedAt) > 90*24*time.Hour
}
```

## Step 5: Error Handling

```go
import (
    "errors"
    "github.com/jeremyhahn/go-quicbaas/pkg/dao"
)

func handleDAOErrors(userDAO dao.GenericDAO[*User], id uint64) {
    ctx := context.Background()

    user, err := userDAO.Get(ctx, id)
    if err != nil {
        // Check for specific error types
        if dao.IsNotFound(err) {
            fmt.Println("User not found, creating new one...")
            // Handle not found case
            return
        }

        // Check for serialization errors
        var serErr dao.ErrSerializationFailed
        if errors.As(err, &serErr) {
            fmt.Printf("Failed to serialize %s: %v\n", serErr.EntityType, serErr.Cause)
            return
        }

        // Check for deserialization errors
        var deserErr dao.ErrDeserializationFailed
        if errors.As(err, &deserErr) {
            fmt.Printf("Failed to deserialize %s: %v\n", deserErr.EntityType, deserErr.Cause)
            return
        }

        // Generic error
        fmt.Printf("Unexpected error: %v\n", err)
        return
    }

    fmt.Printf("Successfully retrieved: %s\n", user.Username)
}
```

## Complete Example: User Management System

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jeremyhahn/go-quicbaas/pkg/dao"
    "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

type User struct {
    ID        uint64    `json:"id"`
    Username  string    `json:"username"`
    Email     string    `json:"email"`
    Active    bool      `json:"active"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

func (u *User) EntityID() uint64      { return u.ID }
func (u *User) SetEntityID(id uint64) { u.ID = id }

type UserManager struct {
    dao dao.GenericDAO[*User]
}

func NewUserManager(svc service.QuicBaaS) (*UserManager, error) {
    userDAO, err := service.CreateDAO[*User](
        svc,
        "users",
        func() *User { return &User{} },
        dao.WithCodec(dao.CodecMsgPack),
        dao.WithTimeout(30*time.Second),
    )
    if err != nil {
        return nil, err
    }

    return &UserManager{dao: userDAO}, nil
}

func (m *UserManager) CreateUser(username, email string) (*User, error) {
    ctx := context.Background()

    user := &User{
        Username:  username,
        Email:     email,
        Active:    true,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }

    if err := m.dao.Save(ctx, user); err != nil {
        return nil, fmt.Errorf("failed to create user: %w", err)
    }

    return user, nil
}

func (m *UserManager) GetUser(id uint64) (*User, error) {
    ctx := context.Background()
    return m.dao.Get(ctx, id)
}

func (m *UserManager) UpdateUser(user *User) error {
    ctx := context.Background()
    user.UpdatedAt = time.Now()
    return m.dao.Save(ctx, user)
}

func (m *UserManager) DeleteUser(id uint64) error {
    ctx := context.Background()
    user, err := m.dao.Get(ctx, id)
    if err != nil {
        return err
    }
    return m.dao.Delete(ctx, user)
}

func (m *UserManager) ListUsers(page, pageSize int) ([]*User, bool, error) {
    ctx := context.Background()

    result, err := m.dao.Page(ctx, dao.PageQuery{
        Page:      page,
        PageSize:  pageSize,
        SortOrder: dao.SortAscending,
    })
    if err != nil {
        return nil, false, err
    }

    return result.Entities, result.HasMore, nil
}

func (m *UserManager) CountActive() (int, error) {
    ctx := context.Background()

    count := 0
    err := m.dao.ForEachPage(ctx, dao.NewPageQuery(), func(users []*User) error {
        for _, user := range users {
            if user.Active {
                count++
            }
        }
        return nil
    })

    return count, err
}

func main() {
    // Initialize QuicBaaS service
    // svc := service.New(...)

    // Create user manager
    // manager, err := NewUserManager(svc)
    // if err != nil {
    //     panic(err)
    // }

    // Create users
    // alice, _ := manager.CreateUser("alice", "alice@example.com")
    // bob, _ := manager.CreateUser("bob", "bob@example.com")

    // List users
    // users, hasMore, _ := manager.ListUsers(1, 10)
    // for _, u := range users {
    //     fmt.Printf("User: %s <%s>\n", u.Username, u.Email)
    // }

    // Count active users
    // activeCount, _ := manager.CountActive()
    // fmt.Printf("Active users: %d\n", activeCount)
}
```

## Next Steps

- Review [Configuration Options](./configuration.md) for advanced settings
- Explore [Architecture Details](./architecture.md) to understand internals
- See the main [README](./README.md) for performance tuning tips

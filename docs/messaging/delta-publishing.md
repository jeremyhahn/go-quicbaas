# Delta Publishing

Delta publishing sends only changed fields instead of entire entities, reducing bandwidth and improving performance for real-time updates.

## Overview

Instead of transmitting complete entity state on every update, delta publishing computes the difference between old and new versions and sends only the modified fields. This is particularly effective for:

- Large entities with frequent small updates
- Mobile/IoT clients with limited bandwidth
- High-frequency update scenarios
- Real-time collaborative applications

## DeltaPayload Structure

```go
type DeltaPayload struct {
    EntityID    string        `json:"entity_id,omitempty"`
    EntityType  string        `json:"entity_type,omitempty"`
    Version     uint64        `json:"version"`
    PrevVersion uint64        `json:"prev_version"`
    Timestamp   time.Time     `json:"timestamp"`
    Changes     []FieldChange `json:"changes"`
}

type FieldChange struct {
    Path      string      `json:"path"`       // JSON path (e.g., "address.city")
    Operation OpType      `json:"op"`         // set, delete, append, increment
    Value     interface{} `json:"value,omitempty"`
    OldValue  interface{} `json:"old_value,omitempty"`
}

type OpType string

const (
    OpSet       OpType = "set"       // Set field to new value
    OpDelete    OpType = "delete"    // Remove field
    OpAppend    OpType = "append"    // Append to array/string
    OpIncrement OpType = "increment" // Increment numeric field
)
```

## DeltaComputer

`DeltaComputer` uses reflection to compare old and new entity states, generating a list of field changes.

### Basic Usage

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/messaging"

computer := messaging.NewDeltaComputer(logger)

oldUser := &User{ID: "123", Name: "Alice", Email: "alice@example.com"}
newUser := &User{ID: "123", Name: "Alice", Email: "alice@newdomain.com"}

delta, err := computer.Compute(oldUser, newUser)
if err != nil {
    return err
}

// delta.Changes contains: [{Path: "email", Op: "set", Value: "alice@newdomain.com"}]
```

### Supported Types

The DeltaComputer handles:

| Type | Behavior |
|------|----------|
| Nested structs | Recursive field-by-field comparison |
| Maps with struct values | Key-level tracking with nested comparison |
| Slices/arrays | Full slice replacement on change |
| Pointers | Nil-safe dereferencing |
| Interfaces | Underlying value comparison |
| time.Time | Treated as atomic value |

### JSON Tag Support

Field paths use JSON tag names when available:

```go
type Address struct {
    Street string `json:"street"`
    City   string `json:"city"`
    Skip   string `json:"-"` // Ignored in delta computation
}
```

Fields tagged with `json:"-"` are excluded from delta computation.

## Diffable Interface

For performance-critical paths, implement the `Diffable` interface to provide optimized delta computation:

```go
type Diffable interface {
    Diff(previous interface{}) ([]FieldChange, error)
    Apply(changes []FieldChange) error
    Version() uint64
}
```

### Implementation Example

```go
type User struct {
    ID      string
    Name    string
    Email   string
    version uint64
}

func (u *User) Diff(previous interface{}) ([]messaging.FieldChange, error) {
    prev, ok := previous.(*User)
    if !ok {
        return nil, errors.New("type mismatch")
    }

    var changes []messaging.FieldChange
    if u.Name != prev.Name {
        changes = append(changes, messaging.FieldChange{
            Path: "name", Operation: messaging.OpSet, Value: u.Name, OldValue: prev.Name,
        })
    }
    if u.Email != prev.Email {
        changes = append(changes, messaging.FieldChange{
            Path: "email", Operation: messaging.OpSet, Value: u.Email, OldValue: prev.Email,
        })
    }
    return changes, nil
}

func (u *User) Apply(changes []messaging.FieldChange) error {
    for _, c := range changes {
        switch c.Path {
        case "name":
            u.Name = c.Value.(string)
        case "email":
            u.Email = c.Value.(string)
        }
    }
    return nil
}

func (u *User) Version() uint64 {
    return u.version
}
```

When an entity implements `Diffable`, `DeltaComputer.Compute` uses the custom implementation instead of reflection.

## Configuration Options

```go
// Set custom codec for serialization
computer := messaging.NewDeltaComputer(logger, messaging.WithDeltaCodec(myCodec))

// Limit recursion depth (default: 10)
computer := messaging.NewDeltaComputer(logger, messaging.WithDeltaMaxDepth(5))

// Ignore zero-value changes (default: true)
computer := messaging.NewDeltaComputer(logger, messaging.WithDeltaIgnoreZeroChanges(false))
```

| Option | Default | Description |
|--------|---------|-------------|
| `WithDeltaCodec(codec)` | JSON | Codec for byte-level comparison |
| `WithDeltaMaxDepth(depth)` | 10 | Maximum recursion depth |
| `WithDeltaIgnoreZeroChanges(bool)` | true | Skip zero-to-zero value changes |

## Wire Format

Delta payloads serialize to compact JSON:

```json
{
    "entity_id": "user-123",
    "entity_type": "User",
    "version": 42,
    "prev_version": 41,
    "timestamp": "2026-01-21T10:30:00Z",
    "changes": [
        {"path": "email", "op": "set", "value": "new@example.com"},
        {"path": "profile.lastLogin", "op": "set", "value": "2026-01-21T10:30:00Z"}
    ]
}
```

### Nested Path Example

For nested structures:

```go
type Profile struct {
    LastLogin time.Time `json:"lastLogin"`
    Settings  Settings  `json:"settings"`
}

type Settings struct {
    Theme string `json:"theme"`
}
```

A change to `Settings.Theme` produces:

```json
{"path": "settings.theme", "op": "set", "value": "dark"}
```

## Byte-Level Comparison

For pre-serialized data, use `ComputeBytes`:

```go
oldData := []byte(`{"id":"123","name":"Alice"}`)
newData := []byte(`{"id":"123","name":"Bob"}`)

delta, err := computer.ComputeBytes(oldData, newData, &User{})
```

## Error Handling

DeltaComputer returns typed errors for specific failure conditions:

```go
delta, err := computer.Compute(old, new)
if err != nil {
    switch {
    case messaging.IsDeltaNilInput(err):
        // Handle nil input
    case messaging.IsDeltaTypeMismatch(err):
        // Handle type mismatch between old and new
    case messaging.IsDeltaMaxDepthExceeded(err):
        // Handle deeply nested structure
    case messaging.IsDeltaDiffFailed(err):
        // Handle Diffable interface failure
    }
}
```

## Performance Considerations

1. **Implement Diffable for hot paths**: Reflection-based comparison is flexible but slower than hand-coded comparison.

2. **Limit recursion depth**: Deep nesting increases computation time. Use `WithDeltaMaxDepth` to cap recursion.

3. **Enable zero-value filtering**: The default `WithDeltaIgnoreZeroChanges(true)` reduces payload size by excluding unchanged zero values.

4. **Use MessagePack for wire format**: Configure `WithDeltaCodec` with MessagePack for smaller payloads.

## Source Code

- DeltaComputer: `pkg/messaging/delta.go`
- Types: `pkg/messaging/types.go`
- Tests: `pkg/messaging/delta_test.go`

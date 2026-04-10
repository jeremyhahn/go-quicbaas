# Job Handlers

## Overview

Job handlers implement the business logic for scheduled tasks. Each handler is registered with the scheduler and invoked when a job of its type is due for execution.

## Built-in Handlers

### kms.grace_period

Disables a key version after its grace period expires.

**Purpose:** When a key version is rotated, the old version remains active during a grace period to allow in-flight operations to complete. This handler automatically disables the version when the grace period ends.

**Parameters:**
```json
{
  "key_id": "string",      // Required: Key identifier
  "version_id": "string"   // Required: Version to disable
}
```

**Behavior:**
1. Validates key and version exist
2. Checks version is still in grace period state
3. Disables the key version
4. Records audit log entry
5. Returns success with version details

**Example Job:**
```json
{
  "name": "disable-mykey-v1",
  "handler_type": "kms.grace_period",
  "schedule": {
    "type": "once",
    "run_at": "2024-01-15T10:00:00Z"
  },
  "params": {
    "key_id": "mykey",
    "version_id": "v1"
  }
}
```

**Error Conditions:**
- `KEY_NOT_FOUND` - Key does not exist
- `VERSION_NOT_FOUND` - Version does not exist
- `ALREADY_DISABLED` - Version already disabled (idempotent success)

### db.compaction

Performs database maintenance and compaction.

**Purpose:** Reclaims storage space by compacting the underlying database files and removing tombstones from deleted records.

**Parameters:**
```json
{
  "scope": "string",       // Optional: "full" or "incremental" [default: incremental]
  "max_duration": "string" // Optional: Maximum compaction time [default: 1h]
}
```

**Behavior:**
1. Acquires compaction lock
2. Identifies compaction candidates
3. Performs compaction within time budget
4. Releases lock
5. Returns statistics

**Example Job:**
```json
{
  "name": "nightly-compaction",
  "handler_type": "db.compaction",
  "schedule": {
    "type": "cron",
    "expression": "0 2 * * *"
  },
  "params": {
    "scope": "full",
    "max_duration": "2h"
  }
}
```

**Result:**
```json
{
  "success": true,
  "message": "Compaction completed",
  "data": {
    "bytes_reclaimed": 1073741824,
    "files_compacted": 15,
    "duration": "45m30s"
  }
}
```

### audit.cleanup

Removes audit log entries older than the retention period.

**Purpose:** Maintains audit log size by removing entries that exceed the configured retention policy.

**Parameters:**
```json
{
  "retention_days": "int",  // Required: Days to retain
  "batch_size": "int",      // Optional: Deletion batch size [default: 1000]
  "dry_run": "bool"         // Optional: Preview without deleting [default: false]
}
```

**Behavior:**
1. Calculates cutoff timestamp
2. Scans audit log entries older than cutoff
3. Deletes in batches to avoid memory pressure
4. Returns deletion statistics

**Example Job:**
```json
{
  "name": "audit-retention-90d",
  "handler_type": "audit.cleanup",
  "schedule": {
    "type": "cron",
    "expression": "0 3 * * 0"
  },
  "params": {
    "retention_days": 90,
    "batch_size": 5000
  }
}
```

**Result:**
```json
{
  "success": true,
  "message": "Audit cleanup completed",
  "data": {
    "entries_deleted": 150000,
    "oldest_remaining": "2024-10-15T00:00:00Z",
    "duration": "5m12s"
  }
}
```

## Handler Interface

```go
// JobContext provides execution context to handlers.
type JobContext struct {
    Context   context.Context
    Job       *Job
    Logger    *slog.Logger
    DB        Database
    KMS       KMSService
    Audit     AuditService
}

// JobResult contains the outcome of handler execution.
type JobResult struct {
    Success bool
    Message string
    Data    map[string]any
}

// JobHandler defines the interface for job handlers.
type JobHandler interface {
    // Type returns the handler type identifier.
    Type() string

    // Validate checks if the parameters are valid.
    Validate(params map[string]any) error

    // Execute runs the handler logic.
    Execute(ctx *JobContext) (*JobResult, error)
}
```

## Custom Handler Development

### Implementation Steps

1. **Define the handler struct:**

```go
type MyCustomHandler struct {
    // Dependencies injected at registration
    db     Database
    logger *slog.Logger
}
```

2. **Implement the interface:**

```go
func (h *MyCustomHandler) Type() string {
    return "custom.my_handler"
}

func (h *MyCustomHandler) Validate(params map[string]any) error {
    requiredParam, ok := params["required_field"]
    if !ok {
        return &ValidationError{
            Field:   "required_field",
            Message: "required_field is required",
        }
    }
    return nil
}

func (h *MyCustomHandler) Execute(ctx *JobContext) (*JobResult, error) {
    // Extract parameters
    target := ctx.Job.Params["target"].(string)

    // Perform operation
    ctx.Logger.Info("executing custom handler", "target", target)

    // Check for cancellation
    select {
    case <-ctx.Context.Done():
        return nil, ctx.Context.Err()
    default:
    }

    // Return result
    return &JobResult{
        Success: true,
        Message: "Custom operation completed",
        Data: map[string]any{
            "processed": target,
        },
    }, nil
}
```

3. **Register the handler:**

```go
func RegisterCustomHandlers(scheduler *Scheduler, db Database) {
    handler := &MyCustomHandler{
        db:     db,
        logger: slog.Default().With("handler", "custom.my_handler"),
    }
    scheduler.RegisterHandler(handler)
}
```

### Best Practices

**Idempotency:** Handlers should be idempotent. Running the same job twice should produce the same result without side effects.

```go
func (h *MyHandler) Execute(ctx *JobContext) (*JobResult, error) {
    // Check if already processed
    if h.isAlreadyProcessed(ctx) {
        return &JobResult{
            Success: true,
            Message: "Already processed (idempotent)",
        }, nil
    }
    // Process...
}
```

**Context Awareness:** Always respect context cancellation for graceful shutdown.

```go
func (h *MyHandler) Execute(ctx *JobContext) (*JobResult, error) {
    for _, item := range items {
        select {
        case <-ctx.Context.Done():
            return nil, ctx.Context.Err()
        default:
            h.processItem(item)
        }
    }
    return &JobResult{Success: true}, nil
}
```

**Error Handling:** Return typed errors for proper categorization.

```go
type RetryableError struct {
    Cause error
}

type PermanentError struct {
    Cause error
}

func (h *MyHandler) Execute(ctx *JobContext) (*JobResult, error) {
    err := h.doWork()
    if isTransient(err) {
        return nil, &RetryableError{Cause: err}
    }
    if err != nil {
        return nil, &PermanentError{Cause: err}
    }
    return &JobResult{Success: true}, nil
}
```

**Logging:** Use structured logging with job context.

```go
func (h *MyHandler) Execute(ctx *JobContext) (*JobResult, error) {
    logger := ctx.Logger.With(
        "job_id", ctx.Job.ID,
        "handler", h.Type(),
    )
    logger.Info("starting execution")
    // ...
    logger.Info("execution completed", "items_processed", count)
}
```

### Testing Handlers

```go
func TestMyHandler_Execute(t *testing.T) {
    tests := []struct {
        name    string
        params  map[string]any
        setup   func(*MockDB)
        want    *JobResult
        wantErr error
    }{
        {
            name: "successful execution",
            params: map[string]any{
                "target": "test-resource",
            },
            setup: func(db *MockDB) {
                db.On("Get", "test-resource").Return(testData, nil)
            },
            want: &JobResult{
                Success: true,
                Message: "Custom operation completed",
            },
        },
        {
            name: "resource not found",
            params: map[string]any{
                "target": "missing",
            },
            setup: func(db *MockDB) {
                db.On("Get", "missing").Return(nil, ErrNotFound)
            },
            wantErr: &PermanentError{},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            db := NewMockDB()
            tt.setup(db)

            handler := &MyCustomHandler{db: db}
            ctx := &JobContext{
                Context: context.Background(),
                Job:     &Job{Params: tt.params},
                Logger:  slog.Default(),
            }

            got, err := handler.Execute(ctx)

            if tt.wantErr != nil {
                require.ErrorAs(t, err, tt.wantErr)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.want.Success, got.Success)
        })
    }
}
```

# Scheduler Architecture

## Overview

The QuicBaaS Scheduler provides a unified job scheduling system for automated tasks across the platform. It supports both system-level operations (database maintenance, audit cleanup) and tenant-scoped jobs (key rotation, grace period enforcement).

The scheduler is designed for high availability, low latency, and distributed execution using lock-free algorithms and channel-based coordination.

## Job Lifecycle States

```
                    +----------+
                    | pending  |
                    +----+-----+
                         |
            +------------+------------+
            |            |            |
            v            v            v
       +---------+  +--------+  +-----------+
       | running |  | paused |  | canceled  |
       +----+----+  +--------+  +-----------+
            |            |
            +------------+
            |
    +-------+-------+
    |               |
    v               v
+----------+   +--------+
| completed|   | failed |
+----------+   +--------+
```

| State | Description |
|-------|-------------|
| `pending` | Job is scheduled and waiting for execution time |
| `running` | Job is currently executing |
| `completed` | Job finished successfully |
| `failed` | Job execution failed (may retry based on policy) |
| `paused` | Job is temporarily suspended |
| `canceled` | Job was canceled and will not execute |

## Data Partitioning

Jobs are stored in QuicBaaS with key prefixes that separate system and tenant data.

### System Jobs

```
sys:scheduler/jobs/{jobID}
sys:scheduler/jobs/{jobID}/history/{timestamp}
sys:scheduler/handlers/{handlerType}
sys:scheduler/locks/{jobID}
```

System jobs handle platform-wide operations:
- Database compaction
- Global audit cleanup
- System health checks

### Tenant Jobs

```
t:{tenantID}:scheduler/jobs/{jobID}
t:{tenantID}:scheduler/jobs/{jobID}/history/{timestamp}
```

Tenant jobs are scoped to specific tenants:
- Key version grace periods
- Tenant-specific maintenance
- Custom scheduled tasks

## Ownership Model

| Owner | Scope | Examples |
|-------|-------|----------|
| System | Platform-wide | db.compaction, audit.cleanup |
| Tenant | Single tenant | kms.grace_period, custom handlers |
| Delegated | Cross-tenant | Admin-managed tenant jobs |

### Access Control

```go
type Job struct {
    ID          string
    TenantID    string    // Empty for system jobs
    OwnerType   OwnerType // System, Tenant, Delegated
    OwnerID     string    // User or service account
    HandlerType string
    // ...
}
```

## Lock-Free Design Patterns

The scheduler avoids mutexes and blocking operations for maximum throughput.

### Atomic State Transitions

```go
type JobState uint32

const (
    StatePending JobState = iota
    StateRunning
    StateCompleted
    StateFailed
    StatePaused
    StateCanceled
)

type Job struct {
    state atomic.Uint32
    // ...
}

func (j *Job) TransitionTo(newState JobState) bool {
    for {
        current := JobState(j.state.Load())
        if !isValidTransition(current, newState) {
            return false
        }
        if j.state.CompareAndSwap(uint32(current), uint32(newState)) {
            return true
        }
    }
}
```

### Channel-Based Coordination

```go
type Scheduler struct {
    jobs       chan *Job
    done       chan struct{}
    ticker     *time.Ticker
    wg         sync.WaitGroup
}

func (s *Scheduler) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-s.ticker.C:
            s.dispatchPendingJobs(ctx)
        case job := <-s.jobs:
            s.wg.Add(1)
            go s.executeJob(ctx, job)
        }
    }
}
```

### Distributed Locking

Job execution uses optimistic locking with atomic compare-and-swap:

```go
func (s *Scheduler) acquireJobLock(ctx context.Context, jobID string) (bool, error) {
    lockKey := fmt.Sprintf("sys:scheduler/locks/%s", jobID)
    lockValue := fmt.Sprintf("%s:%d", s.nodeID, time.Now().UnixNano())

    // Atomic put-if-absent
    return s.db.PutIfAbsent(ctx, lockKey, lockValue, s.lockTTL)
}
```

## Component Diagram

```
+------------------------------------------------------------------+
|                        QuicBaaS Scheduler                         |
+------------------------------------------------------------------+
|                                                                   |
|  +------------------+    +------------------+    +--------------+ |
|  |   Job Queue      |    |  Handler Registry|    |   Executor   | |
|  |   (channel)      |--->|   (map-based)    |--->|   (workers)  | |
|  +------------------+    +------------------+    +--------------+ |
|          ^                                             |          |
|          |                                             v          |
|  +------------------+                         +--------------+    |
|  |  Ticker Loop     |                         |   Storage    |    |
|  |  (select-based)  |                         |   (QuicBaaS) |    |
|  +------------------+                         +--------------+    |
|                                                                   |
+------------------------------------------------------------------+
           |                    |                    |
           v                    v                    v
    +------------+       +------------+       +------------+
    |    REST    |       |    gRPC    |       |    MCP     |
    +------------+       +------------+       +------------+
```

## Handler Interface

```go
type JobContext struct {
    Context   context.Context
    Job       *Job
    Logger    *slog.Logger
    DB        Database
}

type JobResult struct {
    Success bool
    Message string
    Data    map[string]any
}

type JobHandler interface {
    Type() string
    Execute(ctx *JobContext) (*JobResult, error)
    Validate(params map[string]any) error
}
```

## Retry Policy

Failed jobs can be retried based on configurable policy:

```go
type RetryPolicy struct {
    MaxAttempts     int
    InitialInterval time.Duration
    MaxInterval     time.Duration
    Multiplier      float64
}
```

The scheduler uses exponential backoff with jitter to prevent thundering herd.

## Graceful Shutdown

```go
func (s *Scheduler) Shutdown(ctx context.Context) error {
    close(s.done)

    // Wait for in-flight jobs with timeout
    done := make(chan struct{})
    go func() {
        s.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

## Watch Integration for Custom Automation

QuicBaaS's Watch API enables real-time notifications when scheduled jobs affect KMS keys. This allows applications to trigger custom automation workflows such as re-encryption routines when key versions are rotated or disabled.

### KMS Key Watch Prefixes

| Prefix | Events | Use Case |
|--------|--------|----------|
| `kms/keys/` | All key changes | Monitor all key operations |
| `kms/keys/{keyID}` | Specific key changes | Monitor individual keys |
| `kms/keys/{keyID}/versions/` | Version changes | Track version lifecycle |

### Event Types

When the scheduler executes grace period jobs, the following events are emitted:

| Event Type | Description | Payload |
|------------|-------------|---------|
| `PUT` | Key metadata updated | Key with new status |
| `DELETE` | Key version removed | Key identifier |

### Example: Re-encryption Trigger

Applications can watch for key status changes and trigger custom re-encryption workflows:

```go
package main

import (
    "context"
    "encoding/json"
    "log"

    "github.com/jeremyhahn/go-quicbaas/pkg/client"
)

func main() {
    ctx := context.Background()
    db, err := client.Connect(ctx, &client.Config{
        Address: "localhost:10443",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Watch all KMS key changes
    watchCh, err := db.Watch(ctx, "kms/keys/", client.WithPrefix())
    if err != nil {
        log.Fatal(err)
    }

    for event := range watchCh {
        switch event.Type {
        case client.EventPut:
            var key KeyMetadata
            if err := json.Unmarshal(event.Value, &key); err != nil {
                log.Printf("failed to unmarshal key: %v", err)
                continue
            }

            // Check if a version was disabled (grace period expired)
            if key.Status == "disabled" {
                log.Printf("Key version disabled: %s v%d", key.ID, key.Version)
                triggerReEncryption(ctx, key.ID, key.Version)
            }

        case client.EventDelete:
            log.Printf("Key deleted: %s", event.Key)
        }
    }
}

func triggerReEncryption(ctx context.Context, keyID string, oldVersion uint64) {
    // Custom re-encryption logic:
    // 1. Query data encrypted with old version
    // 2. Decrypt with old version (still accessible during grace period)
    // 3. Re-encrypt with current version
    // 4. Update stored ciphertext
    log.Printf("Starting re-encryption for data using %s v%d", keyID, oldVersion)
}

type KeyMetadata struct {
    ID      string `json:"id"`
    Version uint64 `json:"version"`
    Status  string `json:"status"`
}
```

### Grace Period Workflow with Watches

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     Key Rotation with Grace Period                       │
└─────────────────────────────────────────────────────────────────────────┘

Time ─────────────────────────────────────────────────────────────────────►

    T0                      T1 (Grace Period)                T2
    │                           │                             │
    ▼                           │                             ▼
┌─────────┐                     │                      ┌─────────────┐
│ Rotate  │                     │                      │ Disable     │
│ Key     │                     │                      │ Old Version │
└────┬────┘                     │                      └──────┬──────┘
     │                          │                             │
     │ Creates v2 (active)      │                             │ Scheduler executes
     │ v1 remains enabled       │                             │ kms.grace_period
     │                          │                             │
     ▼                          ▼                             ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         Watch Event Stream                               │
├─────────────────────────────────────────────────────────────────────────┤
│ PUT kms/keys/mykey          PUT kms/keys/mykey                          │
│   version: 2                  version: 1                                 │
│   status: active              status: disabled  ◄── Triggers automation │
└─────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
                          ┌─────────────────────┐
                          │ Custom Automation    │
                          │ - Re-encryption      │
                          │ - Cache invalidation │
                          │ - Audit logging      │
                          └─────────────────────┘
```

### Best Practices

1. **Use Specific Prefixes**: Watch specific key IDs rather than the entire `kms/keys/` prefix to reduce event volume.

2. **Idempotent Handlers**: Design automation handlers to be idempotent since watch events may be replayed during reconnection.

3. **Graceful Degradation**: Implement retry logic for re-encryption failures and maintain a dead-letter queue for failed operations.

4. **Monitoring**: Track watch lag and automation success rates to ensure timely processing of key lifecycle events.

5. **Security**: Ensure watch consumers have appropriate permissions and audit all automated key operations.

## Tenant Isolation

The scheduler service enforces tenant boundaries using the `TargetTenantID` field on each job rather than prefix-based key scoping.

### Tenant ID Assignment

`iam.TenantFromContext(ctx)` extracts the tenant ID from the request context. On `CreateJob`, the service sets `TargetTenantID` from the context. If the job already has a `TargetTenantID` set, the service enforces that it matches the context value and rejects mismatches with a typed error.

### Operation Enforcement

- **GetJob**: Returns the job only if its `TargetTenantID` matches the requesting tenant
- **UpdateJob**: Validates `TargetTenantID` match before applying changes
- **DeleteJob**: Validates `TargetTenantID` match before deletion
- **ListJobs**: Filters results to include only jobs where `TargetTenantID` matches the requesting tenant

### Single-Tenant Mode

When the tenant ID from context is empty, the service operates in single-tenant mode. All jobs are visible regardless of their `TargetTenantID` value, and no enforcement is applied. This maintains backward compatibility with existing single-tenant deployments.

### Cross-Tenant Access

Attempts to read, update, or delete a job belonging to a different tenant return a typed `ErrTenantAccessDenied` error. Enforcement occurs at the service layer before any storage operations execute.

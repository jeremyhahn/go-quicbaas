# Scheduler — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/scheduler/`.

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


---

## Scheduler Integration Guide

### Overview

This guide documents how to integrate the QuicBaaS scheduler into consuming applications like go-trusted-platform. The scheduler provides a unified job scheduling system that can be extended with platform-specific handlers.

### Architecture

```
+-------------------+       +-------------------+
| go-trusted-platform|       |   go-quicbaas     |
+-------------------+       +-------------------+
|                   |       |                   |
| Platform Handlers |------>| Scheduler Engine  |
|  - ca.crl_gen     |       |  - Job Storage    |
|  - attestation    |       |  - Handler Dispatch|
|  - tenant.quota   |       |  - Tick Loop      |
|                   |       |                   |
+-------------------+       +-------------------+
        |                           |
        v                           v
+-------------------------------------------+
|          QuicBaaS Storage Layer           |
+-------------------------------------------+
```

### Integration Patterns

#### Pattern 1: Import and Extend

Import the scheduler from go-quicbaas and register platform-specific handlers.

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
    "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
)

func initPlatformScheduler(db *quicbaas.DB) (*scheduler.SchedulerEngine, error) {
    // Get scheduler from QuicBaaS instance
    sched := db.Scheduler()

    // Register platform-specific handlers
    if err := sched.RegisterHandler("ca.crl_generation", handleCRLGeneration); err != nil {
        return nil, err
    }
    if err := sched.RegisterHandler("attestation.verify", handleAttestationVerify); err != nil {
        return nil, err
    }
    if err := sched.RegisterHandler("tenant.quota_sync", handleQuotaSync); err != nil {
        return nil, err
    }

    return sched, nil
}
```

#### Pattern 2: Handler Registry

Use the handler registry for organized handler management.

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
)

// Create platform handler registry
func NewPlatformHandlers(ca *CAService, attest *AttestationService) *handlers.Registry {
    registry := handlers.NewRegistry()

    // Register CA handlers
    registry.Register(&CRLGenerationHandler{ca: ca})
    registry.Register(&CertRenewalHandler{ca: ca})

    // Register attestation handlers
    registry.Register(&AttestationVerifyHandler{attest: attest})

    return registry
}

// Register all handlers with scheduler
func (p *Platform) initScheduler() error {
    registry := NewPlatformHandlers(p.ca, p.attestation)
    return registry.RegisterAll(p.db.Scheduler())
}
```

#### Pattern 3: Service Layer Adapter

Create adapters to bridge platform services with scheduler handlers.

```go
// Adapter pattern for platform services
type CASchedulerAdapter struct {
    ca *CAService
}

func (a *CASchedulerAdapter) ScheduleCRLRegeneration(ctx context.Context, interval time.Duration) error {
    job := &scheduler.Job{
        Name:      "crl-regeneration",
        Type:      scheduler.JobTypeRecurring,
        Handler:   "ca.crl_generation",
        Schedule:  fmt.Sprintf("@every %s", interval),
        OwnerType: scheduler.OwnerTypeSystem,
        Payload:   json.RawMessage(`{"full": false}`),
    }
    return a.ca.db.Scheduler().ScheduleJob(ctx, job)
}
```

### Creating Custom Handlers

#### Handler Interface

All handlers must implement the `Handler` interface:

```go
type Handler interface {
    // Name returns the unique handler identifier
    Name() string

    // Handler returns the job execution function
    Handler() scheduler.JobHandler
}
```

#### Example: CRL Generation Handler

```go
package platform

import (
    "context"
    "encoding/json"

    "github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

const HandlerCRLGeneration = "ca.crl_generation"

type CRLGenerationPayload struct {
    Full        bool   `json:"full"`
    Partition   string `json:"partition,omitempty"`
    MaxEntries  int    `json:"max_entries,omitempty"`
}

type CRLGenerationHandler struct {
    ca *CAService
}

func NewCRLGenerationHandler(ca *CAService) *CRLGenerationHandler {
    return &CRLGenerationHandler{ca: ca}
}

func (h *CRLGenerationHandler) Name() string {
    return HandlerCRLGeneration
}

func (h *CRLGenerationHandler) Handler() scheduler.JobHandler {
    return h.Handle
}

func (h *CRLGenerationHandler) Handle(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
    var payload CRLGenerationPayload
    if err := json.Unmarshal(job.Payload, &payload); err != nil {
        return nil, &scheduler.ErrInvalidJob{
            Field:   "payload",
            Message: "invalid CRL generation payload",
        }
    }

    // Generate CRL
    crl, err := h.ca.GenerateCRL(ctx, payload.Full)
    if err != nil {
        return nil, &scheduler.ErrHandlerFailed{
            Handler: HandlerCRLGeneration,
            JobID:   job.ID,
            Cause:   err,
        }
    }

    return &scheduler.JobResult{
        Success: true,
        Message: fmt.Sprintf("Generated CRL with %d entries", crl.EntryCount),
    }, nil
}
```

### Configuration

#### QuicBaaS Configuration

Enable the scheduler in your QuicBaaS configuration:

```yaml
quicbaas:
  scheduler:
    enabled: true
    tick_interval: 1s
    max_concurrent_jobs: 10
    default_timeout: 5m
    default_retries: 3
    retry_backoff: 5s
```

#### Platform Configuration

Configure platform-specific scheduler settings:

```yaml
platform:
  scheduler:
    # CA-related jobs
    crl_generation:
      interval: 1h
      full_interval: 24h

    # Attestation jobs
    attestation_verify:
      interval: 5m
      batch_size: 100

    # Tenant maintenance
    quota_sync:
      interval: 15m
```

#### Programmatic Configuration

```go
config := &scheduler.SchedulerConfig{
    TickInterval:      time.Second,
    MaxConcurrentJobs: 10,
    DefaultTimeout:    5 * time.Minute,
    DefaultRetries:    3,
    RetryBackoff:      5 * time.Second,
}

db, err := quicbaas.Open(quicbaas.Config{
    EnableScheduler:          true,
    SchedulerTickInterval:    config.TickInterval,
    SchedulerMaxConcurrentJobs: config.MaxConcurrentJobs,
})
```

### Job Scheduling Examples

#### One-Shot Job (Execute Once)

```go
job := &scheduler.Job{
    Name:      "emergency-crl-regen",
    Type:      scheduler.JobTypeOneShot,
    Handler:   "ca.crl_generation",
    ExecuteAt: time.Now().Add(5 * time.Minute),
    Payload:   json.RawMessage(`{"full": true}`),
    Priority:  10, // High priority
}
err := sched.ScheduleJob(ctx, job)
```

#### Recurring Job (Cron Schedule)

```go
job := &scheduler.Job{
    Name:     "nightly-attestation-audit",
    Type:     scheduler.JobTypeRecurring,
    Handler:  "attestation.audit",
    Schedule: "0 2 * * *", // Daily at 2 AM
    Payload:  json.RawMessage(`{"scope": "full"}`),
}
err := sched.ScheduleJob(ctx, job)
```

#### Delayed Job (Execute After Duration)

```go
job := &scheduler.Job{
    Name:    "grace-period-expire",
    Type:    scheduler.JobTypeDelayed,
    Handler: "kms.grace_period",
    Delay:   7 * 24 * time.Hour, // 7 days from now
    Payload: json.RawMessage(`{"key_id": "mykey", "version": 1}`),
}
err := sched.ScheduleJob(ctx, job)
```

### Multi-Protocol API Access

The scheduler is accessible via all QuicBaaS protocols:

| Protocol | Use Case |
|----------|----------|
| REST | Web dashboards, external systems |
| gRPC | Service-to-service, high performance |
| QUIC | Low-latency, mobile clients |
| MCP | AI/LLM integration |
| Unix Socket | Local CLI, platformctl |

#### REST API Example

```bash
# Create a job
curl -X POST https://localhost:8443/api/v1/scheduler/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "name": "weekly-audit",
    "handler": "audit.cleanup",
    "schedule": "0 0 * * 0",
    "payload": {"retention_days": 90}
  }'

# List jobs
curl https://localhost:8443/api/v1/scheduler/jobs

# Trigger immediate execution
curl -X POST https://localhost:8443/api/v1/scheduler/jobs/123/trigger
```

#### CLI Example (platformctl)

```bash
# Create a job
platformctl scheduler job create \
  --name "crl-regen" \
  --handler "ca.crl_generation" \
  --schedule "@hourly" \
  --payload '{"full": false}'

# List jobs
platformctl scheduler job list

# Trigger a job
platformctl scheduler job trigger crl-regen

# View job history
platformctl scheduler job history crl-regen
```

### Watch Integration

Platform consumers can watch for job and key changes using QuicBaaS watches:

```go
// Watch for key status changes to trigger custom automation
watchCh, err := db.Watch(ctx, "kms/keys/", client.WithPrefix())
if err != nil {
    return err
}

for event := range watchCh {
    var key KeyInfo
    if err := json.Unmarshal(event.Value, &key); err != nil {
        continue
    }

    // Trigger re-encryption when key is disabled
    if key.Status == "disabled" {
        go triggerReEncryption(ctx, key.ID, key.Version)
    }
}
```

### Migration Guide

#### From Cron-Based Systems

If migrating from cron or systemd timers:

1. **Identify existing scheduled tasks**
   ```bash
   crontab -l | grep platform
   systemctl list-timers | grep platform
   ```

2. **Map to scheduler jobs**
   | Cron Expression | Scheduler Equivalent |
   |-----------------|---------------------|
   | `0 * * * *` | `Schedule: "0 * * * *"` |
   | `@daily` | `Schedule: "@daily"` |
   | `*/5 * * * *` | `Schedule: "*/5 * * * *"` |

3. **Create migration script**
   ```go
   func migrateCronJobs(sched scheduler.Scheduler) error {
       jobs := []scheduler.Job{
           {Name: "backup", Handler: "db.backup", Schedule: "0 2 * * *"},
           {Name: "cleanup", Handler: "audit.cleanup", Schedule: "0 3 * * 0"},
       }
       for _, job := range jobs {
           if err := sched.ScheduleJob(ctx, &job); err != nil {
               return err
           }
       }
       return nil
   }
   ```

4. **Disable old cron entries** after verification

#### From Custom Schedulers

If migrating from a custom scheduling solution:

1. **Implement handler adapters** for existing job logic
2. **Map job states** to scheduler states
3. **Migrate job data** to QuicBaaS storage format
4. **Update monitoring** to use scheduler metrics

### Monitoring and Observability

#### Scheduler Statistics

```go
stats := sched.Stats()
fmt.Printf("Jobs: %d, Running: %d, Success: %d, Failures: %d\n",
    stats.JobCount, stats.RunningJobs, stats.SuccessCount, stats.FailureCount)
```

#### Health Check Integration

```go
func schedulerHealthCheck(sched scheduler.Scheduler) health.Check {
    return func() error {
        if !sched.IsRunning() {
            return errors.New("scheduler not running")
        }
        stats := sched.Stats()
        if stats.FailureCount > threshold {
            return fmt.Errorf("high failure rate: %d", stats.FailureCount)
        }
        return nil
    }
}
```

#### Metrics Export

The scheduler exposes Prometheus-compatible metrics:

```
# HELP scheduler_jobs_total Total number of scheduled jobs
# TYPE scheduler_jobs_total gauge
scheduler_jobs_total 42

# HELP scheduler_jobs_running Currently running jobs
# TYPE scheduler_jobs_running gauge
scheduler_jobs_running 3

# HELP scheduler_executions_total Total job executions
# TYPE scheduler_executions_total counter
scheduler_executions_total{status="success"} 1234
scheduler_executions_total{status="failure"} 12
```

### Error Handling

#### Typed Errors

The scheduler uses typed errors for precise error handling:

```go
switch err := err.(type) {
case scheduler.ErrJobNotFound:
    // Job doesn't exist
case scheduler.ErrHandlerNotFound:
    // No handler registered for job type
case scheduler.ErrInvalidJob:
    // Job validation failed
    log.Printf("Invalid field: %s - %s", err.Field, err.Message)
case scheduler.ErrHandlerFailed:
    // Handler execution failed
    log.Printf("Handler %s failed: %v", err.Handler, err.Cause)
case scheduler.ErrSchedulerStopped:
    // Scheduler not running
default:
    // Unknown error
}
```

#### Retry Policies

Configure retry behavior per job:

```go
job := &scheduler.Job{
    Name:       "critical-task",
    Handler:    "platform.critical",
    MaxRetries: 5,
    Timeout:    10 * time.Minute,
    // RetryBackoff uses exponential backoff by default
}
```

### Best Practices

1. **Use meaningful job names** - Include context like `tenant-{id}-cleanup`
2. **Set appropriate timeouts** - Prevent runaway jobs
3. **Implement idempotent handlers** - Jobs may be retried
4. **Use typed errors** - Enable precise error handling
5. **Log job execution** - Include job ID in all log entries
6. **Monitor failure rates** - Alert on elevated failures
7. **Test handlers independently** - Unit test before integration
8. **Use dry-run modes** - Validate destructive operations first

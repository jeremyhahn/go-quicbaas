# Scheduler Integration Guide

## Overview

This guide documents how to integrate the QuicBaaS scheduler into consuming applications like go-trusted-platform. The scheduler provides a unified job scheduling system that can be extended with platform-specific handlers.

## Architecture

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

## Integration Patterns

### Pattern 1: Import and Extend

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

### Pattern 2: Handler Registry

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

### Pattern 3: Service Layer Adapter

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

## Creating Custom Handlers

### Handler Interface

All handlers must implement the `Handler` interface:

```go
type Handler interface {
    // Name returns the unique handler identifier
    Name() string

    // Handler returns the job execution function
    Handler() scheduler.JobHandler
}
```

### Example: CRL Generation Handler

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

## Configuration

### QuicBaaS Configuration

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

### Platform Configuration

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

### Programmatic Configuration

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

## Job Scheduling Examples

### One-Shot Job (Execute Once)

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

### Recurring Job (Cron Schedule)

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

### Delayed Job (Execute After Duration)

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

## Multi-Protocol API Access

The scheduler is accessible via all QuicBaaS protocols:

| Protocol | Use Case |
|----------|----------|
| REST | Web dashboards, external systems |
| gRPC | Service-to-service, high performance |
| QUIC | Low-latency, mobile clients |
| MCP | AI/LLM integration |
| Unix Socket | Local CLI, platformctl |

### REST API Example

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

### CLI Example (platformctl)

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

## Watch Integration

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

## Migration Guide

### From Cron-Based Systems

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

### From Custom Schedulers

If migrating from a custom scheduling solution:

1. **Implement handler adapters** for existing job logic
2. **Map job states** to scheduler states
3. **Migrate job data** to QuicBaaS storage format
4. **Update monitoring** to use scheduler metrics

## Monitoring and Observability

### Scheduler Statistics

```go
stats := sched.Stats()
fmt.Printf("Jobs: %d, Running: %d, Success: %d, Failures: %d\n",
    stats.JobCount, stats.RunningJobs, stats.SuccessCount, stats.FailureCount)
```

### Health Check Integration

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

### Metrics Export

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

## Error Handling

### Typed Errors

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

### Retry Policies

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

## Best Practices

1. **Use meaningful job names** - Include context like `tenant-{id}-cleanup`
2. **Set appropriate timeouts** - Prevent runaway jobs
3. **Implement idempotent handlers** - Jobs may be retried
4. **Use typed errors** - Enable precise error handling
5. **Log job execution** - Include job ID in all log entries
6. **Monitor failure rates** - Alert on elevated failures
7. **Test handlers independently** - Unit test before integration
8. **Use dry-run modes** - Validate destructive operations first

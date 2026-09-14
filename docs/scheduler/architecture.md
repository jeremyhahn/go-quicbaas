# Scheduler — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/scheduler/`.

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


---

## Scheduler — Cluster Dispatch Semantics

Scheduler jobs in go-quicbaas are stored on the Platform shard
(`PlatformShardID = 1`) which is Raft-replicated in cluster
deployments. Every node runs its own `SchedulerEngine` and polls the
same replicated storage. Without a dispatch gate, every due job
would fire on every node once per tick — correct only if the handler
is idempotent.

This page documents the gate introduced in
`pkg/scheduler/scheduler.go:NewWithClustering`.

### The gate

Two per-engine settings, both optional:

| Engine field | Source | Role |
|---|---|---|
| `nodeID string` | `NewWithClustering` arg | This node's identity for `Job.NodeAffinity` matching |
| `leader LeaderChecker` | `NewWithClustering` arg | Raft leader introspection for cluster-wide jobs |

And one per-job field:

| `Job.NodeAffinity` | Meaning |
|---|---|
| `""` | Cluster-wide job. Executes iff this node is leader. |
| `"X"` | Node-affine job. Executes iff `nodeID == "X"`. Leader status irrelevant. |

### Behavior matrix

| `nodeID` | `leader` | Cluster-wide job (`Affinity==""`) | Affine job (`Affinity=="X"`) |
|---|---|---|---|
| `""` | nil | always fires | always fires |
| `"N"` | nil | always fires | fires iff `X == N` |
| `""` | non-nil | fires iff `IsLeader() == true` | falls through to leader gate |
| `"N"` | non-nil | fires iff `IsLeader() == true` | fires iff `X == N` (leader status ignored) |

Empty `nodeID` + nil `leader` (the backward-compatible default from
`scheduler.New`) preserves the pre-clustering behavior: all jobs fire,
every tick, on every node.

### Why node-affinity bypasses the leader gate

For workloads that depend on node-local state — certificate private
keys, local TLS config, BMC credentials in the local secret store —
the affine node is the only node that *can* perform the work. If
that node happened to be a follower, making it wait for leadership
would mean the work never runs. So:

> Affine jobs run on the affinity-matching node regardless of
> leadership. Cluster-wide jobs run only on the leader.

### Why the leader gate is belt-and-suspenders for cluster-wide jobs

Without the leader gate, N nodes all observe the same due job, all
call `UpdateJob(Running)`, and all invoke the handler. Raft
serializes the writes, so the jobs' storage state becomes consistent,
but the handler ran N times. Existing quicbaas handlers
(`kms_grace`, `audit_cleanup`, `db_compaction`) tolerate this
because they're idempotent — setting a flag twice, deleting
already-deleted records, etc.

ACME cert issuance is **not** idempotent in that sense: each call
consumes ACME rate-limit budget and generates a fresh keypair.
Leader-gating prevents the waste.

### Leader churn, and why the gate is not enough

The leader gate decides who *may* fire. It does not decide who
*does*, and the difference is the whole reason the firing claim
exists.

During a raft transition a brief window exists where two nodes each
believe they lead: the deposed leader has not yet learned otherwise
while its successor is already elected. Both answer `IsLeader()`
truthfully-as-they-see-it for the same tick, both pass the gate, and
both dispatch the same job. The engine's per-job stripe mutex cannot
help — the two nodes are different processes on different hosts.

**Every due job is therefore also arbitrated by a firing claim.**

### The firing claim

Each job carries one **monotonic high-water mark** at
`__sys:scheduler/claims/{scope}/{jobID}` — where `{scope}` is `sys:` or
`t:{tenantID}:`, the same discriminator job keys carry, because job IDs are
unique only within a scope, holding the latest firing any node
has claimed. Claiming firing *T* is one of:

| State | Guard | Outcome |
|---|---|---|
| No mark yet | `CompareCreateRevision == 0` | created ⇒ won |
| Mark below *T* | `CompareValue` on the bytes just read | advanced ⇒ won |
| Mark at or past *T* | none — no proposal is made | lost |

Each write is a single `ExecuteTransaction` — one raft proposal whose
compare and write are evaluated together inside the state machine — so
arbitration is atomic by construction and independent of seal and
barrier configuration. The claim's identity is the job's own `NextRun`,
read from the replicated job record rather than from any local clock,
so contending nodes derive byte-identical identities.

A failed compare is a **loss, never a retry**: the loser's re-read
would only show the mark now at or past that firing.

Three properties worth stating because they were each chosen against
an alternative that looks reasonable:

- **A mark per job, not a record per firing.** Per-firing records must
  be pruned or grow forever, and pruning un-decides a decided firing:
  a contender that read *T* and then stalled finds *T*'s key gone,
  wins it again, and runs it twice. The mark makes a stale contender
  lose by comparison instead of by key presence, and bounds growth at
  one record per job — so there is nothing to prune.
- **`CompareValue` is safe here only because the mark is monotonic.**
  A value once written can never recur, so it cannot return to bytes a
  stale reader still holds. Break the monotonicity and this guard must
  become a revision compare.
- **`__sys:`, not `sys:`.** go-qrdb excludes that namespace from
  user-facing List/Scan and from MVCC restore, so arbitration state is
  not enumerable by a tenant prefix sweep. It is a discovery guard, not
  a confidentiality boundary and not a write ACL.

The one cost, and it is real: the mark only moves forward, so a job
whose `NextRun` is ever moved *below* its mark becomes unclaimable.
That errs toward a miss rather than a duplicate. `TriggerJob` bypasses
the claim and is the operator's recovery; `StrandedFirings()` counts
the occurrences.

### Construction is fail-closed

A **clustered** boot builds `NewWithClustering` over a
`ClusteredStorage`, and refuses to start without both the leader
checker and the claim arbiter. Neither is useful alone: without the
checker every node fires every cluster-wide job on every tick; without
the arbiter two nodes fire it during churn. They travel as one value,
so the incomplete combination cannot be constructed.

A **standalone** boot builds neither, and is unchanged. That is a
different topology, not a disabled safeguard: one scheduler has no
peer to race.

Leadership is queried live per check, never cached — a cached bool is
how a deposed leader goes on believing it leads.

### Wire-protocol exposure

`Job.NodeAffinity` is exposed on:

- `pkg/scheduler/types.go` — persisted JSON field `node_affinity`
- `pkg/service/scheduler_service.go` `CreateSchedulerJobRequest`
- `sdk/go/client/scheduler` `CreateJobRequest`
- `api/proto/scheduler.proto` `CreateJobRequest.node_affinity` (field 16)
- `pkg/api/grpc/server_scheduler.go` — wired in handler
- `pkg/api/rest/handlers_scheduler.go` — wired in handler (`node_affinity`)
- `pkg/api/mcp/scheduler_tools.go` — wired in tool schema + handler

External clients can schedule node-affine jobs via all exposed
protocols.

### Example: platformd cert renewal

Platformd on each node creates one job:

```go
sched.CreateJob(ctx, &scheduler.CreateJobRequest{
    Name:         "platform.cert-renewal." + nodeID,
    Handler:      "platform.cert-renewal",
    JobType:      scheduler.JobTypeRecurring,
    Schedule:     "0 2 * * *",
    NodeAffinity: nodeID,
    OwnerType:    scheduler.OwnerTypeSystem,
    OwnerID:      nodeID,
})
```

In a 3-node cluster there are three jobs (one per node, distinct
names), each firing only on its owning node via the affinity gate.
No two nodes ever race on the same cert renewal.

See `go-trusted-platform/docs/architecture/bmc-tls.md` for the
end-to-end cert lifecycle that rides on this scheduler pattern.

### Construction cheat sheet

```go
// Single-node / test mode — no clustering:
engine := scheduler.New(config, storage)

// Cluster deployment:
engine := scheduler.NewWithClustering(config, storage, "node-A", raftChecker)

// Embedded from quicbaas service (platformd):
engine := svc.SchedulerEngine()   // raw engine for RegisterHandler, etc.
svc.Scheduler()                    // service-level surface (no RegisterHandler)
```

`LeaderChecker` is a one-method interface the caller implements
against whatever raft client is available:

```go
type LeaderChecker interface {
    IsLeader() (bool, error)
}
```

Wire in by satisfying this against the platform's existing raft
accessor (e.g., `server.IsLeader(shards.PlatformShardID)` for
qrdb-backed deployments).

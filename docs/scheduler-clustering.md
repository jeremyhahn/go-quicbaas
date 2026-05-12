# Scheduler — Cluster Dispatch Semantics

Scheduler jobs in go-quicbaas are stored on the Platform shard
(`PlatformShardID = 1`) which is Raft-replicated in cluster
deployments. Every node runs its own `SchedulerEngine` and polls the
same replicated storage. Without a dispatch gate, every due job
would fire on every node once per tick — correct only if the handler
is idempotent.

This page documents the gate introduced in
`pkg/scheduler/scheduler.go:NewWithClustering`.

## The gate

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

## Behavior matrix

| `nodeID` | `leader` | Cluster-wide job (`Affinity==""`) | Affine job (`Affinity=="X"`) |
|---|---|---|---|
| `""` | nil | always fires | always fires |
| `"N"` | nil | always fires | fires iff `X == N` |
| `""` | non-nil | fires iff `IsLeader() == true` | falls through to leader gate |
| `"N"` | non-nil | fires iff `IsLeader() == true` | fires iff `X == N` (leader status ignored) |

Empty `nodeID` + nil `leader` (the backward-compatible default from
`scheduler.New`) preserves the pre-clustering behavior: all jobs fire,
every tick, on every node.

## Why node-affinity bypasses the leader gate

For workloads that depend on node-local state — certificate private
keys, local TLS config, BMC credentials in the local secret store —
the affine node is the only node that *can* perform the work. If
that node happened to be a follower, making it wait for leadership
would mean the work never runs. So:

> Affine jobs run on the affinity-matching node regardless of
> leadership. Cluster-wide jobs run only on the leader.

## Why the leader gate is belt-and-suspenders for cluster-wide jobs

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

## Leader churn edge case

A brief window exists where two nodes each think they're leader
(raft transition). In that window, both could fire the same
cluster-wide job. Consequences:

- Two ACME orders, wasted rate-limit budget
- Two concurrent handler invocations — which then race on storage
  writes; raft serializes but application state may diverge

Current implementation **does not CAS-claim jobs**. The leader
gate alone is relied upon. Acceptable for the current handler set
because:

1. Leader transitions are rare
2. Transition windows are measured in low seconds
3. Worst-case impact is a duplicate handler run that wastes budget
   but does not corrupt state (handlers are still tolerant by
   original design)

If a future handler class surfaces as intolerant, the fix is to add
`UpdateJobIfStatus` CAS claim semantics — the groundwork is
preserved in `docs/scheduler-clustering.md#future-hardening`.

## Future hardening

| Option | Effect | Scope |
|---|---|---|
| CAS claim on UpdateJob | Only one node ever transitions `Pending → Running` | ~30 lines on storage + engine |
| Lease-based claim with TTL | Crash-tolerant CAS | Larger storage API change |
| Job hash → shard routing | Deterministic node selection | Cluster coordination change |

None are wired today. The node-affinity path plus leader-gate
combination covers the current handler set + the BMC cert renewal
pattern.

## Wire-protocol exposure

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

## Example: platformd cert renewal

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

## Construction cheat sheet

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

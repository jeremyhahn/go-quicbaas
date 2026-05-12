# Auto-Spawn Tasks

Auto-spawn is a task mode that immediately dispatches a job to an available agent, bypassing the manual queue. Paired with job-to-task status mirroring, it enables lightweight fire-and-forget work patterns.

## Two Task Modes

Tasks can run in one of two modes:

| Mode | Behavior | Use Case |
|------|----------|----------|
| **Queued** (default) | Task waits in queue until an agent claims it | Interactive workflows, multi-step orchestration |
| **Auto-spawn** | Task immediately dispatches as a spawned job | One-off commands, parallel batch work |

## Auto-Spawn Data Model

From `pkg/orchestration/orchestrator_types.go`:

```go
type Task struct {
    // ... other fields ...
    
    // AutoSpawn, when true, instructs the orchestrator to spawn a job for
    // this task immediately rather than queue it for claim.
    AutoSpawn bool `json:"auto_spawn,omitempty"`
    
    // SpawnedJobID is populated when a task is auto-spawned. Ignored when
    // AutoSpawn is false.
    SpawnedJobID string `json:"spawned_job_id,omitempty"`
    
    // SpawnedAt records when the task's job was dispatched.
    SpawnedAt *time.Time `json:"spawned_at,omitempty"`
}
```

## Dispatch Flow

When the scheduler encounters an auto-spawn task:

1. **Scheduler runs** (interval-based, default 5s)
2. **Filters** matching agents:
   - Host affinity (if set, filter to that host)
   - GPU requirement (if set, filter to GPU-capable agents)
3. **Picks an agent** from the filtered set (round-robin or by load)
4. **Calls `StartJob`** on the selected agent
5. **Populates `SpawnedJobID`** with the spawned job ID
6. **Task status** changes to `dispatched`

### Failure Modes

If no matching agent is available:

- The task is persisted with `SpawnedJobID` empty
- Status remains `queued`
- Scheduler retries on next interval
- No error is logged (graceful degradation)

If all matching agents are unavailable and `HostAffinityStrict=true`:

- Task transitions to `failed` status
- Error reason: `NoMatchingAgentError`

## Status Mirroring

When a spawned job completes or fails, the parent task's status is automatically updated:

| Spawned Job Status | Task Status |
|--------------------|-------------|
| `succeeded` | `succeeded` |
| `failed` | `failed` |
| `cancelled` | `cancelled` |

This mirroring happens via the job event stream. The orchestrator subscribes to job completion events and flips the parent task's status accordingly.

## CLI Usage

Create and auto-spawn a task immediately:

```bash
# Create and dispatch task to run "fix bug" on an available agent
quicbaas task create \
  --subject "fix bug" \
  --auto-spawn \
  --backend ollama \
  --gpu

# With host affinity (strict)
quicbaas task create \
  --subject "process shard 42" \
  --auto-spawn \
  --host-affinity gpu-host-02 \
  --host-affinity-strict

# List only auto-spawned tasks
quicbaas task list --filter auto-spawned

# List queued (non-spawned) tasks
quicbaas task list --filter queued

# List all tasks regardless of spawn mode
quicbaas task list --filter all
```

## UI Integration

The QuicBaaS UI (Svelte) includes auto-spawn controls:

### CreateTaskDialog

- **"Run immediately"** toggle switches between queued and auto-spawn modes
- When enabled, task dispatches on submit

### TasksView

- **Filter buttons**: "All", "Auto-spawned", "Queued"
- **"Spawned Job" column** shows the dispatched job ID (if any)
- Click job ID to navigate to job detail view

## REST API

### Create Task with Auto-Spawn

```
POST /api/v1/tasks
Content-Type: application/json

{
  "subject": "fix bug",
  "auto_spawn": true,
  "backend": "ollama",
  "gpu_required": true
}

201 Created
{
  "id": "task-uuid",
  "subject": "fix bug",
  "auto_spawn": true,
  "spawned_job_id": "job-uuid",
  "spawned_at": "2025-04-26T12:00:00Z",
  "status": "dispatched"
}
```

### List Tasks with Filter

```
GET /api/v1/tasks?filter=auto-spawned&project_id=proj-uuid

200 OK
{
  "tasks": [
    {
      "id": "task-1",
      "auto_spawn": true,
      "spawned_job_id": "job-1",
      "status": "dispatched"
    },
    ...
  ],
  "total": 42
}
```

Valid filter values: `all`, `auto-spawned`, `queued`.

## gRPC API

```protobuf
rpc CreateTask(CreateTaskRequest) returns (CreateTaskResponse) {}

message CreateTaskRequest {
    string subject = 1;
    bool auto_spawn = 2;              // Enable immediate dispatch
    string backend = 3;               // "ollama", "claude", etc.
    bool gpu_required = 4;            // GPU-only constraint
    string host_affinity = 5;         // Preferred host
    bool host_affinity_strict = 6;    // Fail if unavailable
}

message CreateTaskResponse {
    string task_id = 1;
    string spawned_job_id = 2;        // Populated if auto_spawn=true
    int64 spawned_at_unix = 3;
}

rpc ListTasks(ListTasksRequest) returns (ListTasksResponse) {}

message ListTasksRequest {
    string filter = 1;                // "all", "auto-spawned", "queued"
    string project_id = 2;            // Filter by project
}
```

## Service Implementation

The `TaskService` in `pkg/service/task_service.go` handles auto-spawn logic:

1. **On create**: If `AutoSpawn=true`, mark task for immediate dispatch
2. **Scheduler run**: For each auto-spawn task, execute the dispatch flow above
3. **On job completion**: Trigger status mirror via event listener
4. **Error handling**: Log retry attempts without blocking; use exponential backoff

All mutations are audited. See `docs/security/audit.md` for event types.

## Configuration

Auto-spawn behavior is tuned in `Config.Orchestration`:

```go
type OrchestrationConfig struct {
    // ... other fields ...
    
    // TaskSchedulerInterval is how often the scheduler checks for
    // auto-spawn tasks and other work. Default: 5 seconds.
    TaskSchedulerInterval time.Duration
    
    // AutoSpawnRetryMax is the maximum number of retry attempts before
    // giving up on an auto-spawn task. Default: 10.
    AutoSpawnRetryMax int
    
    // AutoSpawnRetryBackoff is the backoff strategy between retries.
    AutoSpawnRetryBackoff time.Duration
}
```

## Cross-References

- Sub-job spawning: `docs/orchestration/sub-jobs.md`
- Job status lifecycle: `docs/orchestration/state-machine.md`
- Scheduler architecture: `docs/scheduler/architecture.md`
- Host affinity: `docs/orchestration/multi-host.md`

## Source Files

- `pkg/orchestration/orchestrator_types.go:783-792` — Task auto-spawn fields
- `pkg/orchestration/orchestrator.go` — Scheduler and dispatch logic
- `pkg/orchestration/auto_spawn_test.go` — Usage examples and tests
- `pkg/api/rest/handlers_orchestrator.go` — REST endpoints
- `pkg/api/grpc/server_orchestrator.go` — gRPC endpoints

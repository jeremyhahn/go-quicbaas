# Sub-Job Spawning & Typed Artifacts

Sub-job spawning enables massively parallel agentic fan-out across machines. A lead agent in one job spawns multiple child jobs, each potentially running on different hosts with their own task graphs, and coordinates results via typed artifacts.

## Motivation

Traditional sequential workflows scale poorly when a task requires parallel exploration or distributed work. Sub-jobs let a lead agent:

- Spawn N independent job instances from within a running task
- Assign each to a different agent/host (based on affinity + availability)
- Let each solve a sub-problem in parallel
- Gather results via artifacts (commits, KV entries, files)

Example: "Build a trading system" → lead spawns three child jobs ("API service", "data pipeline", "analytics engine") in parallel, each delegated to a different specialized agent.

## Data Model

Job parent-child relationship is tracked in `pkg/orchestration/orchestrator_types.go`:

```go
type Job struct {
    // ... other fields ...
    
    // ParentJobID is empty for root jobs; set to the spawning job's ID for children.
    ParentJobID string `json:"parent_job_id,omitempty"`
    
    // ChildJobIDs accumulates IDs of jobs spawned by this job.
    // Appended atomically as children are created.
    ChildJobIDs []string `json:"child_job_ids,omitempty"`
}

type SpawnChildJobRequest struct {
    ParentJobID       string   // required: job whose lead agent is spawning
    WorkflowID        string   // required: which workflow to run for the child
    ProjectID         string   // required: project context
    Tasks             []Task   // required: initial task graph for the child
    HostAffinity      string   // optional: preferred host
    GPURequired       bool     // optional: GPU machine only
    HostAffinityStrict bool    // optional: fail if affinity not available
}
```

## Spawning a Child Job

Only the **registered lead agent** of `parent_job_id` may call `SpawnChildJob`. This is enforced via mTLS-bound OAuth2 scope checks. The orchestrator verifies:

1. Caller's certificate matches the lead agent ID
2. Lead agent has valid OAuth2 token scoped to `orchestrator:write`
3. Parent job exists and is in a valid state (not failed, not finished)

### gRPC RPC

```protobuf
rpc SpawnChildJob(SpawnChildJobRequest) returns (SpawnChildJobResponse) {}

message SpawnChildJobRequest {
    string parent_job_id = 1;           // UUID of the parent job
    string workflow_id = 2;             // workflow template to run
    string project_id = 3;              // project owning the parent job
    repeated Task tasks = 4;            // initial task graph
    string host_affinity = 5;           // preferred agent host (optional)
    bool gpu_required = 6;              // GPU-only constraint (optional)
    bool host_affinity_strict = 7;      // fail if affinity unavailable (optional)
}

message SpawnChildJobResponse {
    string job_id = 1;                  // UUID of newly spawned child job
    string parent_job_id = 2;           // echoed from request
    int64 spawned_at_unix = 3;          // creation timestamp
}
```

### Client Code Example

From a running task (agent-side), spawn a child job:

```go
// Inside an agent's task handler
resp, err := client.SpawnChildJob(ctx, &pb.SpawnChildJobRequest{
    ParentJobID:       parentJobID,
    WorkflowID:        "data-processing",
    ProjectID:         projectID,
    Tasks: []*pb.Task{
        {
            Subject:        "Process shard 1",
            SubjectDetails: "Process data shard from row 0-1000",
            RequiredBackend: "claude",
        },
    },
    HostAffinity:       "gpu-host-02",
    GPURequired:        true,
    HostAffinityStrict: false,
})
if err != nil {
    return fmt.Errorf("spawn child: %w", err)
}
childJobID := resp.JobID
```

## Artifacts

Artifacts are typed outputs/inputs passed between tasks and jobs. Five artifact kinds support different storage backends:

| Kind | Storage | Use Case |
|------|---------|----------|
| `inline` | Task record | Small blobs (≤ 1 MiB) |
| `file` | Agent filesystem | Large files (path + agent_id) |
| `commit` | Git repository | Code snapshots (SHA + branch) |
| `kv_key` | QuicBaaS KV store | Auto-promoted large inlines |
| `url` | External (S3, HTTP) | Third-party resources |

### Artifact Data Model

From `pkg/orchestration/artifact.go`:

```go
type ArtifactKind string

const (
    ArtifactKindInline  ArtifactKind = "inline"
    ArtifactKindFile    ArtifactKind = "file"
    ArtifactKindCommit  ArtifactKind = "commit"
    ArtifactKindKVKey   ArtifactKind = "kv_key"
    ArtifactKindURL     ArtifactKind = "url"
)

type Artifact struct {
    Kind     ArtifactKind   // Required: kind classifier
    Name     string         // Required: unique within task
    MIMEType string         // Optional: e.g., "text/plain"
    Size     int64          // Byte count
    Content  []byte         // Inline bytes when Kind=inline
    Ref      ArtifactRef    // For non-inline kinds
}

type ArtifactRef struct {
    AgentID string // For file-kind: agent owning the file
    Path    string // Filesystem path, KV key, or full URL
    SHA     string // Content hash or git commit SHA
    Branch  string // Git branch (commit-kind only)
}
```

### Inline Limit & Auto-Promotion

By default, artifacts larger than 1 MiB are automatically promoted from `inline` to `kv_key` storage:

```go
const InlineLimit int64 = 1 << 20 // 1 MiB
```

This is configurable at orchestration service setup:

```go
cfg := &orchestration.Config{
    Artifacts: orchestration.ArtifactConfig{
        InlineLimit: 5 << 20, // 5 MiB
    },
}
```

When an inline artifact exceeds the limit, the orchestrator:

1. Stores content in KV under a project-scoped key
2. Updates the artifact record: `Kind=kv_key`, clears `Content`, sets `Ref.Path`
3. Task record size remains bounded

### Artifact REST API

```
GET /api/v1/tasks/{task_id}/artifacts
200 OK
[
  {
    "name": "analysis-result",
    "kind": "inline",
    "mime_type": "application/json",
    "size": 2048,
    "content": "..."
  },
  {
    "name": "compiled-binary",
    "kind": "file",
    "mime_type": "application/octet-stream",
    "size": 45678901,
    "ref": {
      "agent_id": "agent-prod-01",
      "path": "/tmp/output/binary"
    }
  }
]

GET /api/v1/tasks/{task_id}/artifacts/{name}
200 OK
{
  "name": "analysis-result",
  ...
}
```

### Artifact MCP Tools

Two tools expose artifact operations:

| Tool | Purpose |
|------|---------|
| `task_list_artifacts` | List all artifacts for a task |
| `task_get_artifact` | Retrieve a single artifact by name |

Both enforce project-level authorization. Inline artifacts return full content; non-inline return references.

## Cross-Machine Handoff Pattern

1. **Parent job (lead)** spawns child job on remote host
2. **Child agent** executes tasks, produces commit artifact (e.g., Git push to `feature/child-work`)
3. **Parent agent** reads commit artifact reference from child's completed task
4. **Parent agent** uses SHA to fetch code context on next phase (post-rebase merge, testing, etc.)

Example sequence:

```
Parent Job (Host A)
  ├─ Spawn Child Job → "Implement API endpoints" (Host B)
  │   └─ Child Job completes, produces Artifact(name="code", kind="commit", sha="abc123...")
  │
  └─ Parent reads Artifact(name="code") from child task
      └─ Gets ref.SHA = "abc123", checks out and integrates code
```

## Service Implementation

The `SpawnChildJob` RPC is implemented in `pkg/orchestration/orchestrator.go`. The service:

1. Validates caller is the registered lead agent (via mTLS OAuth2 scope)
2. Checks parent job state (must be running or waiting)
3. Runs scheduler to pick a matching agent (affinity + GPU filter)
4. Creates child job record with `ParentJobID` set
5. Appends child job ID to parent's `ChildJobIDs` slice
6. Returns child job ID to caller

Error scenarios:

- `NoMatchingAgentError`: scheduler found no available agent matching constraints
- `InvalidParentJobError`: parent job not found or in terminal state
- `UnauthorizedError`: caller is not the lead agent (mTLS/OAuth2 mismatch)

## Source Files

- `pkg/orchestration/orchestrator_types.go:891-894, 969-970` — Job parent-child fields, SpawnChildJobRequest
- `pkg/orchestration/artifact.go` — Artifact types and kinds
- `pkg/orchestration/orchestrator.go` — SpawnChildJob implementation
- `pkg/api/grpc/server_orchestrator.go` — gRPC RPC handler
- `pkg/api/rest/handlers_orchestrator.go` — REST endpoint wrapper
- `pkg/orchestration/spawn_child_job_test.go` — Usage examples and tests

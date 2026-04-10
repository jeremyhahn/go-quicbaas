# Scheduler gRPC API Reference

## Service Definition

```protobuf
package quicbaas.scheduler.v1;

service SchedulerService {
    // Job Management
    rpc CreateJob(CreateJobRequest) returns (CreateJobResponse);
    rpc GetJob(GetJobRequest) returns (GetJobResponse);
    rpc UpdateJob(UpdateJobRequest) returns (UpdateJobResponse);
    rpc DeleteJob(DeleteJobRequest) returns (DeleteJobResponse);
    rpc ListJobs(ListJobsRequest) returns (ListJobsResponse);

    // Job Control
    rpc TriggerJob(TriggerJobRequest) returns (TriggerJobResponse);
    rpc PauseJob(PauseJobRequest) returns (PauseJobResponse);
    rpc ResumeJob(ResumeJobRequest) returns (ResumeJobResponse);
    rpc CancelJob(CancelJobRequest) returns (CancelJobResponse);

    // History and Status
    rpc GetJobHistory(GetJobHistoryRequest) returns (GetJobHistoryResponse);
    rpc GetSchedulerStatus(GetSchedulerStatusRequest) returns (GetSchedulerStatusResponse);
}
```

## Connection

| Property | Value |
|----------|-------|
| Host | `localhost` |
| Port | `10443` |
| TLS | Required |

## Authentication

Pass authentication token in gRPC metadata:

```go
md := metadata.New(map[string]string{
    "authorization": "Bearer <jwt-token>",
})
ctx := metadata.NewOutgoingContext(context.Background(), md)
```

## Enums

### JobType

```protobuf
enum JobType {
    JOB_TYPE_UNSPECIFIED = 0;
    JOB_TYPE_ONE_SHOT = 1;    // Execute once at specific time
    JOB_TYPE_RECURRING = 2;   // Execute on cron schedule
    JOB_TYPE_DELAYED = 3;     // Execute after delay
}
```

### JobStatus

```protobuf
enum JobStatus {
    JOB_STATUS_UNSPECIFIED = 0;
    JOB_STATUS_PENDING = 1;    // Awaiting execution
    JOB_STATUS_RUNNING = 2;    // Currently executing
    JOB_STATUS_COMPLETED = 3;  // Successfully finished
    JOB_STATUS_FAILED = 4;     // Execution failed
    JOB_STATUS_PAUSED = 5;     // Temporarily suspended
    JOB_STATUS_CANCELED = 6;   // Permanently stopped
}
```

### OwnerType

```protobuf
enum OwnerType {
    OWNER_TYPE_UNSPECIFIED = 0;
    OWNER_TYPE_SYSTEM = 1;     // Internal system jobs
    OWNER_TYPE_TENANT = 2;     // User-created tenant jobs
    OWNER_TYPE_DELEGATED = 3;  // System job for tenant data
}
```

## Messages

### Job

```protobuf
message Job {
    uint64 id = 1;
    string name = 2;
    string description = 3;
    OwnerType owner_type = 4;
    string owner_id = 5;
    string target_tenant_id = 6;
    string created_by = 7;
    JobType type = 8;
    string schedule = 9;           // Cron expression
    int64 execute_at = 10;         // Unix timestamp
    int64 delay_seconds = 11;
    string handler = 12;
    bytes payload = 13;
    int32 priority = 14;
    int64 timeout_seconds = 15;
    int32 max_retries = 16;
    JobStatus status = 17;
    int64 last_run = 18;           // Unix timestamp
    int64 next_run = 19;           // Unix timestamp
    uint64 run_count = 20;
    uint64 fail_count = 21;
    string last_error = 22;
    map<string, string> labels = 23;
    int64 created_at = 24;         // Unix timestamp
    int64 updated_at = 25;         // Unix timestamp
}
```

### JobExecution

```protobuf
message JobExecution {
    uint64 id = 1;
    uint64 job_id = 2;
    JobStatus status = 3;
    int64 started_at = 4;          // Unix timestamp
    int64 ended_at = 5;            // Unix timestamp
    int64 duration_ms = 6;
    string error = 7;
    string output = 8;
}
```

### SchedulerStats

```protobuf
message SchedulerStats {
    uint64 job_count = 1;
    uint64 running_jobs = 2;
    uint64 success_count = 3;
    uint64 failure_count = 4;
    int64 last_tick_time = 5;      // Unix timestamp
    int64 started_at = 6;          // Unix timestamp
}
```

## RPC Methods

### CreateJob

Create a new scheduled job.

```protobuf
message CreateJobRequest {
    string name = 1;
    string description = 2;
    OwnerType owner_type = 3;
    string owner_id = 4;
    string target_tenant_id = 5;
    JobType type = 6;
    string schedule = 7;
    int64 execute_at = 8;
    int64 delay_seconds = 9;
    string handler = 10;
    bytes payload = 11;
    int32 priority = 12;
    int64 timeout_seconds = 13;
    int32 max_retries = 14;
    map<string, string> labels = 15;
}

message CreateJobResponse {
    Job job = 1;
    int64 timestamp = 2;
}
```

**Example (Go):**

```go
req := &proto.CreateJobRequest{
    Name:      "daily-backup",
    OwnerType: proto.OWNER_TYPE_SYSTEM,
    Type:      proto.JOB_TYPE_RECURRING,
    Schedule:  "0 2 * * *",
    Handler:   "backup.full",
    Priority:  10,
    Labels:    map[string]string{"env": "production"},
}

resp, err := client.CreateJob(ctx, req)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Created job: %d\n", resp.Job.Id)
```

---

### GetJob

Retrieve job details by ID.

```protobuf
message GetJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
}

message GetJobResponse {
    Job job = 1;
    int64 timestamp = 2;
}
```

**Example (Go):**

```go
resp, err := client.GetJob(ctx, &proto.GetJobRequest{
    JobId: 12345,
})
```

---

### UpdateJob

Update an existing job.

```protobuf
message UpdateJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
    string name = 3;
    string description = 4;
    string schedule = 5;
    int64 execute_at = 6;
    string handler = 7;
    bytes payload = 8;
    int32 priority = 9;
    int64 timeout_seconds = 10;
    int32 max_retries = 11;
    map<string, string> labels = 12;
}

message UpdateJobResponse {
    Job job = 1;
    int64 timestamp = 2;
}
```

---

### DeleteJob

Delete a scheduled job.

```protobuf
message DeleteJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
    bool force = 3;
}

message DeleteJobResponse {
    uint64 job_id = 1;
    bool deleted = 2;
    int64 timestamp = 3;
}
```

---

### ListJobs

List jobs with optional filtering.

```protobuf
message ListJobsRequest {
    OwnerType owner_type = 1;
    string owner_id = 2;
    string target_tenant_id = 3;
    JobStatus status = 4;
    string handler = 5;
    map<string, string> labels = 6;
    int32 limit = 7;
    string cursor = 8;
}

message ListJobsResponse {
    repeated Job jobs = 1;
    string next_cursor = 2;
    bool has_more = 3;
    int32 total_count = 4;
    int64 timestamp = 5;
}
```

**Example (Go):**

```go
resp, err := client.ListJobs(ctx, &proto.ListJobsRequest{
    Status:  proto.JOB_STATUS_PENDING,
    Handler: "db.compaction",
    Limit:   50,
})

for _, job := range resp.Jobs {
    fmt.Printf("Job: %s (ID: %d)\n", job.Name, job.Id)
}
```

---

### TriggerJob

Execute job immediately.

```protobuf
message TriggerJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
}

message TriggerJobResponse {
    uint64 job_id = 1;
    JobStatus status = 2;
    int64 timestamp = 3;
}
```

---

### PauseJob

Pause scheduled execution.

```protobuf
message PauseJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
}

message PauseJobResponse {
    uint64 job_id = 1;
    JobStatus old_status = 2;
    JobStatus new_status = 3;
    int64 timestamp = 4;
}
```

---

### ResumeJob

Resume paused job.

```protobuf
message ResumeJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
}

message ResumeJobResponse {
    uint64 job_id = 1;
    JobStatus old_status = 2;
    JobStatus new_status = 3;
    int64 timestamp = 4;
}
```

---

### CancelJob

Cancel job permanently.

```protobuf
message CancelJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
    string reason = 3;
}

message CancelJobResponse {
    uint64 job_id = 1;
    JobStatus old_status = 2;
    JobStatus new_status = 3;
    int64 timestamp = 4;
}
```

---

### GetJobHistory

Get execution history.

```protobuf
message GetJobHistoryRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
    int32 limit = 3;
    string cursor = 4;
}

message GetJobHistoryResponse {
    repeated JobExecution executions = 1;
    string next_cursor = 2;
    bool has_more = 3;
    int64 timestamp = 4;
}
```

---

### GetSchedulerStatus

Get scheduler health and statistics.

```protobuf
message GetSchedulerStatusRequest {}

message GetSchedulerStatusResponse {
    bool running = 1;
    SchedulerStats stats = 2;
    int64 timestamp = 3;
}
```

## Error Handling

gRPC status codes map to scheduler errors:

| gRPC Code | Scheduler Error |
|-----------|-----------------|
| `NOT_FOUND` | `SCHEDULER_JOB_NOT_FOUND` |
| `ALREADY_EXISTS` | `SCHEDULER_JOB_EXISTS` |
| `INVALID_ARGUMENT` | `SCHEDULER_INVALID_JOB`, `SCHEDULER_INVALID_SCHEDULE` |
| `FAILED_PRECONDITION` | Invalid state transition |
| `PERMISSION_DENIED` | `SCHEDULER_UNAUTHORIZED` |
| `UNAVAILABLE` | `SCHEDULER_STOPPED` |
| `DEADLINE_EXCEEDED` | `SCHEDULER_JOB_TIMEOUT` |
| `INTERNAL` | `SCHEDULER_STORAGE_ERROR`, `SCHEDULER_HANDLER_FAILED` |

**Example Error Handling (Go):**

```go
resp, err := client.GetJob(ctx, req)
if err != nil {
    st, ok := status.FromError(err)
    if ok {
        switch st.Code() {
        case codes.NotFound:
            fmt.Println("Job not found")
        case codes.PermissionDenied:
            fmt.Println("Access denied")
        default:
            fmt.Printf("Error: %s\n", st.Message())
        }
    }
}
```

## Client Examples

### Go Client Setup

```go
import (
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials"
    proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// Load TLS credentials
creds, err := credentials.NewClientTLSFromFile("ca.crt", "")
if err != nil {
    log.Fatal(err)
}

// Connect
conn, err := grpc.Dial("localhost:10443", grpc.WithTransportCredentials(creds))
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

// Create client
client := proto.NewSchedulerServiceClient(conn)
```

### Python Client Setup

```python
import grpc
from quicbaas.scheduler.v1 import scheduler_pb2, scheduler_pb2_grpc

# Load credentials
with open('ca.crt', 'rb') as f:
    creds = grpc.ssl_channel_credentials(f.read())

# Connect
channel = grpc.secure_channel('localhost:10443', creds)
client = scheduler_pb2_grpc.SchedulerServiceStub(channel)

# Create job
response = client.CreateJob(scheduler_pb2.CreateJobRequest(
    name="daily-backup",
    owner_type=scheduler_pb2.OWNER_TYPE_SYSTEM,
    type=scheduler_pb2.JOB_TYPE_RECURRING,
    schedule="0 2 * * *",
    handler="backup.full",
))
print(f"Created job: {response.job.id}")
```

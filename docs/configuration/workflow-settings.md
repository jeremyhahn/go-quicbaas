# Workflow Settings Reference

Complete configuration reference for `WorkflowSettings` in workflow definitions.

Source: `pkg/orchestration/workflow_types.go`

## WorkflowDefinition Structure

```yaml
name: my-workflow
description: Description of the workflow
mode: constrained          # canned | constrained | autonomous
agents:
  - role: lead
    backend: acp
    model: claude-sonnet-4-20250514
    capabilities: [code_review, task_decomposition]
    mcp_scopes: [quicbaas_agent_*, quicbaas_orch_*]
  - role: worker
    backend: acp
    model: claude-sonnet-4-20250514
    capabilities: [code_write, test_write]
    mcp_scopes: [quicbaas_orch_peer_*, quicbaas_orch_file_*]
    count: 3
settings:
  # ... (see below)
```

## Workflow Modes

| Mode | Constant | Description |
|------|----------|-------------|
| `canned` | `WorkflowModeCanned` | Fully predetermined workflow with fixed steps. Requires lead agent. |
| `constrained` | `WorkflowModeConstrained` | Lead decomposes tasks within defined agent roles. Requires lead agent. |
| `autonomous` | `WorkflowModeAutonomous` | Lead has full control over task decomposition and agent spawning. |

## Complete Settings Reference

### Core Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_iterations` | `int` | 0 (unlimited) | Maximum workflow iteration cycles |
| `max_parallelism` | `int` | 0 (unlimited) | Maximum concurrent agent tasks |
| `poll_interval` | `string` | - | Duration between control loop polls (e.g., `"5s"`) |
| `timeout_per_item` | `string` | - | Maximum duration per work item (e.g., `"10m"`) |
| `total_timeout` | `string` | - | Maximum total workflow duration (e.g., `"2h"`) |
| `review_criteria` | `[]ReviewCriterion` | `[]` | Validation checks for the review phase |

### Communication

| Field | Type | Default | Valid Values | Description |
|-------|------|---------|-------------|-------------|
| `topology` | `string` | `hub_and_spoke` | `hub_and_spoke`, `hybrid`, `peer_to_peer` | Inter-agent messaging topology |
| `isolation` | `string` | `shared` | `shared`, `worktree` | Workspace isolation mode |

### Peer Messaging

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `peer_messaging.max_message_size` | `int` | `10240` | Maximum serialized message size in bytes |
| `peer_messaging.max_inbox_size` | `int` | `1000` | Maximum messages per agent inbox |
| `peer_messaging.overflow_action` | `string` | `"reject"` | `"reject"` or `"drop_oldest"` |
| `peer_messaging.inbox_ttl` | `string` | `"1h"` | Message retention duration |

### File Locking

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `file_locking.lease_ttl` | `string` | `"5m"` | Lock lease duration |
| `file_locking.heartbeat_interval` | `string` | `"30s"` | Lease renewal interval |
| `file_locking.max_renewals` | `int` | `60` | Maximum lease renewals |

### Vector DB / Auto-Indexing

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `vector_db.enabled` | `bool` | `false` | Enable auto-indexing |
| `vector_db.index_type` | `string` | `"auto"` | `"auto"`, `"memory"`, `"hnsw"` |
| `vector_db.auto_index` | `bool` | `false` | Auto-index on file changes |
| `vector_db.triggers` | `[]string` | `[]` | Indexing triggers |
| `vector_db.gpu_accelerated` | `bool` | `false` | Request GPU acceleration |

### Cost Controls

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `cost_controls.max_tokens_per_job` | `int64` | `0` (unlimited) | Total token budget for the job |
| `cost_controls.max_tokens_per_agent` | `int64` | `0` (unlimited) | Per-agent token limit |
| `cost_controls.budget_action` | `string` | `""` | `"warn"`, `"pause"`, or `"shutdown"` |

### Recovery

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `recovery.auto_restart` | `bool` | `false` | Enable automatic agent restart |
| `recovery.max_restarts` | `int` | `3` | Maximum restart attempts |
| `recovery.restart_backoff` | `string` | `"30s"` | Delay between restart attempts |

### Review Criteria

Each entry in `review_criteria`:

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Human-readable criterion name |
| `validator` | `string` | `"agent"`, `"command"`, or `"policy"` |
| `config` | `string` | Validator-specific configuration |
| `required` | `bool` | Whether failure blocks completion |

## Example Configurations

### Conservative (Production)

```yaml
settings:
  max_iterations: 5
  max_parallelism: 2
  poll_interval: 10s
  timeout_per_item: 15m
  total_timeout: 1h
  topology: hub_and_spoke
  isolation: shared
  peer_messaging:
    max_message_size: 10240
    max_inbox_size: 500
    overflow_action: reject
    inbox_ttl: 30m
  file_locking:
    lease_ttl: 5m
    heartbeat_interval: 30s
    max_renewals: 30
  cost_controls:
    max_tokens_per_job: 500000
    max_tokens_per_agent: 100000
    budget_action: shutdown
  recovery:
    auto_restart: false
```

### Recommended

```yaml
settings:
  max_iterations: 10
  max_parallelism: 4
  poll_interval: 5s
  timeout_per_item: 10m
  total_timeout: 2h
  topology: hybrid
  isolation: shared
  peer_messaging:
    max_message_size: 10240
    max_inbox_size: 1000
    overflow_action: reject
    inbox_ttl: 1h
  file_locking:
    lease_ttl: 5m
    heartbeat_interval: 30s
    max_renewals: 60
  vector_db:
    enabled: true
    index_type: auto
    auto_index: true
    triggers:
      - file_change
  cost_controls:
    max_tokens_per_job: 1000000
    max_tokens_per_agent: 200000
    budget_action: warn
  recovery:
    auto_restart: true
    max_restarts: 3
    restart_backoff: 30s
  review_criteria:
    - name: tests_pass
      validator: command
      config: "make test"
      required: true
    - name: lint_clean
      validator: command
      config: "make lint"
      required: false
```

### Advanced (Autonomous, High Throughput)

```yaml
settings:
  max_iterations: 50
  max_parallelism: 8
  poll_interval: 2s
  timeout_per_item: 5m
  total_timeout: 4h
  topology: peer_to_peer
  isolation: worktree
  peer_messaging:
    max_message_size: 32768
    max_inbox_size: 5000
    overflow_action: drop_oldest
    inbox_ttl: 2h
  file_locking:
    lease_ttl: 2m
    heartbeat_interval: 15s
    max_renewals: 120
  vector_db:
    enabled: true
    index_type: hnsw
    auto_index: true
    triggers:
      - file_change
      - iteration_complete
  cost_controls:
    max_tokens_per_job: 5000000
    max_tokens_per_agent: 500000
    budget_action: pause
  recovery:
    auto_restart: true
    max_restarts: 10
    restart_backoff: 10s
```

## Validation

`WorkflowSettings.Validate()` checks:

1. Topology is a known value (or empty for default)
2. Isolation mode is a known value (or empty for default)
3. Overflow action is `"reject"` or `"drop_oldest"` (or empty for default)
4. Budget action is `"warn"`, `"pause"`, or `"shutdown"` (or empty for default)

`WorkflowSettings.ApplyDefaults()` fills zero-value fields before validation.

`WorkflowDefinition.Validate()` additionally checks:
- Name is non-empty
- Mode is valid
- At least one agent is defined
- Canned/constrained modes include a `lead` role

# Recovery

Crash recovery and auto-restart configuration for orchestrated AI agent workflows.

Source: `pkg/orchestration/workflow_types.go`

## Overview

When an orchestrated AI agent (Claude Code, Codex, OpenCode) crashes or becomes unresponsive, the recovery system determines whether and how to restart it. Auto-restart is disabled by default to prevent runaway cost accumulation.

## Recovery Settings

```go
type RecoverySettings struct {
    AutoRestart    bool   `yaml:"auto_restart"`     // Enable auto-restart (default: false)
    MaxRestarts    int    `yaml:"max_restarts"`      // Maximum restart attempts (default: 3)
    RestartBackoff string `yaml:"restart_backoff"`   // Delay between restarts (default: 30s)
}
```

## Bootstrap Ordering

When recovering a workflow, components must be restored in the correct order:

1. **KV Store** -- Ensure the underlying storage is available
2. **Lease Manager** -- Reconnect or recreate lease state
3. **File Lock Service** -- Expired locks are cleaned up automatically via lease TTL
4. **Cost Tracker** -- Read current budget state from KV
5. **Peer Messaging Service** -- Restore inbox state from KV
6. **State Machine** -- Transition crashed agents to `error` state using `Force` flag
7. **Agent Sessions** -- Restart agents if auto-restart is enabled

This ordering ensures that safety checks (budget, locks) are available before any agent resumes work.

## Auto-Restart Logic

When an agent failure is detected:

```
Agent crash detected
    |
    v
[auto_restart enabled?] --no--> Mark agent as "error", stop
    |
    yes
    v
[restart_count < max_restarts?] --no--> Mark as "error", stop
    |
    yes
    v
[budget check: status != exceeded?] --no--> Mark as "error", stop
    |
    yes
    v
[lead agent healthy?] --no--> Mark as "error", stop
    |
    yes
    v
Wait restart_backoff duration
    |
    v
Restart agent session
    |
    v
Increment restart counter
```

### Safety Conditions

Before restarting an agent, the system verifies:

1. **Budget check**: `CheckBudget()` must not return `BudgetExceeded`
2. **Lead health**: The lead agent must be in `ready` or `busy` state (not crashed itself)
3. **Restart count**: Must be below `MaxRestarts`

If any condition fails, the agent is left in `error` state for manual intervention.

## Configuration

```yaml
settings:
  recovery:
    auto_restart: false    # Default: off (explicit opt-in)
    max_restarts: 3        # Attempts before giving up
    restart_backoff: 30s   # Delay between restart attempts
```

### Conservative (Production)

```yaml
recovery:
  auto_restart: false
```

### Recommended

```yaml
recovery:
  auto_restart: true
  max_restarts: 3
  restart_backoff: 30s
```

### Aggressive (Development)

```yaml
recovery:
  auto_restart: true
  max_restarts: 10
  restart_backoff: 5s
```

## State Machine Integration

On crash detection, the state machine transitions the agent:

- **MemberStatus**: `busy` -> `error` (using `Force` flag to bypass normal validation)
- **ExecutionStatus**: current state -> `error` (using `Force` flag)

On successful restart:

- **MemberStatus**: `error` -> `ready`
- **ExecutionStatus**: `error` -> `idle`

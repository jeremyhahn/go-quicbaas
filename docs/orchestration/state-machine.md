# State Machine

Two-level state machine for tracking orchestrated AI agent lifecycle and task execution.

Source: `pkg/orchestration/state_machine.go`

## Why Two Levels

A single state machine cannot capture both "is this agent available?" and "what is this agent doing right now?" without an explosion of combined states. The two-level design separates:

- **MemberStatus** (fleet-level): Is the agent registered, busy, shutting down, or crashed?
- **ExecutionStatus** (task-level): Is the agent idle, processing, calling a tool, or done?

This means an agent can be `busy` (fleet) and `calling_tool` (execution) simultaneously, and each level transitions independently.

## MemberStatus (Fleet-Level)

5 states representing the agent's lifecycle within the fleet.

```
                  +-------+
           +----->| ready |<-----+
           |      +---+---+      |
           |        |   |        |
           |        v   v        |
           |   +------+ +---+   |
           +---| busy | |err|---+
               +--+---+ +---+
                  |   |
                  v   v
         +-----------+----------+
         | shutdown_  | shutdown |
         | requested  |          |
         +-----+------+----------+
               |
               v
          +---------+
          |shutdown  |
          +---------+
```

### Transitions

| From | To | Trigger |
|------|----|---------|
| `ready` | `busy` | Task assigned |
| `ready` | `shutdown_requested` | Graceful shutdown initiated |
| `ready` | `error` | Health check failure |
| `busy` | `ready` | Task completed |
| `busy` | `shutdown_requested` | Graceful shutdown during work |
| `busy` | `error` | Agent crash or timeout |
| `shutdown_requested` | `shutdown` | Shutdown complete |
| `shutdown_requested` | `error` | Shutdown failure |
| `shutdown` | (terminal) | No transitions out |
| `error` | `ready` | Recovery / restart |

## ExecutionStatus (Task-Level)

10 states representing the agent's current execution phase.

```
        +------+
  +---->| idle |<---------+---------+
  |     +--+---+          |         |
  |        |              |         |
  |        v              |         |
  |  +-----------+        |         |
  |  | awaiting  |        |         |
  |  | _input    |        |         |
  |  +-----+-----+        |         |
  |        |              |         |
  |        v              |         |
  |  +------------+       |         |
  |  | processing |<--+   |         |
  |  +--+--+--+---+   |   |         |
  |     |  |  |       |   |         |
  |     v  v  v       |   |         |
  |  +----+ +-----+  |   |         |
  |  |call| |gen-  |  |   |         |
  |  |tool| |erate |--+   |         |
  |  +-+--+ +--+---+      |         |
  |    |        |          |         |
  |    +---+----+          |         |
  |        |               |         |
  |        v               |         |
  |  +----------+    +-----------+   |
  |  | completed|--->|           |   |
  |  +----------+    |           |   |
  |                  |  (back    |   |
  |  +----------+    |  to idle) |   |
  +--| cancelled|<---|           |   |
     +----------+    +-----------+   |
                                     |
     +-------+                       |
     | error |-->--------------------+
     +-------+
```

### Transitions

| From | Valid Targets |
|------|--------------|
| `idle` | `awaiting_input`, `processing`, `error` |
| `awaiting_input` | `processing`, `cancelling`, `error` |
| `processing` | `calling_tool`, `generating`, `paused`, `cancelling`, `completed`, `error` |
| `calling_tool` | `processing`, `cancelling`, `error` |
| `generating` | `processing`, `completed`, `cancelling`, `error` |
| `paused` | `processing`, `cancelling`, `error` |
| `cancelling` | `cancelled`, `error` |
| `cancelled` | `idle` |
| `completed` | `idle` |
| `error` | `idle` |

## Transition Options

```go
type TransitionOption struct {
    Guard bool  // Idempotent: same-state transition returns nil
    Force bool  // Bypass validation (post-crash recovery only)
}
```

### Guard Flag

When `Guard` is true and `from == to`, the transition succeeds with no-op. This prevents errors when setting a state that is already set (idempotent operations).

Use case: Heartbeat updates that re-confirm the current state.

### Force Flag

When `Force` is true, all transitions are allowed regardless of the transition map. This is exclusively for crash recovery where the state machine may be in an inconsistent state.

Use case: Moving a crashed agent from any state to `error`.

## Validation Functions

```go
ValidateMemberTransition(from, to MemberStatus, opts TransitionOption) error
ValidateExecTransition(from, to ExecutionStatus, opts TransitionOption) error
```

Both use O(1) map lookups against pre-built transition tables. Invalid transitions return `MemberTransitionError` or `ExecTransitionError` with the sentinel errors `ErrInvalidMemberTransition` or `ErrInvalidExecTransition`.

## Example

```go
// Normal transition
err := ValidateMemberTransition(MemberStatusReady, MemberStatusBusy, TransitionOption{})

// Idempotent guard
err := ValidateExecTransition(ExecStatusProcessing, ExecStatusProcessing, TransitionOption{Guard: true})
// err == nil (no-op)

// Crash recovery
err := ValidateMemberTransition(MemberStatusBusy, MemberStatusError, TransitionOption{Force: true})
// err == nil (always succeeds)
```

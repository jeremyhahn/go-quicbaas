# Communication Topology

Inter-agent messaging topology that controls how orchestrated AI agents (Claude Code, Codex, OpenCode) communicate during a workflow.

Source: `pkg/orchestration/workflow_types.go`, `pkg/orchestration/communication_policy.go`

## Topology Modes

| Mode | Constant | Worker-to-Worker | Use Case |
|------|----------|-----------------|----------|
| `hub_and_spoke` | `TopologyHubAndSpoke` | Blocked | Default. Safest. All coordination flows through the lead agent. |
| `hybrid` | `TopologyHybrid` | Advisory only | Workers share observations (progress, findings, conflict warnings) directly; control actions route through lead. |
| `peer_to_peer` | `TopologyPeerToPeer` | Unrestricted | Full mesh. Workers can assign, cancel, or request work from each other. |

## When to Use Each

**hub_and_spoke** -- Use when you need deterministic control flow or are running untrusted agent configurations. The lead agent sees every message and can enforce ordering.

```
     +---------+
     |  Lead   |
     +----+----+
      /   |   \
     v    v    v
  +---+ +---+ +---+
  | W1| | W2| | W3|
  +---+ +---+ +---+
  (no cross-links)
```

**hybrid** -- Use when workers benefit from sharing progress or conflict warnings without waiting for the lead to relay. Control flow (assign, cancel, complete) still goes through the lead.

```
     +---------+
     |  Lead   |   <-- control flow
     +----+----+
      /   |   \
     v    v    v
  +---+ +---+ +---+
  | W1|<-->| W2|<-->| W3|   <-- advisory only
  +---+ +---+ +---+
```

**peer_to_peer** -- Use for autonomous workflows where agents self-organize. Requires careful budget and lock configuration to prevent runaway behavior.

```
  +---+     +---+
  | W1|<--->| W2|
  +---+\   /+---+
    ^   \ /   ^
    |    X    |
    v   / \   v
  +---+/   \+---+
  | W3|<--->|Lead|
  +---+     +----+
```

## Advisory vs Control Actions

The `advisoryActions` map classifies which actions are informational (no acknowledgment required):

| Action | Type | Description |
|--------|------|-------------|
| `advisory` | Advisory | General informational message |
| `progress` | Advisory | Status update on current work |
| `finding` | Advisory | Report a discovery or observation |
| `conflict_warning` | Advisory | Warn about potential file/resource conflict |
| `assign` | Control | Assign a task to an agent |
| `complete` | Control | Signal task completion |
| `cancel` | Control | Cancel an agent's current work |
| `feedback` | Control | Provide review feedback |

In `hybrid` mode, worker-to-worker messages are allowed only for advisory actions. Any control action between workers returns `ErrNonAdvisoryPeerMessage`.

## Communication Policies (Deadlock Prevention)

Policies define whether a message is fire-and-forget or blocking:

| Direction | Action | Policy | Rationale |
|-----------|--------|--------|-----------|
| Lead -> Worker | `assign` | Fire-and-forget | Prevents hub-spoke deadlock |
| Lead -> Worker | `feedback` | Fire-and-forget | Lead must not block on worker |
| Lead -> Worker | `cancel` | Fire-and-forget | Cancellation is best-effort |
| Worker -> Lead | `complete` | Blocking | Completion must be acknowledged |
| Worker -> Lead | `failed` | Blocking | Failure must be acknowledged |
| Worker -> Lead | `question` | Fire-and-forget | Questions are advisory |
| Validator -> Lead | `result` | Blocking | Review results must be acknowledged |

Unknown combinations default to fire-and-forget to prevent accidental deadlocks. Blocking messages time out after `MaxBlockDuration` (5 minutes).

## Configuration

Set in `WorkflowSettings`:

```yaml
settings:
  topology: hybrid   # hub_and_spoke | hybrid | peer_to_peer
```

Default: `hub_and_spoke` (applied by `ApplyDefaults()`).

Validation rejects unknown values via `ValidateTopology()`.

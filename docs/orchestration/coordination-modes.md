# Coordination Modes

## Overview

QuicBaaS supports multiple coordination patterns for orchestrating agent teams. The pattern determines how tasks are assigned, claimed, and tracked across agents.

## Direct Orchestration

A lead agent decomposes work and assigns tasks directly to specific teammates.

```
Lead Agent
    |
    +-- assign task A --> Worker 1
    +-- assign task B --> Worker 2
    +-- assign task C --> Worker 3
```

**How it works:**

1. Lead agent calls `quicbaas_orchestrator_decompose` to break work into prioritized items.
2. Lead agent calls `quicbaas_orchestrator_spawn` for each worker session.
3. Lead agent calls `quicbaas_orchestrator_prompt` to assign specific tasks to specific workers.
4. Workers fire `TaskCompleted` hooks when done. Lead receives completion events via channel server.
5. Lead assigns follow-up work based on results.

**Best for**: Well-defined tasks with clear ownership. The lead agent has full visibility and control.

## Pub/Sub Coordination

Tasks are broadcast to a coordination channel. Any available agent claims the work.

```
Task Store (pending tasks)
    |
    +-- broadcast --> Channel: orchestrator:tasks:team-x
                        |
                        +-- Worker 1 (claims task A)
                        +-- Worker 2 (claims task B)
                        +-- Worker 3 (idle, waiting)
```

**How it works:**

1. Tasks enter the task store via hooks or direct API calls (status: `pending`).
2. QuicBaaS publishes task events to the team's messaging channel.
3. Channel servers push events into all active Claude Code sessions for that team.
4. The `TeammateIdle` hook checks for pending tasks. When a teammate goes idle, QuicBaaS responds with `exit_code: 2` and the next available task, preventing idle state.
5. The teammate picks up the task and fires `TaskCompleted` when done.

**Best for**: Elastic workloads where any worker can handle any task. Self-balancing across available agents.

## Hybrid Coordination

The lead agent assigns high-priority or specialized tasks directly. Remaining tasks go into the pub/sub pool for workers to claim.

```
Lead Agent
    |
    +-- assign critical task --> Worker 1 (specialist)
    |
    +-- publish remaining tasks --> Channel: orchestrator:tasks:team-x
                                      |
                                      +-- Worker 2 (claims)
                                      +-- Worker 3 (claims)
```

**How it works:**

1. Lead agent decomposes work, tagging items with priority and required capabilities.
2. High-priority or specialized tasks are assigned directly to specific workers.
3. General tasks are left in `pending` status for pub/sub claiming.
4. `TeammateIdle` hooks distribute pending work to free agents automatically.
5. Lead agent monitors progress via channel events and reassigns as needed.

**Best for**: Teams with specialized agents (e.g., one agent for database work, others for general tasks).

## Task Dependencies and Blocking

Tasks support dependency chains via the `dependencies` field on work items produced by `quicbaas_orchestrator_decompose`.

```json
{
  "work_items": [
    {"id": "wi_001", "title": "Define interface", "priority": 1, "dependencies": []},
    {"id": "wi_002", "title": "Implement handler", "priority": 2, "dependencies": ["wi_001"]},
    {"id": "wi_003", "title": "Add tests", "priority": 3, "dependencies": ["wi_002"]}
  ]
}
```

**Dependency resolution:**

- A task with unmet dependencies stays in `pending` status but is not assigned to workers.
- When a dependency completes (`TaskCompleted` hook), the task store evaluates blocked tasks.
- Newly unblocked tasks are published to the messaging channel for assignment or claiming.
- Circular dependencies are rejected at decomposition time.

## Agent-to-Agent Messaging

Agents communicate directly through QuicBaaS messaging channels using the broadcast tool and channel server reply tool.

### Broadcasting

Any agent can publish to a coordination channel:

```json
{
  "tool": "quicbaas_orchestrator_broadcast",
  "arguments": {
    "channel": "agent-updates",
    "message": "Found breaking API change in pkg/api/, all agents should re-read interfaces.",
    "metadata": {
      "session_id": "sess_x1y2z3",
      "severity": "high"
    }
  }
}
```

All agents subscribed to `agent-updates` receive this via their channel servers.

### Replying

Agents with the reply tool enabled can send messages back through the channel server:

```json
{
  "name": "reply",
  "arguments": {
    "channel": "agent-updates",
    "message": "Acknowledged, re-reading pkg/api/ interfaces."
  }
}
```

### Channel Naming Conventions

| Channel Pattern | Purpose |
|---|---|
| `orchestrator:tasks:<team>` | Task lifecycle events (created, completed) |
| `agent-updates` | Cross-agent status broadcasts |
| `orchestrator:health:<team>` | Health check results |

## Choosing a Coordination Mode

| Factor | Direct | Pub/Sub | Hybrid |
|---|---|---|---|
| Task assignment control | High | Low | Medium |
| Load balancing | Manual | Automatic | Mixed |
| Specialized workers | Supported | All workers equal | Supported |
| Lead agent required | Yes | No | Yes |
| Complexity | Low | Low | Medium |
| Best scale | Small teams | Elastic pools | Mixed workloads |

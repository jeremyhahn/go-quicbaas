# Agent Orchestration Quickstart

Get started with QuicBaaS agent orchestration in minutes.

## Prerequisites

- QuicBaaS server running with orchestration enabled (default)
- CLI tool built: `make build`
- Backend configured (ACP for Claude, Ollama for local inference)

## 1. Start the Server

Orchestration is enabled by default. No configuration needed for basic setup.

```bash
quicbaas server start
```

Check health:

```bash
quicbaas orchestrator health
```

## 2. Check Available Backends

List registered backends and capabilities:

```bash
quicbaas orchestrator backends

# Output:
# NAME        TYPE        STATUS      AVAILABLE
# claude      acp         running     true
# ollama      ollama      running     true
```

## 3. Configure Backends (Optional)

If backends are not auto-discovered, add to your config file or environment:

```yaml
orchestration:
  enabled: true
  lead_agent:
    backend: "claude"
    model: "claude-3-5-sonnet-20241022"
    system_prompt: "You are an expert software engineer"
  heartbeat_timeout: 90s
```

Or via environment variables:

```bash
export QUICBAAS_ORCHESTRATION_ENABLED=true
export QUICBAAS_ORCHESTRATION_LEAD_AGENT_BACKEND=claude
export QUICBAAS_ORCHESTRATION_LEAD_AGENT_MODEL=claude-3-5-sonnet-20241022
export QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT=90s
```

## 4. Spawn an Agent Session

Create a persistent session bound to a backend:

```bash
quicbaas orchestrator spawn --backend claude

# Output:
# Session ID:     sess_abc123def456
# Backend:        claude
```

Save the session ID for later use.

## 5. Send a Prompt

Send a prompt to the active session:

```bash
quicbaas orchestrator prompt sess_abc123def456 "Write a Go function to calculate Fibonacci"

# Agent responds with code and explanation
```

## 6. Check Session Status

Query the session state:

```bash
quicbaas orchestrator status sess_abc123def456

# Output:
# Session ID:     sess_abc123def456
# Backend:        claude
# Status:         idle
# Created:        2026-03-31T14:20:00Z
# Last Active:    2026-03-31T14:22:30Z
```

## 7. Register Agents in Fleet

Register agent workers on remote machines:

```bash
# Register agent with default model
quicbaas agent register --name worker-1 --host 192.168.1.10:9090 --type claude

# Register with custom model
quicbaas agent register --name worker-2 --host 192.168.1.11:9090 --type claude \
  --model claude-3-opus-20250219

# Register Ollama agent for local inference
quicbaas agent register --name worker-local --host localhost:9090 --type ollama \
  --backend ollama --model llama3
```

## 8. List Fleet Agents

View registered agents:

```bash
quicbaas agent list

# Output:
# ID                                      NAME            HOST                TYPE        STATUS
# a1b2c3d4-e5f6-7890-abcd-ef1234567890  worker-1        192.168.1.10:9090   claude      running
# e5f6-7890-abcd-ef1234567890-a1b2c3d4  worker-2        192.168.1.11:9090   claude      registered
```

## 9. Create and Assign Tasks

Create tasks for agents to claim:

```bash
# Create a task
quicbaas orchestrator task create --subject "Build authentication module" --team backend

# List pending tasks
quicbaas orchestrator task list --status pending

# Agent claims the task
quicbaas agent claim task_abc123 --agent worker-1
```

## 10. Decompose Complex Tasks

Break down a large task into work items using the lead agent:

```bash
quicbaas orchestrator decompose "Build multi-tenant REST API with authentication and caching"

# Output (JSON):
# {
#   "work_items": [
#     {
#       "id": "wi_001",
#       "title": "Design API schema",
#       "priority": 1,
#       "dependencies": []
#     },
#     {
#       "id": "wi_002",
#       "title": "Implement authentication layer",
#       "priority": 2,
#       "dependencies": ["wi_001"]
#     }
#   ]
# }
```

## 11. Close Sessions

Clean up resources when done:

```bash
quicbaas orchestrator close sess_abc123def456
```

## Multi-Machine Agent Setup

Deploy agents on multiple machines:

### On Node 1 (192.168.1.10)

```bash
# Register the agent
quicbaas agent register --name prod-agent-01 --host 192.168.1.10:9090 --type claude

# Start agent process
quicbaas orchestrator spawn --backend claude &
```

### On Node 2 (192.168.1.11)

```bash
quicbaas agent register --name prod-agent-02 --host 192.168.1.11:9090 --type claude

quicbaas orchestrator spawn --backend claude &
```

### Coordination

All agents automatically register with the central orchestrator. The orchestrator tracks heartbeats and marks agents offline if no heartbeat received within `heartbeat_timeout` (default: 90s).

## Environment Variables

Lead agent configuration via environment:

```bash
# Backend to use for task decomposition and intelligent orchestration
QUICBAAS_ORCHESTRATION_LEAD_AGENT_BACKEND=claude

# Model for the lead agent
QUICBAAS_ORCHESTRATION_LEAD_AGENT_MODEL=claude-3-5-sonnet-20241022

# How long to wait before marking an agent as offline (default: 90s)
QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT=120s

# Enable/disable orchestration service
QUICBAAS_ORCHESTRATION_ENABLED=true
```

## Next Steps

- See [CLI Reference](/docs/orchestration/cli-reference.md) for complete command documentation
- See [Fleet Management](/docs/orchestration/fleet-management.md) for agent lifecycle details
- See [Overview](/docs/orchestration/overview.md) for architecture and design principles

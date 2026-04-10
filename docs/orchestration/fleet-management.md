# Agent Fleet Management

Centralized registration, tracking, deployment, and lifecycle management of AI agents across distributed hosts.

## Architecture

- **Agent Registry** - Server-side, in-memory store backed by `sync.Map` for lock-free concurrent access.
- **Self-Registration** - Agents register themselves on startup via CLI or REST API, providing name, host, type, and tenant.
- **Heartbeat Monitoring** - Agents send heartbeats every 30 seconds. Registry marks agents offline after configurable timeout (default: 90s).
- **Tenant Isolation** - Every operation requires a tenant ID. Agents from one tenant are invisible to another.

## Agent Status Transitions

```
registered  -->  running  -->  idle  -->  offline
                     ^                      |
                     +----------------------+

Error conditions:
running     -->  error  -->  offline
```

**Status meanings:**

- `registered` - Agent registered, initial state
- `running` - Actively processing or responding to requests
- `idle` - Registered and healthy, ready for work
- `offline` - No heartbeat received within heartbeat timeout
- `error` - Last operation failed, needs recovery

## CLI Commands

### Register an Agent

Register a new agent with the fleet:

```bash
quicbaas agent register --name "worker-1" --host 192.168.1.10:9090 --type claude --tenant dev-team

quicbaas agent register --name "worker-2" --host 192.168.1.11:9090 --type claude \
  --model claude-3-opus-20250219 \
  --tenant dev-team

quicbaas agent register --name "local-agent" --host localhost:9090 --type ollama \
  --backend ollama --model llama3 --tenant dev-team
```

Flags:
- `--name` (required) - Agent name
- `--host` (required) - Host address (IP:port)
- `--type` (required) - Agent type (e.g., claude, ollama)
- `--backend` - Backend name for the agent
- `--model` - Model identifier (e.g., claude-3-opus-20250219)
- `--work-dir` - Working directory for agent persistence
- `--tenant` - Tenant ID (default: "default")

### List Fleet Agents

List all agents with optional filters:

```bash
# All agents for a tenant
quicbaas agent list --tenant dev-team

# Filter by host
quicbaas agent list --tenant dev-team --host 192.168.1.10:9090

# Filter by status
quicbaas agent list --tenant dev-team --status running

# Filter by type
quicbaas agent list --tenant dev-team --type claude

# Filter by backend
quicbaas agent list --tenant dev-team --backend claude

# JSON output for scripting
quicbaas agent list --tenant dev-team --output json
```

### Get Agent Status

Get detailed information for a specific agent:

```bash
quicbaas agent status agent_abc123 --tenant dev-team

quicbaas agent status agent_abc123 --tenant dev-team --output json
```

### Send Heartbeat

Manually send a heartbeat to keep agent alive (normally done by agent):

```bash
quicbaas agent heartbeat agent_abc123 --tenant dev-team
```

### Remove an Agent

Remove an agent from the registry:

```bash
quicbaas agent remove agent_abc123 --tenant dev-team
```

## Model Configuration Per Agent

Agents can be registered with specific models that override backend defaults:

```bash
# Agent uses specific Claude model
quicbaas agent register --name worker-opus --host node01:9090 --type claude \
  --model claude-3-opus-20250219

# Agent uses specific Ollama model
quicbaas agent register --name worker-local --host localhost:9090 --type ollama \
  --backend ollama --model mistral:7b
```

When a task is assigned to the agent, the agent's configured model is used. If no model is configured at registration, the backend's default model is used.

## Heartbeat Lifecycle

Agents maintain connectivity through regular heartbeats:

```
Agent                           Registry
  |                                |
  |--- POST /heartbeat (every 30s) -->|
  |                                | Update last_heartbeat timestamp
  |<------ 200 OK ------------------|
  |                                |
  |--- (no heartbeat for 90s) ---->|
  |                                | Mark as "offline"
```

**Configuration:**

```bash
# Heartbeat timeout (default: 90s)
export QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT=120s
```

If an agent crashes without proper deregistration, heartbeat timeout causes the registry to mark it offline automatically.

## REST API

All endpoints require the `X-Tenant-ID` header for tenant isolation.

### POST /api/v1/orchestrator/agents

Register a new agent:

```bash
curl -X POST http://localhost:8080/api/v1/orchestrator/agents \
  -H "X-Tenant-ID: dev-team" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "worker-1",
    "host": "192.168.1.10:9090",
    "type": "claude",
    "backend": "claude",
    "model": "claude-3-opus-20250219",
    "work_dir": "/data/agents"
  }'
```

Response `201 Created`:
```json
{
  "agent": {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "name": "worker-1",
    "host": "192.168.1.10:9090",
    "type": "claude",
    "backend": "claude",
    "model": "claude-3-opus-20250219",
    "status": "registered",
    "registered_at": "2026-03-31T12:00:00Z"
  }
}
```

### GET /api/v1/orchestrator/agents

List agents with optional filters:

```bash
curl http://localhost:8080/api/v1/orchestrator/agents?status=running \
  -H "X-Tenant-ID: dev-team"

curl http://localhost:8080/api/v1/orchestrator/agents?host=192.168.1.10:9090 \
  -H "X-Tenant-ID: dev-team"

curl http://localhost:8080/api/v1/orchestrator/agents?type=claude \
  -H "X-Tenant-ID: dev-team"

curl http://localhost:8080/api/v1/orchestrator/agents?backend=claude \
  -H "X-Tenant-ID: dev-team"
```

Response `200 OK`:
```json
{
  "agents": [
    {
      "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "name": "worker-1",
      "host": "192.168.1.10:9090",
      "type": "claude",
      "backend": "claude",
      "model": "claude-3-opus-20250219",
      "status": "running",
      "last_seen": "2026-03-31T12:05:30Z",
      "registered_at": "2026-03-31T12:00:00Z"
    }
  ],
  "total": 1
}
```

### GET /api/v1/orchestrator/agents/{agentID}

Get a single agent's details:

```bash
curl http://localhost:8080/api/v1/orchestrator/agents/a1b2c3d4 \
  -H "X-Tenant-ID: dev-team"
```

Response `200 OK`:
```json
{
  "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "tenant_id": "dev-team",
  "name": "worker-1",
  "host": "192.168.1.10:9090",
  "type": "claude",
  "backend": "claude",
  "model": "claude-3-opus-20250219",
  "status": "running",
  "work_dir": "/data/agents",
  "version": 1,
  "registered_at": "2026-03-31T12:00:00Z",
  "last_seen": "2026-03-31T12:05:30Z",
  "metadata": {
    "capabilities": "code-generation,testing",
    "region": "us-west-2"
  }
}
```

### POST /api/v1/orchestrator/agents/{agentID}/heartbeat

Send a heartbeat to update last-seen timestamp:

```bash
curl -X POST http://localhost:8080/api/v1/orchestrator/agents/a1b2c3d4/heartbeat \
  -H "X-Tenant-ID: dev-team"
```

Response `200 OK`:
```json
{
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "status": "running",
  "last_heartbeat": "2026-03-31T12:06:00Z"
}
```

### DELETE /api/v1/orchestrator/agents/{agentID}

Remove an agent from the registry:

```bash
curl -X DELETE http://localhost:8080/api/v1/orchestrator/agents/a1b2c3d4 \
  -H "X-Tenant-ID: dev-team"
```

Response `204 No Content`.

## MCP Tools

Agent fleet operations also available via MCP (Model Context Protocol):

| Tool | Description |
|------|-------------|
| `quicbaas_agent_fleet_register` | Register a new agent |
| `quicbaas_agent_fleet_list` | List agents with filters |
| `quicbaas_agent_fleet_status` | Get agent status |
| `quicbaas_agent_fleet_remove` | Remove agent from registry |
| `quicbaas_agent_fleet_heartbeat` | Send heartbeat |

## Agent Lifecycle

### Self-Registration (Containerized Agents)

Containerized agents follow this lifecycle:

1. **Startup** - Agent sends `POST /api/v1/orchestrator/agents` with metadata
2. **Heartbeat Loop** - Background goroutine sends heartbeat every 30 seconds
3. **Shutdown** - On `SIGTERM`/`SIGINT`, agent sends `DELETE /api/v1/orchestrator/agents/{agentID}` before exiting

If the agent crashes without deregistration, heartbeat timeout (90s default) causes the registry to mark it offline.

### Monitoring Agent Health

Check registry for offline agents:

```bash
# List only running agents
quicbaas agent list --tenant dev-team --status running

# List offline agents for recovery
quicbaas agent list --tenant dev-team --status offline

# Remove stuck agents
quicbaas agent remove offline_agent_id --tenant dev-team
```

## Multi-Protocol Support

All fleet operations available over every transport:

| Protocol | Transport | Usage |
|----------|-----------|-------|
| REST | HTTP/1.1, HTTP/2 | HTTP clients, browsers |
| gRPC | HTTP/2 | Native gRPC clients |
| QUIC | UDP (HTTP/3) | Low-latency connections |
| Unix | gRPC over Unix socket | Local agent connections |
| MCP | Model Context Protocol | Claude, Codex, AI tools |
| Embedded | In-process | SDK clients in same process |

All use the same SDK client abstraction. Multi-protocol support is transparent.

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_ORCHESTRATION_ENABLED` | Enable orchestration service | `true` |
| `QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT` | Timeout before marking offline | `90s` |
| `QUICBAAS_ORCHESTRATION_LEAD_AGENT_BACKEND` | Backend for decomposition/orchestration | (none) |
| `QUICBAAS_ORCHESTRATION_LEAD_AGENT_MODEL` | Model for lead agent | (backend default) |
| `QUICBAAS_TENANT_ID` | Default tenant for agent registration | `"default"` |

## Debugging

View agent registry state (admin only):

```bash
# Get all agents (all tenants)
quicbaas agent list --tenant admin

# Check specific agent health
quicbaas agent status agent_id --tenant dev-team --output json

# Remove problematic agents
quicbaas agent remove offline_agent --tenant dev-team
```

Check orchestrator health:

```bash
quicbaas orchestrator health --output json
```

## Common Patterns

### Agent Pool for Load Balancing

Register multiple agents and rotate task assignment:

```bash
for i in {1..5}; do
  quicbaas agent register --name "worker-$i" --host "node-$i:9090" --type claude
done

# View pool
quicbaas agent list | grep worker

# Assign tasks round-robin
quicbaas orchestrator task claim task_1 --agent worker-1
quicbaas orchestrator task claim task_2 --agent worker-2
# ... etc
```

### Graceful Shutdown

Agent deregistration on shutdown:

```bash
#!/bin/bash
AGENT_ID=$(quicbaas agent register ... --output json | jq -r '.id')

trap "quicbaas agent remove $AGENT_ID; exit" SIGTERM SIGINT

# Run agent
quicbaas orchestrator spawn &
wait
```

## Agent Roles and Capabilities

When agents are provisioned through a workflow, each agent is assigned a role with specific capabilities and MCP scopes. See [Agent Identity](agent-identity.md) for IAM integration details.

### Role Types

| Role | Purpose | Typical Backend |
|------|---------|-----------------|
| lead | Decomposes tasks, coordinates workers, reviews results | claude |
| coder | Implements code changes, runs tests | claude, codex |
| reviewer | Reviews code quality, validates output | codex, ollama |

### Capabilities

Capabilities declared in workflow YAML control what features an agent can use:

- `persistent_sessions` - Agent maintains state across prompts
- `tool_use` - Agent can invoke MCP tools
- `file_access` - Agent can read/write project files

### MCP Scope Assignment

Each role receives IAM scopes that filter which MCP tools are visible. See [MCP Permissions](mcp-permissions.md) for how tool filtering works.

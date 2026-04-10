# Naming Convention

Strict nomenclature for distinguishing infrastructure agents from AI coding agents.

## Definitions

| Term | Meaning | Examples |
|------|---------|---------|
| **QuicBaaS agent** | The infrastructure agent binary that runs on remote systems. Handles enrollment, attestation, fleet heartbeat, and MCP tool execution. | `quicbaas-agent` binary, fleet registration endpoint, agent heartbeat |
| **AI agent** / **orchestrated agent** | An AI coding tool (Claude Code, Codex, OpenCode) managed by the QuicBaaS orchestrator. Runs inside a container or on a developer machine. | Claude Code CLI, Codex CLI, OpenCode CLI |

## Why This Distinction Matters

Without clear terminology, documentation and code become ambiguous:

- "The agent crashed" -- which agent? The infrastructure binary or the AI tool?
- "Register the agent" -- fleet registration (infrastructure) or project registration (AI agent)?
- "Agent health check" -- infrastructure heartbeat or AI session status?

Using precise terms prevents miscommunication in docs, code reviews, incident response, and operational runbooks.

## Where Each Term Is Used

### QuicBaaS Agent (Infrastructure)

- `pkg/orchestration/agent_registry.go` -- Fleet registry for QuicBaaS agent instances
- `pkg/orchestration/agent_identity.go` -- Agent identity and attestation
- REST API: `/api/v1/orchestrator/agents` -- Register/deregister QuicBaaS agents
- MCP tools: `quicbaas_agent_*` prefix -- Operations on the infrastructure layer
- Entrypoint scripts: Fleet registration and heartbeat loops
- Docker healthchecks: Verify the infrastructure agent is responsive

### AI Agent / Orchestrated Agent

- `pkg/orchestration/state_machine.go` -- MemberStatus and ExecutionStatus track AI agent state
- `pkg/orchestration/peer_messaging.go` -- AI agents exchange messages
- `pkg/orchestration/file_lock.go` -- AI agents coordinate file access
- `pkg/orchestration/cost_tracker.go` -- Token budgets per AI agent
- MCP tools: `quicbaas_orch_*` prefix -- Coordination between AI agents
- Workflow definitions: `AgentRoleSpec` describes AI agent roles (lead, worker, validator)
- Container Dockerfiles: Package the AI agent CLI tools

### Overlap

The entrypoint scripts bridge both worlds: they register the container as a QuicBaaS agent (infrastructure) and then exec the AI agent CLI (orchestrated agent). The QuicBaaS agent ID maps to the container hostname; the AI agent session is created separately via `quicbaas_agent_spawn`.

## Style Guide

- Always write "QuicBaaS agent" (capitalized, two words) when referring to the infrastructure binary
- Always write "AI agent" or "orchestrated agent" when referring to Claude Code, Codex, or OpenCode
- Never use bare "agent" without qualification in documentation or comments
- In code, struct/variable names may use `Agent` alone when the context is unambiguous within a single package

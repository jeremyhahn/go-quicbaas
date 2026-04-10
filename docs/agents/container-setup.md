# Container Setup

Docker containers for running orchestrated AI agents (Claude Code, Codex, OpenCode) with QuicBaaS integration.

Source: `deployments/agents/`

## Architecture Overview

```
docker-compose.yml
    |
    +-- quicbaas (server)          :8080 REST/MCP, :50051 gRPC, :4433 QUIC
    +-- ollama (local LLM)         :11434
    +-- claude-code-agent          Claude Code CLI + MCP + gopls + Serena
    +-- codex-agent                Codex CLI + MCP + gopls + Serena
    +-- opencode-agent             OpenCode CLI + MCP + gopls + Serena
```

All agent containers follow the same pattern: Alpine 3.21 base, non-root `agent` user (UID 1001), workspace at `/workspace`, MCP config auto-generated at startup.

## Container Details

### Claude Code Agent

| Component | Details |
|-----------|---------|
| Base | Alpine 3.21 + Node.js 20 LTS |
| CLI | `@anthropic-ai/claude-code@latest` |
| LSP | `gopls` (built in Go 1.23 stage) |
| Semantic | Serena MCP server (Python) |
| Auth | `ANTHROPIC_API_KEY` env or mounted `~/.claude` OIDC credentials |
| Config dir | `/home/agent/.claude` |

### Codex Agent

| Component | Details |
|-----------|---------|
| Base | Alpine 3.21 + Node.js 20 LTS |
| CLI | `@openai/codex@latest` |
| LSP | `gopls` |
| Semantic | Serena MCP server |
| Auth | `OPENAI_API_KEY` env |
| Config dir | `/home/agent/.codex` |

### OpenCode Agent

| Component | Details |
|-----------|---------|
| Base | Alpine 3.21 (multi-stage with Go 1.23) |
| CLI | `github.com/opencode-ai/opencode@latest` (built from source) |
| LSP | `gopls` |
| Semantic | Serena MCP server |
| Auth | `OPENAI_API_KEY` env (required) |
| Config dir | `/home/agent/.config/opencode` |

## Fleet Registration and Heartbeat

Every entrypoint script performs the same registration lifecycle:

1. **Register**: `POST /api/v1/orchestrator/agents` with hostname, agent type, backend, work directory
2. **Heartbeat**: Background loop every 30s via `POST /api/v1/orchestrator/agents/{id}/heartbeat`
3. **Deregister**: `DELETE /api/v1/orchestrator/agents/{id}` on exit (via `trap EXIT`)

Agent ID format: `{hostname}-{unix_timestamp}`

Registration is non-fatal: if QuicBaaS is unreachable, the agent starts anyway and retries on next heartbeat.

## Bind Mount Configuration

### Workspace

Each agent mounts its workspace directory:

```yaml
volumes:
  - ${CLAUDE_WORKSPACE:-./workspaces/claude-code}:/workspace
```

For shared isolation mode, all agents should mount the same workspace directory. For worktree isolation mode, each gets a separate directory.

### Credentials (Claude Code)

```yaml
volumes:
  - ${HOME}/.claude:/home/agent/.claude
  - ${HOME}/.cache/claude:/home/agent/.cache/claude
  - ${HOME}/.local/share/claude:/home/agent/.local/share/claude
  - ${HOME}/.config/claude:/home/agent/.config/claude
```

This mounts host OIDC credentials into the container, avoiding the need for API keys.

## MCP Configuration

Each entrypoint writes `/workspace/.mcp.json` with:

```json
{
  "mcpServers": {
    "quicbaas": {
      "type": "http",
      "url": "http://quicbaas:8080/mcp"
    },
    "serena": {
      "type": "stdio",
      "command": "serena",
      "args": ["--workspace", "/workspace"]
    }
  }
}
```

Serena is auto-detected: if the `serena` binary is not found, its block is omitted.

## gopls and Serena Integration

All containers include:

- **gopls**: Go Language Server Protocol for code intelligence (installed in a separate build stage to keep the runtime image small)
- **Serena**: Semantic code MCP server that provides symbol navigation, rename, find references via LSP. Runs as a stdio MCP server connected to gopls.

## GPU Compose Override

For GPU-accelerated Ollama inference:

```bash
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up
```

The GPU override adds NVIDIA device reservations to the Ollama container:

```yaml
deploy:
  resources:
    reservations:
      devices:
        - driver: nvidia
          capabilities: [gpu, compute, utility]
```

## Environment Variables

| Variable | Required | Default | Used By |
|----------|----------|---------|---------|
| `QUICBAAS_ADDRESS` | Yes | - | All agents |
| `ANTHROPIC_API_KEY` | No (OIDC fallback) | - | Claude Code |
| `ANTHROPIC_BASE_URL` | No | `https://api.anthropic.com` | Claude Code |
| `OPENAI_API_KEY` | Yes (Codex/OpenCode) | - | Codex, OpenCode |
| `OPENAI_BASE_URL` | No | `https://api.openai.com` | Codex, OpenCode |
| `QUICBAAS_TENANT_ID` | No | `default` | All agents |
| `CLAUDE_E2E_MODE` | No | `true` | Claude Code (skips permissions) |
| `CODEX_E2E_MODE` | No | `true` | Codex (enables --full-auto) |

## Resource Limits

| Service | CPU Limit | Memory Limit |
|---------|-----------|-------------|
| QuicBaaS server | 2 | 1G |
| Ollama | 4 | 8G |
| Each agent | 1 | 512M |

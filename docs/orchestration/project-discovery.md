# Project Discovery

The orchestrator discovers agent-enabled projects by scanning a directory tree for convention-based marker files. Discovery is the first step before registration, session spawning, or task decomposition.

## How It Works

The `quicbaas_orchestrator_discover` MCP tool calls `AgentService.DiscoverProjects`, which:

1. Validates and resolves the `root_path` to an absolute path
2. Reads the immediate children of the root directory
3. For each child directory, calls `scanProjectDir` which checks for:
   - `CLAUDE.md` in the directory root (project root marker)
   - `.claude/agents/*.md` files (agent definitions)
4. Also checks the root directory itself as a potential project
5. Returns all directories that contain at least one marker

A directory is considered a project if it has a `CLAUDE.md` file, agent definition files, or both.

## Marker Files

| File | Purpose |
|------|---------|
| `CLAUDE.md` | Marks a directory as a project root. Contains project instructions and conventions. |
| `.claude/agents/<name>.md` | Defines a subagent. Each `.md` file in this directory is collected as an agent file. |

## Discovered Project Fields

Each discovered project contains:

```json
{
  "name": "software",
  "path": "/mnt/vm-host-1/automatethethings/software",
  "has_claude_md": true,
  "has_agents": true,
  "agent_files": [
    "/mnt/vm-host-1/automatethethings/software/.claude/agents/go-quicbaas.md"
  ]
}
```

## Example Directory Structure

```
/mnt/vm-host-1/automatethethings/
  software/
    CLAUDE.md
    .claude/agents/
      backend-architect.md
      golang-pro.md
  another-project/
    CLAUDE.md
  not-a-project/
    main.go
```

Scanning `/mnt/vm-host-1/automatethethings/` discovers `software` (has both markers) and `another-project` (has `CLAUDE.md`). The `not-a-project` directory is skipped.

## MCP Tool

```
Tool:     quicbaas_orchestrator_discover
Required: root_path (string) - absolute path to scan
```

## Registration

After discovery, projects are registered via `quicbaas_orchestrator_register` which stores them in a `sync.Map` keyed by a generated project ID. Registration adds configuration that discovery does not capture:

- `dependencies` - IDs of projects this project depends on
- `default_backend` - preferred agent backend (e.g., "claude", "ollama")
- `default_model` - preferred model for sessions
- `labels` - key-value metadata for categorization

Registered projects are stored in-memory. Tenant-scoped persistence is planned for a future release.

## Scan Depth

Discovery scans one level deep from the root path plus the root itself. It does not recurse into nested subdirectories. To discover projects in a deep hierarchy, call discover on each parent directory or restructure projects as immediate children of the scan root.

## Build Tags

Project discovery is always compiled. The MCP tool layer requires the `mcp` build tag.

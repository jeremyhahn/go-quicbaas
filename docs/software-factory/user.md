# Software Factory — User Guide

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Task-oriented usage, in the order tasks are done: discover and register a
> project, tune the prompts, manage pooled accounts. Binding the SDLC module,
> handing over an issue and unblocking the factory are in
> [../sdlc/user.md](../sdlc/user.md); projects and accounts have their own
> guides in [../projects/user.md](../projects/user.md) and
> [../accounts/user.md](../accounts/user.md). Feature folder:
> `docs/software-factory/`. Conflicts with this document are defects.

Starting the server, spawning and prompting sessions, tasks, fleet setup,
inference and the container and Claude Code setup guides are in
[../orchestration/user.md](../orchestration/user.md). The tasks below assume a
running server and a spawned session.

## Discover and register agentic projects

The orchestrator discovers agent-enabled projects by scanning a directory tree
for convention-based marker files. Discovery is the first step before
registration, session spawning, or task decomposition; creating the Project
record itself is `quicbaas project create` ([projects/cli.md](../projects/cli.md)),
and the record it creates is described in [projects/](../projects/architecture.md).

### How It Works

The `quicbaas_orchestrator_discover` MCP tool calls `AgentService.DiscoverProjects`, which:

1. Validates and resolves the `root_path` to an absolute path
2. Reads the immediate children of the root directory
3. For each child directory, calls `scanProjectDir` which checks for:
   - `CLAUDE.md` in the directory root (project root marker)
   - `.claude/agents/*.md` files (agent definitions)
4. Also checks the root directory itself as a potential project
5. Returns all directories that contain at least one marker

A directory is considered a project if it has a `CLAUDE.md` file, agent definition files, or both.

### Marker Files

| File | Purpose |
|------|---------|
| `CLAUDE.md` | Marks a directory as a project root. Contains project instructions and conventions. |
| `.claude/agents/<name>.md` | Defines a subagent. Each `.md` file in this directory is collected as an agent file. |

### Discovered Project Fields

Each discovered project contains:

```json
{
  "name": "software",
  "path": "/workspace/software",
  "has_claude_md": true,
  "has_agents": true,
  "agent_files": [
    "/workspace/software/.claude/agents/go-quicbaas.md"
  ]
}
```

### Example Directory Structure

```
/workspace/
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

Scanning `/workspace/` discovers `software` (has both markers) and `another-project` (has `CLAUDE.md`). The `not-a-project` directory is skipped.

### MCP Tool

```
Tool:     quicbaas_orchestrator_discover
Required: root_path (string) - absolute path to scan
```

### Registration

After discovery, projects are registered via `quicbaas_orchestrator_register` which stores them in a `sync.Map` keyed by a generated project ID. Registration adds configuration that discovery does not capture:

- `dependencies` - IDs of projects this project depends on
- `default_backend` - preferred agent backend (e.g., "claude", "ollama")
- `default_model` - preferred model for sessions
- `labels` - key-value metadata for categorization

Registered projects are stored in-memory. Tenant-scoped persistence is planned for a future release.

### Scan Depth

Discovery scans one level deep from the root path plus the root itself. It does not recurse into nested subdirectories. To discover projects in a deep hierarchy, call discover on each parent directory or restructure projects as immediate children of the scan root.

### Build Tags

Project discovery is always compiled. The MCP tool layer requires the `mcp` build tag.

## Bind a project and follow one issue

Binding a project to a forge, following one issue to a merged pull
request and working the attention inbox are in
[../sdlc/user.md](../sdlc/user.md).

## Tune the prompts

**Not available from a running node yet.** Every instruction the Software
Factory sends an agent — the worker charter, the review framing, the lead's
decomposition prompt — is now a markdown file embedded in the binary rather
than a Go constant, so changing one is editing
`pkg/sdlc/promptdefaults/sdlc/*.md` or
`pkg/orchestration/promptdefaults/workflow/*.md` and rebuilding. Those files
are the system tenant's defaults. The tenant- and project-scoped, editable,
versioned store that will let you change one from the node or the GUI is
specified in [prompt-sets.md](prompt-sets.md).

## Manage pooled provider accounts

A tenant can hold a pool of provider logins — Anthropic/Claude seats,
OpenAI/Codex keys, OpenCode provider configs — and spawn a session against one
instead of the host's shared identity. Import one, make it the default and
spawn against it:

```bash
claude setup-token > ./token.txt
jq -n --arg t "$(cat ./token.txt)" '{access_token: $t}' \
  | quicbaas account create --provider claude \
      --display-name team-seat-1 --refresh-policy none --credential-file -

quicbaas orchestrator spawn --backend claude --account-auto
```

The verbs are in [../accounts/cli.md](../accounts/cli.md#quicbaas-account);
how the credential reaches the worker host, how a limit hold rotates a seat and
how cost is attributed are in [../accounts/user.md](../accounts/user.md).

## Next steps

- [../orchestration/user.md](../orchestration/user.md): multi-machine setup,
  environment variables, finding things, fleet quick start, inference and
  container setup.
- [../sdlc/user.md](../sdlc/user.md) and [../projects/user.md](../projects/user.md)
  for the modules above.

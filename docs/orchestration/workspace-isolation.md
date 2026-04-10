# Workspace Isolation

Controls how orchestrated AI agents share or isolate their working copies of the codebase.

Source: `pkg/orchestration/workflow_types.go`

## Modes

| Mode | Constant | Description |
|------|----------|-------------|
| `shared` | `IsolationShared` | All agents edit the same files in a single workspace. File locks are mandatory. |
| `worktree` | `IsolationWorktree` | Each agent gets an isolated git worktree. File locks are advisory. |

Default: `shared` (applied by `ApplyDefaults()`).

## Shared Mode

All agents operate on the same filesystem directory. Concurrent edits to the same file will cause data corruption unless coordinated.

**Requirements:**
- File locking must be enforced (use `quicbaas_orch_file_lock` before editing)
- Agents must check lock status before writing
- The lead agent should assign non-overlapping file sets when possible

**When to use:**
- Simple workflows where agents work on different files
- Small teams (2-3 agents) with clear task boundaries
- When merge conflicts are unacceptable

```
/workspace/
  main.go        <-- locked by agent-1
  handler.go     <-- locked by agent-2
  service.go     <-- unlocked (available)
```

## Worktree Mode

Each agent receives its own git worktree branched from the main working tree. Changes are merged back after task completion.

**Behavior:**
- File locks are advisory only -- agents can override each other's locks with a warning
- Each agent's edits are isolated until merge
- Merge conflicts are surfaced at merge time, not during editing

**When to use:**
- Large workflows with many agents editing overlapping files
- When agents need to experiment without affecting others
- When merge-at-completion is acceptable

```
/workspace/              <-- main worktree (lead agent)
/workspace/.worktrees/
  agent-1/               <-- isolated copy
  agent-2/               <-- isolated copy
  agent-3/               <-- isolated copy
```

## Merge Conflict Handling (Worktree Mode)

When a worktree is merged back:

1. The orchestrator attempts a fast-forward merge
2. If fast-forward fails, a three-way merge is attempted
3. If conflicts occur, the lead agent (or a designated validator) receives the conflict details
4. Resolution strategy depends on workflow configuration:
   - **Manual**: Lead agent resolves conflicts
   - **Rebase**: Worker rebases onto the latest main and retries
   - **Abort**: The worker's changes are discarded

## Configuration

```yaml
settings:
  isolation: shared    # shared | worktree
  file_locking:
    lease_ttl: 5m      # More important in shared mode
    heartbeat_interval: 30s
    max_renewals: 60
```

## Decision Matrix

| Factor | Shared | Worktree |
|--------|--------|----------|
| File conflict risk | High (mitigated by locks) | Low (isolated until merge) |
| Lock enforcement | Mandatory | Advisory |
| Disk usage | 1x | Nx (one copy per agent) |
| Merge step required | No | Yes |
| Agent count | 2-3 recommended | Any |
| Real-time visibility | Immediate | At merge time |

# File Locking

Lease-based distributed file locks for coordinating file access among orchestrated AI agents.

Source: `pkg/orchestration/file_lock.go`, `pkg/orchestration/file_lock_store.go`

## Overview

When multiple AI agents (Claude Code, Codex, OpenCode) work on the same codebase, file locking prevents destructive concurrent edits. The system uses CAS (compare-and-swap) semantics backed by the KV store, leases for automatic expiry, and optional Raft fencing tokens for split-brain protection.

## Lock Structure

```go
type FileLock struct {
    LockKey      string // Deterministic hash: filelock:{tenant}:{project}:{sha256(path)}
    AgentID      string // Holding agent
    TenantID     string
    ProjectID    string
    FilePath     string
    LeaseID      string // Lease manager reference
    FencingToken uint64 // Raft term at acquisition (0 if standalone)
    AcquiredAt   int64  // Unix nanos
    Isolation    string // "shared" or "worktree"
}
```

## Lock Acquisition Flow

```
Agent                     FileLockService                 KV Store / LeaseManager
  |                           |                                |
  |-- Acquire(path) --------->|                                |
  |                           |-- loadLock(key) -------------->|
  |                           |<-- (existing lock or nil) -----|
  |                           |                                |
  |                           |   [if exists & lease valid]:   |
  |                           |     shared mode: reject if     |
  |                           |       different agent          |
  |                           |     worktree mode: warn, allow |
  |                           |                                |
  |                           |-- GetShardRaftState() -------->|  (fencing token)
  |                           |-- CreateLease(key, TTL) ------>|
  |                           |-- storeLock(lock) ------------>|
  |                           |                                |
  |<-- FileLock --------------|                                |
```

1. Build deterministic lock key from `sha256(filePath)`
2. Check KV store for existing lock
3. If lock exists with valid lease:
   - **Shared mode**: Reject with `FileLockedError` if held by different agent. Same-agent re-acquire is idempotent.
   - **Worktree mode**: Log warning, allow override (advisory lock).
4. If lock exists with expired lease: overwrite (crash recovery).
5. Obtain fencing token from Raft term (or 0 in standalone mode).
6. Create lease with configurable TTL.
7. Persist lock to KV store.

## Configurable TTL

| Setting | Default | Description |
|---------|---------|-------------|
| `lease_ttl` | `5m` | How long a lock remains valid without renewal |
| `heartbeat_interval` | `30s` | How often an agent should renew its lock |
| `max_renewals` | `60` | Maximum number of lease renewals |

Total maximum hold time: `lease_ttl + (heartbeat_interval * max_renewals)` = 5m + 30m = 35 minutes with defaults.

## Shared vs Worktree Isolation

| Behavior | Shared Mode | Worktree Mode |
|----------|-------------|---------------|
| Lock enforcement | Mandatory (reject) | Advisory (warn + allow) |
| Re-acquire by different agent | `FileLockedError` | Allowed with warning |
| Same-agent re-acquire | Idempotent | Idempotent |
| Use case | All agents edit same files | Each agent has isolated git worktree |

## Raft vs Standalone Compatibility

**With Raft**: The fencing token is set to the current Raft term at lock acquisition. On renewal, the token is validated against the current term. If the term has advanced (leader changed), the lock is stale and `StaleLockError` is returned.

**Without Raft** (`raftNode == nil`): Fencing tokens are set to 0 and validation is skipped. Lease-based expiry is the sole safety mechanism.

Fencing token validation logic:
- Token == 0: skip validation (standalone mode)
- raftNode == nil: skip validation
- Token < current term: return `StaleLockError`
- Token >= current term: valid

## Crash Recovery

When an agent crashes, its locks are recovered via lease TTL expiry:

1. Agent crashes -- no explicit release
2. Heartbeat stops -- lease is not renewed
3. Lease TTL expires
4. Next `Acquire()` or `IsLocked()` call detects expired lease
5. Stale lock is overwritten or cleaned up

`ListLocks()` also cleans up stale locks on read.

## FileLockService Interface

```go
type FileLockService interface {
    Acquire(ctx, tenantID, projectID, filePath, agentID) (*FileLock, error)
    Release(ctx, tenantID, lockKey) error
    Renew(ctx, tenantID, lockKey) error
    IsLocked(ctx, tenantID, projectID, filePath) (*FileLock, bool, error)
    WaitForRelease(ctx, tenantID, lockKey) error
    ListLocks(ctx, tenantID, projectID) ([]*FileLock, error)
}
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `quicbaas_orch_file_lock` | Acquire a lock on a file path |
| `quicbaas_orch_file_unlock` | Release a previously acquired lock |
| `quicbaas_orch_file_lock_status` | Check lock status and list active locks |

## Configuration

```yaml
settings:
  file_locking:
    lease_ttl: 5m
    heartbeat_interval: 30s
    max_renewals: 60
```

## Error Types

| Error | Sentinel | Condition |
|-------|----------|-----------|
| `FileLockedError` | `ErrFileLocked` | File locked by another agent (shared mode) |
| `LockNotFoundError` | `ErrLockNotFound` | Lock key does not exist |
| `StaleLockError` | `ErrStaleLock` | Fencing token behind current Raft term |
| `LockReleaseNotOwnerError` | `ErrLockReleaseNotOwner` | Release by non-holder |
| `FileLockValidationError` | `ErrFileLockValidation` | Missing required field |

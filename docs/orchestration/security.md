# Agent Orchestration Security

## Multi-Tenancy

All orchestration data is tenant-scoped. The tenant ID is extracted from the request context via `iam.TenantFromContext(ctx)`. Projects, sessions, cache entries, and vector namespaces are isolated per tenant. Cross-tenant access is not possible through the MCP tool interface.

## Encryption at Rest

| Data Type          | Storage Backend    | Encryption                          |
|--------------------|--------------------|-------------------------------------|
| Project catalog    | KV Store           | ObjectEncryption (AES-256-GCM)      |
| Backend credentials| Secrets Store      | Envelope encryption                 |
| Backend config     | Parameter Store    | Encrypted parameters                |
| Cache entries      | KV Store           | ObjectEncryption (AES-256-GCM)      |
| Vector embeddings  | Vector Store       | Namespace-level encryption           |

Backend API keys (Anthropic, Ollama endpoints) are stored in the Secrets Store and never exposed through MCP tool responses.

## Access Control

### Per-Object ACL

Every stored object (project registrations, cache entries) supports per-object ACL through the standard quicbaas ObjectEncryption and ACL mechanisms. The service layer enforces ACL checks before returning data.

### RBAC Permissions

The following permissions govern agent orchestration operations:

| Permission            | Grants Access To                              |
|-----------------------|-----------------------------------------------|
| `agent:discover`      | Scan directories for agent-enabled projects    |
| `agent:register`      | Register and list projects                     |
| `agent:spawn`         | Create persistent agent sessions               |
| `agent:prompt`        | Send prompts to sessions or backends           |
| `agent:close`         | Terminate active sessions                      |
| `agent:decompose`     | Decompose tasks into work items                |
| `agent:cache:read`    | Read from the agent cache                      |
| `agent:cache:write`   | Write to the agent cache                       |
| `agent:admin`         | Full access including backend registration     |

Assign permissions through IAM roles. A typical developer role includes `agent:discover`, `agent:register`, `agent:spawn`, `agent:prompt`, `agent:close`, and `agent:cache:read`. Admin operations like `agent:admin` should be restricted to platform operators.

## Callback Path Validation

When ACP backends (Claude Code, Codex) request file system operations via callbacks, the `CallbackHandler` enforces strict path-based access control.

### Validation Steps

1. **Path cleaning** -- `filepath.Clean()` normalizes the requested path, removing `.` and collapsing `..` segments.

2. **Symlink resolution** -- `filepath.EvalSymlinks()` resolves the real path. If the target file does not exist yet (write operations), the parent directory is resolved instead.

3. **Traversal detection** -- `filepath.Rel()` computes the relative path from each allowed directory to the resolved path. If the relative path starts with `..`, access is denied.

4. **Allowed directory check** -- The handler is initialized with a list of allowed paths (typically project work directories). Only paths within these directories pass validation.

### Supported Callbacks

| Callback                       | Path Validated | Action                      |
|--------------------------------|----------------|-----------------------------|
| `fs/read_text_file`            | Yes            | Read file contents          |
| `fs/write_text_file`           | Yes            | Write file contents         |
| `session/request_permission`   | Yes (if path)  | Approve/deny path access    |

### Example

```go
// pkg/orchestration/callbacks.go — there is no pkg/acp; the callback
// handler is a file inside pkg/orchestration.
handler := orchestration.NewCallbackHandler([]string{
    "/home/user/projects/my-service",
    "/home/user/projects/shared-lib",
})
// Requests to /home/user/projects/my-service/main.go --> allowed
// Requests to /etc/passwd --> denied
// Requests to /home/user/projects/my-service/../../etc/passwd --> denied
```

## Sandbox Bind Mounts

Every host path a spawn request would bind-mount into an agent container is
authorized by `orchestration.MountPolicy` inside `SpawnSession`, before any side
effect. A rejected spawn has started no container and called no backend.

Three request fields reach a container runtime, and all three are
caller-controlled:

| Field | When it is authorized | Where it would be mounted |
|-------|----------------------|---------------------------|
| `work_dir` | Containerized spawns | `/workspace` (ACP backends) or the same path (orchestrator sandbox path), read-write |
| `container_config.mounts` | Containerized spawns | Verbatim, as the caller wrote them |
| `extra_mounts` | Always | Appended to the container spec |

A source is permitted only when it lies under:

- `<orchestration.projects_root>/<tenant_id>/` -- the **requesting tenant's own**
  subtree of the managed projects root, or
- one of `orchestration.sandbox.allowed_mount_roots`.

Everything else is denied with a typed `MountNotAllowedError`, including every
mount when no policy is wired at all.

Three properties are load-bearing:

1. **Tenant scoping.** The projects root is shared
   (`<root>/<tenant_id>/<project_id>/workspace`), so the grant is the tenant's
   subtree, never the root. Directory permissions cannot substitute for this:
   the sandbox container runs as the daemon's own uid, which owns every tenant's
   workspace.
2. **Resolved, not lexical.** Each source is resolved with
   `filepath.EvalSymlinks` and re-tested against the roots, so an agent that
   plants a symlink inside its own workspace cannot have the symlink's target
   mounted on the next spawn. A source that does not exist is refused.
3. **Read-write, as the daemon user.** This is why an unchecked `work_dir` of
   `/` is host root read-write for the agent, and why the check lives in
   `SpawnSession` rather than in either backend.

Raw (non-sandboxed) execution bind-mounts nothing, so its work directory is not
a mount source and is not policy-checked. What isolates a raw agent from the
host is the sandbox axis (`sandbox_policy`), not the work directory.

```yaml
orchestration:
  projects_root: /var/lib/quicbaas/projects
  sandbox:
    # Shared deployment-wide; NOT tenant-scoped. Do not list a directory
    # holding one tenant's data.
    allowed_mount_roots:
      - /srv/models
```

Operator notes:

- A workflow agent spec that pairs `sandboxed: true` with a `workspace_path`
  outside the projects root needs that path in `allowed_mount_roots`, otherwise
  the spawn is refused.
- A spawn dispatched to a remote agent daemon is authorized **twice**: against
  the control plane's roots here, and again on the daemon against its own
  `allowed_mount_roots` (see
  [agent daemon](../agents/daemon.md#bind-mount-allowlist-reference)). Both must
  permit the path. The daemon's check is not a duplicate — a spawn whose
  control-plane-resolved mode is not `required` skips the `work_dir`
  authorization above, while the daemon's own `sandbox_policy` may still
  containerize it — and the daemon's filesystem layout is its own. A daemon with
  no roots configured refuses every sandboxed spawn that names a work
  directory.
- The in-process **embedded** agent enforces this same policy, so a spawn
  dispatched over its bufconn cannot bypass the check.

### Policy denials on the wire

A refused mount, sandbox mode, network, or trust tier is an authorization
decision about the request, not a server fault, and every transport says so.
`orchestration.IsPolicyDenial` is the single classifier all of them share:

| Transport | Status | Message |
|-----------|--------|---------|
| REST | `403 Forbidden` | The typed error's own text |
| gRPC | `PermissionDenied` | The typed error's own text |
| QUIC | `StatusPermissionDenied` | The typed error's own text |
| MCP | `OperationNotAllowed` (-32010) | The typed error's own text |
| Agent daemon (gRPC) | `PermissionDenied` for a refused mount; the control plane re-surfaces it as 403 | The typed error's own text |

The message names the offending field (`work_dir`, `extra_mounts`,
`container_config.mounts`), the tenant, the path, and the reason, so an operator
can tell a missing `allowed_mount_roots` entry from an outage. MCP deliberately
uses `OperationNotAllowed` rather than `PermissionDenied`: the latter answers
with an OAuth `insufficient_scope` challenge, which would send a client off to
acquire a broader token for a denial no token can lift.

## Sandbox Network

Container isolation has a second dimension: a container run with
`--network host` joins the host's own network namespace and can dial every
loopback-bound service on it, so a "sandboxed" agent is only half sandboxed.
Sandboxed sessions therefore default to an isolated namespace with egress
(`bridge` -- see `orchestration.SandboxNetwork`), and host networking is
reachable only as an explicit operator decision
(`ACPBackendConfig.SandboxNetwork`).

`SpawnSession` clamps the request side of this, next to the mount and trust
checks:

- A `container_config` naming `host` (in any spelling) fails the spawn with a
  typed `SandboxNetworkNotAllowedError`, before any container is created.
- Any other caller-supplied network is replaced by the operator-resolved value.
  The network is configuration, so the request has no say in it -- the only
  thing it can do is fail.

`SpawnRequest` carries no network field at all, so no tenant path reaches the
backend's own container launch either.

## Trust Policy

The trust axis is orthogonal to the sandbox axis: `sandbox_policy` answers "is
the agent isolated from the host", `trust_policy` answers "is the agent the code
we approved, and can the host prove it".

```yaml
orchestration:
  trust_policy: pinned   # opt-in (default) | pinned | attested | measured
```

| Policy | Floor | Requires |
|--------|-------|----------|
| `opt-in` | none | nothing (default; a session gets a tier only if it asks) |
| `pinned` | Tier 0 | container launched from an immutable image digest, verified after launch |
| `attested` | Tier 1 | Tier 0 plus a host TPM quote verified against the session's expected PCRs |
| `measured` | Tier 2 | Tier 1 plus measured launch (image + launch config extended into a PCR before start) |

`SpawnSession` resolves every request against this floor next to the sandbox
mode, and the **resolved** level -- never the caller's -- is what reaches the
container spec and the backend:

- A request naming no level runs at the floor.
- A request naming a **weaker** level is refused (`TrustLevelViolationError`),
  so a caller cannot opt out of the operator's baseline.
- A tier the target host cannot prove is refused, never downgraded. Raising the
  policy beyond what the deployment's runtime supports therefore fails spawns
  closed. A remote (daemon-dispatched) spawn advertises no local capability, so
  it is refused above `opt-in` until that daemon proves the tier itself.
- The resolved level is persisted on the session record and **re-resolved** on
  resume, so a floor raised after a session started applies to the process being
  restarted.

A tenant policy override, on either axis, may only **tighten** the server
policy. A weaker override is ignored.

This section covers the policy that admits a spawn. The tiers themselves --
digest pinning and detached signatures, the deterministic configuration digest,
the host quote and its binding to the PCR read, measured launch, and the SDK
seams a fleet operator fills -- are documented in
[Container Trust](container-trust.md).

## Service Integration Pattern

Agent orchestration follows the same service integration pattern as Secrets and Scheduler:

1. The `AgentService` interface is defined in `pkg/service/`
2. The implementation resides behind the service layer with proper encapsulation
3. MCP tools in `pkg/api/mcp/` call the service layer (never business logic directly)
4. IAM enforcement occurs at the service layer before any data access
5. All errors are typed (never inline `fmt.Errorf`)

## Sandboxed Session Isolation

A sandboxed agent session is isolated along two independent dimensions:

| Dimension | Control | Default |
|---|---|---|
| Process/filesystem | `sandbox_policy` (`pkg/orchestration/sandbox_policy.go`) | container-sandboxed on agent daemons |
| Network namespace | `sandbox_network` (`pkg/orchestration/sandbox_network.go`) | `bridge` — own namespace with egress |

Neither dimension is caller-selectable in the unsafe direction: a spawn request
can ask for a *stricter* sandbox mode but carries no network field at all, so no
tenant or workflow can move a session onto the host network namespace. Only
operator configuration can (`--sandbox-network host`), and doing so is logged at
WARN.

Provider credentials in a sandboxed session's environment are passed to the
container runtime through a private `0600` `--env-file`, never as command-line
arguments, because `/proc/<pid>/cmdline` is world readable on a default Linux
host. See `docs/orchestration/accounts.md` and
`pkg/orchestration/container_env_file.go`.

## Recommendations

- Scope allowed callback paths to project work directories only
- Prefer managed project workspaces over `allowed_mount_roots`: a managed
  workspace is already tenant-scoped, an allowlist root is not
- Set `orchestration.trust_policy: pinned` wherever the container runtime can
  pin, so a mutable tag cannot be swapped under a running fleet
- Use separate tenants for production and development agent workloads
- Store all backend credentials in the Secrets Store, not in config files
- Assign minimal RBAC permissions per role
- Monitor session lifecycle events for anomalous activity
- Leave `sandbox_network` at `bridge`; reach for `host` only when the host's
  loopback services are already inside the agent's trust boundary
- Set TTL on cache entries to limit data retention

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
handler := acp.NewCallbackHandler([]string{
    "/home/user/projects/my-service",
    "/home/user/projects/shared-lib",
})
// Requests to /home/user/projects/my-service/main.go --> allowed
// Requests to /etc/passwd --> denied
// Requests to /home/user/projects/my-service/../../etc/passwd --> denied
```

## Service Integration Pattern

Agent orchestration follows the same service integration pattern as Secrets and Scheduler:

1. The `AgentService` interface is defined in `pkg/service/`
2. The implementation resides behind the service layer with proper encapsulation
3. MCP tools in `pkg/api/mcp/` call the service layer (never business logic directly)
4. IAM enforcement occurs at the service layer before any data access
5. All errors are typed (never inline `fmt.Errorf`)

## Recommendations

- Scope allowed callback paths to project work directories only
- Use separate tenants for production and development agent workloads
- Store all backend credentials in the Secrets Store, not in config files
- Assign minimal RBAC permissions per role
- Monitor session lifecycle events for anomalous activity
- Set TTL on cache entries to limit data retention

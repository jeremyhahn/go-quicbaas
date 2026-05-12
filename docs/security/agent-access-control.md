# Agent Access Control

QuicBaaS provides two distinct access-control mechanisms for managing agent lifecycle and project participation: **global agent disable/enable** (revokes all permissions across all projects) and **project-level access revocation** (removes agent from a single project). Both are reversible and emit audit events.

## Two Access-Control Levels

### 1. Global Agent Disable/Enable

Disabling an agent globally revokes its credentials and closes all active connections. This is used when an agent is compromised, retired, or needs immediate remediation.

| Operation | Effect | Reversible |
|-----------|--------|-----------|
| **Disable Agent** | Revokes OAuth2 client, removes mTLS cert pin, force-closes gRPC streams, sets Status=disabled | Yes (Enable Agent) |
| **Enable Agent** | Restores OAuth2 client, re-activates mTLS pin, restores Status | Yes (Disable Agent) |

**Use cases:**
- Agent private key compromised → Disable → Re-certificate → Enable
- Agent host decommissioned → Disable
- Audit remediation (agent exceeded quota) → Disable → Investigation → Enable after fix

### 2. Project-Level Access Revocation

Revoking project access removes an agent from a single project without affecting its other projects or global status. Used for fine-grained access control and workflow containment.

| Operation | Effect | Reversible |
|-----------|--------|-----------|
| **Revoke Project Access** | Sets project role to `denied`, cancels active project claims, emits audit event | Yes (Grant Role) |
| **Grant Project Role** | Sets project role (lead, worker, observer), allows claims, emits audit event | N/A |

**Use cases:**
- Remove a worker agent from a sensitive project → Revoke Access
- Rotate lead agents between projects → Revoke from old, Grant to new
- Temporary quarantine pending investigation → Revoke, then Grant after remediation

## Disable Agent (Global)

Globally disables an agent across all projects. All active work claims are cancelled.

### REST API

```
POST /api/v1/agents/{agent_id}/disable
Authorization: Bearer <token>

204 No Content
```

Success: agent is disabled; all subsequent operations fail with `PERMISSION_DENIED`.

**Errors:**

- `404 Not Found`: Agent does not exist
- `409 Conflict`: Agent is already disabled (`AgentAlreadyDisabledError`)

### gRPC RPC

```protobuf
rpc DisableAgent(DisableAgentRequest) returns (DisableAgentResponse) {}

message DisableAgentRequest {
    string tenant_id = 1;   // Tenant owning the agent
    string agent_id = 2;    // Agent to disable
}

message DisableAgentResponse {
    string agent_id = 1;           // Echoed from request
    bool disabled = 2;             // True if successfully disabled
    int64 disabled_at_unix = 3;    // Timestamp
}
```

### Service Implementation

From `pkg/service/agent_admin_service.go`:

```go
type AgentAdminService interface {
    DisableAgent(ctx context.Context, req *DisableAgentRequest) (*DisableAgentResponse, error)
}

type DisableAgentRequest struct {
    TenantID string
    AgentID  string
}

type DisableAgentResponse struct {
    AgentID    string
    Disabled   bool
    DisabledAt time.Time
    Timestamp  time.Time
}
```

The service:

1. Validates agent exists and belongs to tenant
2. Revokes OAuth2 client credentials (client_id/secret pair)
3. Removes mTLS certificate thumbprint pin from IAM
4. Emits `agent.disabled` audit event
5. Force-closes all active gRPC streams (context cancellation)
6. Sets `AgentRegistration.Status = disabled`

After disable, the agent cannot:
- Register or re-register
- Make any orchestrator API calls (REST, gRPC, QUIC, MCP)
- Claim tasks or run jobs

Active claims are cancelled; in-flight jobs transition to `failed` status.

## Enable Agent (Global)

Re-enables a previously disabled agent. The agent may re-register and resume operations.

### REST API

```
POST /api/v1/agents/{agent_id}/enable
Authorization: Bearer <token>

204 No Content
```

**Errors:**

- `404 Not Found`: Agent does not exist
- `409 Conflict`: Agent is not disabled (`AgentNotDisabledError`)

### gRPC RPC

```protobuf
rpc EnableAgent(EnableAgentRequest) returns (EnableAgentResponse) {}

message EnableAgentRequest {
    string tenant_id = 1;
    string agent_id = 2;
}

message EnableAgentResponse {
    string agent_id = 1;
    bool enabled = 2;
    int64 timestamp_unix = 3;
}
```

### Service Implementation

From `pkg/service/agent_admin_service.go`:

```go
type EnableAgentRequest struct {
    TenantID string
    AgentID  string
}

type EnableAgentResponse struct {
    AgentID   string
    Enabled   bool
    Timestamp time.Time
}
```

The service:

1. Validates agent exists and belongs to tenant
2. Issues new OAuth2 client credentials
3. Re-activates mTLS certificate (if TLS enabled)
4. Emits `agent.enabled` audit event
5. Sets `AgentRegistration.Status = online`

The agent must re-register to obtain new credentials. The daemon watches for enable events and triggers a registration refresh.

## Revoke Project Access

Removes an agent's access to a single project by setting its role to `denied`.

### REST API

```
PUT /api/v1/projects/{project_id}/permissions/{agent_id}
Authorization: Bearer <token>
Content-Type: application/json

{
  "role": "denied"
}

200 OK
{
  "agent_id": "agent-id",
  "project_id": "proj-id",
  "role": "denied",
  "revoked_at": "2025-04-26T12:00:00Z"
}
```

**Alternative: DELETE endpoint (shorthand for "revoke")**

```
DELETE /api/v1/projects/{project_id}/permissions/{agent_id}

204 No Content
```

This is equivalent to `PUT` with `role=denied`.

### gRPC RPC

```protobuf
rpc RevokeAgentRole(RevokeAgentRoleRequest) returns (RevokeAgentRoleResponse) {}

message RevokeAgentRoleRequest {
    string tenant_id = 1;
    string project_id = 2;
    string agent_id = 3;
}

message RevokeAgentRoleResponse {
    string agent_id = 1;
    string project_id = 2;
    string role = 3;                   // "denied"
    int64 revoked_at_unix = 4;
}
```

Semantically equivalent to:

```protobuf
rpc GrantAgentRole(GrantAgentRoleRequest) returns (GrantAgentRoleResponse) {}

message GrantAgentRoleRequest {
    string tenant_id = 1;
    string project_id = 2;
    string agent_id = 3;
    string role = 4;                   // "denied"
}
```

### Service Implementation

From `pkg/service/project_service.go`:

```go
// RevokeAgentRole sets project role to "denied" for an agent.
func (s *ProjectService) RevokeAgentRole(ctx context.Context, req *RevokeAgentRoleRequest) (*RevokeAgentRoleResponse, error) {
    // 1. Validate project exists and belongs to tenant
    // 2. Set role to "denied" in Permissions.AgentRoles map
    // 3. Cancel active claims for this agent in this project
    // 4. Emit "project.access.revoked" audit event
    // 5. Return response
}
```

The service:

1. Validates project and agent exist
2. Sets `Permissions.AgentRoles[agent_id] = "denied"`
3. Cancels all active task claims for the agent in that project
4. Emits `project.access.revoked` audit event (includes agent_id, project_id, reason)
5. Returns response

After revocation, the agent cannot:
- Claim queued tasks in the project
- Spawn child jobs under the project
- Run any workflows for the project

The agent remains online and active in other projects. Global operations are unaffected.

## Grant Agent Role

Grants or updates an agent's role in a project.

### REST API

```
PUT /api/v1/projects/{project_id}/permissions/{agent_id}
Authorization: Bearer <token>
Content-Type: application/json

{
  "role": "lead"
}

200 OK
{
  "agent_id": "agent-id",
  "project_id": "proj-id",
  "role": "lead",
  "granted_at": "2025-04-26T12:00:00Z"
}
```

Valid roles: `lead`, `worker`, `observer`, `denied`.

### gRPC RPC

```protobuf
rpc GrantAgentRole(GrantAgentRoleRequest) returns (GrantAgentRoleResponse) {}

message GrantAgentRoleRequest {
    string tenant_id = 1;
    string project_id = 2;
    string agent_id = 3;
    string role = 4;                   // "lead" | "worker" | "observer" | "denied"
}

message GrantAgentRoleResponse {
    string agent_id = 1;
    string project_id = 2;
    string role = 3;
    int64 granted_at_unix = 4;
}
```

## MCP Tools

Four MCP tools manage agent access:

| Tool | Purpose |
|------|---------|
| `agent_disable` | Disable an agent globally |
| `agent_enable` | Enable a previously disabled agent |
| `project_grant_agent` | Grant an agent a role in a project |
| `project_revoke_agent` | Revoke an agent's project access (role=denied) |

All tools enforce authorization. Callers must have appropriate IAM scopes.

Example (Python):

```python
# Disable agent globally
result = client.call_tool("agent_disable", {
    "tenant_id": "acme",
    "agent_id": "agent-prod-01"
})
# → {"agent_id": "agent-prod-01", "disabled": true}

# Revoke project access
result = client.call_tool("project_revoke_agent", {
    "tenant_id": "acme",
    "project_id": "proj-uuid",
    "agent_id": "agent-prod-01"
})
# → {"agent_id": "agent-prod-01", "role": "denied"}

# Re-enable agent after remediation
result = client.call_tool("agent_enable", {
    "tenant_id": "acme",
    "agent_id": "agent-prod-01"
})
# → {"agent_id": "agent-prod-01", "enabled": true}
```

## CLI Usage

```bash
# Disable agent globally
quicbaas agent disable --tenant acme --agent agent-prod-01
# → Agent disabled. Active claims cancelled.

# Enable agent after remediation
quicbaas agent enable --tenant acme --agent agent-prod-01
# → Agent enabled. Re-register to obtain new credentials.

# Revoke project access
quicbaas project revoke-role \
  --tenant acme \
  --project proj-uuid \
  --agent agent-prod-01
# → Access revoked. Active claims in this project cancelled.

# Grant project role
quicbaas project grant-role \
  --tenant acme \
  --project proj-uuid \
  --agent agent-prod-01 \
  --role lead
# → Role granted.

# List agent registrations with status
quicbaas agent list --tenant acme
# Shows status: online | offline | disabled | error

# Check agent status
quicbaas agent get --tenant acme --agent agent-prod-01
# Shows: status, last_heartbeat, registration_time, permissions
```

## UI Integration

### AgentDetailView (Agents Page)

- **Disable/Enable buttons** in agent detail card
  - Click "Disable" → confirmation modal → agent status changes to "disabled"
  - Click "Enable" → agent status returns to "online"
- **Last heartbeat**, **Status**, **Registration time** displayed
- **Permissions matrix** tab shows all projects and roles

### ProjectsView (Projects Page)

- **Permissions matrix** shows agent role for each project (lead, worker, observer, denied)
- **Revoke button** next to each agent
  - Click "Revoke" → confirmation modal → role set to "denied", button greys out
- **Grant button** with role selector to re-grant after revocation

## Audit Events

All access-control operations emit audit events for compliance and forensics:

| Event Type | Payload | Trigger |
|------------|---------|---------|
| `agent.disabled` | `{agent_id, tenant_id, timestamp}` | DisableAgent RPC |
| `agent.enabled` | `{agent_id, tenant_id, timestamp}` | EnableAgent RPC |
| `project.access.revoked` | `{agent_id, project_id, tenant_id, timestamp}` | RevokeAgentRole RPC |
| `project.access.granted` | `{agent_id, project_id, role, tenant_id, timestamp}` | GrantAgentRole RPC |

All events include the calling principal (user, service account, or agent) and are logged to the audit trail. See `docs/security/audit.md` for retention and query details.

## Flow Diagrams

### Disable → Remediate → Enable

```
[Agent Online]
     ↓
[Compromised / Audit Trigger]
     ↓
[DisableAgent] → Revoke OAuth2, mTLS pin
                 Force-close streams
                 Cancel all claims
     ↓
[Agent Disabled]
     ↓
[Remediation: Re-certificate, Key rotation, etc.]
     ↓
[EnableAgent] → Issue new OAuth2, mTLS pin
                Set status=online
                Agent re-registers with new creds
     ↓
[Agent Online] → Ready for dispatch
```

### Revoke & Re-grant Project Access

```
[Agent has role=lead in Project A]
     ↓
[Need to limit scope]
     ↓
[RevokeAgentRole] → Set role=denied
                     Cancel active claims in Project A
                     Agent remains online, other projects unaffected
     ↓
[Agent denied in Project A]
     ↓
[Later: New team member joins]
     ↓
[GrantAgentRole] → Set role=worker
                    Agent can claim tasks in Project A again
     ↓
[Agent can work in Project A]
```

## Cross-References

- Authorization & RBAC: `docs/security/authorization.md`
- Audit logging: `docs/security/audit.md`
- Agent registration: `docs/agents/daemon.md#identity--registration`
- Project permissions: `docs/orchestration/projects.md#permission-roles`
- mTLS setup: `docs/security/tls-setup.md`

## Source Files

- `pkg/service/agent_admin_service.go` — AgentAdminService interface and types
- `pkg/service/agent_admin_service_impl.go` — Implementation
- `pkg/service/project_service.go` — GrantAgentRole, RevokeAgentRole RPCs
- `pkg/api/rest/handlers_agent.go` — REST endpoints for disable/enable
- `pkg/api/rest/handlers_project.go:451-560` — REST endpoints for grant/revoke
- `pkg/api/grpc/server_agent.go` — gRPC service for agent admin
- `pkg/api/grpc/server_project.go` — gRPC service for project permissions
- `pkg/api/mcp/agent_tools.go` — MCP tool definitions
- `pkg/api/mcp/project_tools.go` — MCP tool definitions

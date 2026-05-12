# Projects

Projects are first-class orchestration entities that group agentic work with dedicated workspace, permissions, and resource quotas. Each project belongs to a tenant and may span multiple agents and hosts.

## Concept

Projects sit in the hierarchy: Tenant → Project → Workflow → Job → Task.

A project encapsulates:

- **Workspace**: a filesystem location (agent + path) where agent operations occur
- **Host affinity**: preferred agent host for project work (optional)
- **Git metadata**: remote repository URL, default branch, current HEAD SHA
- **Lead agent**: default agent ID for spawning workflows (optional; job-level override allowed)
- **Permissions matrix**: per-agent role assignments (lead, worker, observer, denied)
- **Quotas**: LLM token budget, GPU minutes, max concurrent runs (0 = unlimited)

## Data Model

All projects and their fields are defined in `pkg/orchestration/project.go`:

```go
type Project struct {
    ID               string
    TenantID         string
    Name             string
    Description      string
    Workspace        ProjectWorkspace  // agent_id + path
    HostAffinity     string            // optional
    DefaultLeadAgent string            // optional
    Git              ProjectGit        // repo_url, default_branch, current_sha
    Permissions      ProjectPermissions
    Quotas           ProjectQuotas
    Metadata         map[string]string
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

type ProjectWorkspace struct {
    AgentID string
    Path    string
}

type ProjectGit struct {
    RepoURL       string
    DefaultBranch string
    CurrentSHA    string
}

type ProjectPermissions struct {
    AgentRoles map[string]ProjectRole
}

type ProjectRole string // "lead" | "worker" | "observer" | "denied"

type ProjectQuotas struct {
    TokenBudget   int64 // 0 = unlimited
    GPUMinutes    int64 // 0 = unlimited
    MaxConcurrency int  // 0 = unlimited
}
```

## Permission Roles

Projects enforce per-agent role-based access control:

| Role | Claim Worker Tasks | Spawn Child Jobs | Read-Only | Block All |
|------|-------------------|------------------|-----------|-----------|
| `lead` | Yes | Yes | N/A | No |
| `worker` | Yes | No | N/A | No |
| `observer` | No | No | Yes | No |
| `denied` | No | No | No | Yes |

**Permission semantics:**

- `denied`: agent cannot claim any task under this project; blocks orchestration entry
- `observer`: read-only visibility into project state; cannot claim or spawn work
- `worker`: can claim queued tasks and work autonomously; cannot spawn child jobs
- `lead`: full orchestration rights; can spawn sub-jobs and delegate work to others

**Legacy behavior:** Projects with an empty permissions matrix (no AgentRoles entries) fall back to allow-all mode. This is transitional; new projects should define explicit roles.

## REST API

### Create Project

```
POST /api/v1/projects
Content-Type: application/json

{
  "tenant_id": "acme",
  "name": "backend-service",
  "description": "REST API backend",
  "workspace_agent_id": "agent-prod-01",
  "workspace_path": "/home/dev/projects/backend",
  "host_affinity": "gpu-host-02",
  "default_lead_agent": "claude-1",
  "git_repo_url": "https://github.com/acme/backend.git",
  "git_default_branch": "main",
  "agent_roles": {
    "claude-1": "lead",
    "ollama-dev": "worker",
    "codex-qa": "observer"
  },
  "token_budget": 10000000,
  "gpu_minutes": 1440,
  "max_concurrency": 2
}

201 Created
{
  "id": "proj-uuid",
  "tenant_id": "acme",
  "name": "backend-service",
  ...
}
```

### Get Project

```
GET /api/v1/projects/{id}

200 OK
{
  "id": "proj-uuid",
  "tenant_id": "acme",
  "name": "backend-service",
  ...
}
```

### List Projects

```
GET /api/v1/projects?tenant_id=acme

200 OK
{
  "projects": [
    {"id": "proj-uuid-1", "name": "backend-service", ...},
    {"id": "proj-uuid-2", "name": "frontend-spa", ...}
  ],
  "total": 2
}
```

### Update Project

```
PUT /api/v1/projects/{id}
Content-Type: application/json

{
  "description": "Updated description",
  "host_affinity": "gpu-host-03",
  "git_default_branch": "develop",
  "gpu_minutes": 2880
}

200 OK
{
  "id": "proj-uuid",
  ...
}
```

### Delete Project

```
DELETE /api/v1/projects/{id}

204 No Content
```

### Grant Agent Role

Grant or update an agent's role in a project:

```
PUT /api/v1/projects/{id}/permissions/{agent_id}
Content-Type: application/json

{
  "role": "lead"
}

200 OK
{
  "agent_id": "agent-id",
  "role": "lead",
  "granted_at": "2025-04-26T12:00:00Z"
}
```

### Revoke Agent Role

Remove an agent's access to a project:

```
DELETE /api/v1/projects/{id}/permissions/{agent_id}

204 No Content
```

## gRPC API

The `ProjectService` in `api/proto/project.proto` exposes all project operations over gRPC with identical semantics to REST. See `pkg/api/grpc/server_project.go` for service implementation.

Core RPCs:

- `CreateProject(CreateProjectRequest) → CreateProjectResponse`
- `GetProject(GetProjectRequest) → GetProjectResponse`
- `UpdateProject(UpdateProjectRequest) → UpdateProjectResponse`
- `DeleteProject(DeleteProjectRequest) → DeleteProjectResponse`
- `ListProjects(ListProjectsRequest) → ListProjectsResponse`
- `GrantAgentRole(GrantAgentRoleRequest) → GrantAgentRoleResponse`
- `RevokeAgentRole(RevokeAgentRoleRequest) → RevokeAgentRoleResponse`

## MCP Tools

Eight MCP tools manage projects programmatically:

| Tool | Purpose |
|------|---------|
| `project_create` | Create a new project |
| `project_get` | Retrieve a single project by ID |
| `project_list` | List all projects for a tenant |
| `project_update` | Update project fields |
| `project_delete` | Delete a project |
| `project_grant_agent` | Grant an agent a role in the project |
| `project_revoke_agent` | Revoke an agent's project access |
| `project_list_permissions` | List all agent roles for a project |

All tools validate the caller's authorization before proceeding. Implementation: `pkg/api/mcp/project_tools.go`.

## CLI Usage

```bash
# Create a project
quicbaas project create \
  --tenant acme \
  --name backend-service \
  --workspace-agent agent-prod-01 \
  --workspace-path /home/dev/projects/backend \
  --git-repo https://github.com/acme/backend.git \
  --git-branch main \
  --lead-agent claude-1 \
  --token-budget 10000000 \
  --gpu-minutes 1440

# List projects
quicbaas project list --tenant acme

# Get project details
quicbaas project get --id proj-uuid

# Update project
quicbaas project update --id proj-uuid \
  --host-affinity gpu-host-03

# Grant agent role
quicbaas project grant-role --id proj-uuid \
  --agent agent-id \
  --role lead

# Revoke agent role
quicbaas project revoke-role --id proj-uuid \
  --agent agent-id

# Delete project
quicbaas project delete --id proj-uuid
```

## Service Implementation

The `ProjectService` interface in `pkg/service/project_service.go` handles all project operations. It enforces:

- Tenant isolation (agents can only access projects in their tenant)
- Workspace validation (agent must be online and path accessible)
- Quota enforcement (token budget, GPU minutes, concurrency checks)
- Permission consistency (valid role values, no orphaned references)

All mutations are audited. See `docs/security/audit.md` for audit event types.

## Cross-References

- Workspace isolation: `docs/orchestration/workspace-isolation.md`
- Job spawning from projects: `docs/orchestration/sub-jobs.md`
- Permission matrix details: `docs/orchestration/security.md`
- Quota enforcement: `docs/orchestration/cost-controls.md`
- Audit trail: `docs/security/audit.md`

## Source Files

- `pkg/orchestration/project.go:26-131` — Project types
- `pkg/service/project_service.go` — ProjectService implementation
- `pkg/api/rest/handlers_project.go` — REST handlers
- `pkg/api/grpc/server_project.go` — gRPC service
- `pkg/api/mcp/project_tools.go` — MCP tool definitions

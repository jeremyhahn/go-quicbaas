# Agentic projects

> **Status: NORMATIVE** · Last verified: 2026-09-10
> Supplement to the Software Factory feature folder: the Project record, its
> data model, its permission matrix and its own REST/gRPC/MCP detail. Every
> verb and flag is in [cli.md](cli.md#quicbaas-project); the transport matrix
> is in [api.md](api.md#quicbaasproject--sdk-package-sdkgoclientproject);
> discovering and registering one is in
> [user.md](user.md#discover-and-register-agentic-projects).
> Conflicts with this document are defects.

Projects are first-class orchestration entities that group agentic work with dedicated workspace, permissions, and resource quotas. Each project belongs to a tenant and may span multiple agents and hosts.

## Concept

Projects sit in the hierarchy: Tenant → Project → Workflow → Job → Task.

A project encapsulates:

- **Workspace**: a filesystem location (agent + path) where agent operations occur
- **Host affinity**: preferred agent host for project work (optional)
- **Git metadata**: remote repository URL, default branch, current HEAD SHA
- **Lead agent**: default agent ID for spawning workflows (optional; job-level override allowed)
- **Permissions matrix**: per-agent role assignments (lead, worker, observer, denied)
- **Quotas**: LLM token budget, GPU minutes, max concurrent runs (0 = unlimited).
  Zero is a VALUE, not an absence, so an update carries the three quotas as
  presence-based fields: omit one to leave it alone, send `0` to lift the cap.
  They were plain scalars until go-quicbaas#796, and the update applied a
  quota only when it was non-zero — so an operator could set a cap to 0, watch
  the save succeed, and stay capped.

## Data Model

All projects and their fields are defined in `pkg/orchestration/project.go`:

```go
type Project struct {
    ID               string
    TenantID         string
    Name             string
    Description      string
    Workspace        ProjectWorkspace  // agent_id + path
    Storage          ProjectStorage    // storage tier of the sandboxed checkout; zero = host bind mount
    HostAffinity     string            // optional
    DefaultLeadAgent string            // optional
    DefaultWorkflow  string            // tenant-scoped workflow a Task auto-spawns into; empty = never
    Autonomous       bool              // the poller adopts and the dispatcher starts work for this project
    ContextRepos     []ContextRepo     // repositories the WORKER clones beside the project checkout
    Git              ProjectGit        // repo_url, default_branch, current_sha, author, signing, ssh_key
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

// See docs/software-factory/storage-tiers.md. Every field is inert at its zero
// value: an unconfigured project keeps the host bind mount it always had.
// NOT YET carried by CreateProject / UpdateProject on any transport (#913).
type ProjectStorage struct {
    WorkspaceTier     WorkspaceTier // "" | "persistent" | "ephemeral"
    WorkspaceTmpfsMB  int           // sizes the ephemeral tier's tmpfs checkout
    ContainerMemoryMB int           // the container's memory cgroup limit
}

type ProjectGit struct {
    RepoURL       string
    DefaultBranch string
    CurrentSHA    string
    Author        GitAuthor     // identity recorded on commits made from the workspace
    Signing       CommitSigning // zero value signs nothing, explicitly
    SSHKey        KMSKeyRef     // access key for an ssh:// RepoURL; empty = the signing key
}

type GitAuthor struct {
    Name  string
    Email string
}

// CommitSigning is a policy, not a key path: the algorithm is an owner ruling
// (ED25519 default, ECDSA-P256 under FIPS) and the key lives in the KMS. An
// enabled policy must name a key; it never degrades to unsigned.
type CommitSigning struct {
    Enabled   bool
    Algorithm string    // "ED25519" | "ECDSA-P256"
    Key       KMSKeyRef
}

type KMSKeyRef struct {
    Name  string
    Scope string // "user" | "tenant" | "service"
    Owner string
}
```

`ProjectGit.AccessKey()` returns `SSHKey` when set and the signing key
otherwise — one key per workspace, registered on the remote as both signing and
access key, is the common shape. `GitProvisioner.CommitForProject` records a
commit under the project's `Author` and `Signing`; the request's own values are
overwritten from the project so a caller cannot substitute an identity.

`Autonomous` is the single switch for autonomous development: it gates the
task-source poller and the task dispatcher, bounded by
`Quotas.MaxConcurrency`. `ContextRepos` names the repositories the worker's
container clones **beside** the project checkout — the daemon clones nothing.
Both are set on create (`autonomous`, `context_repos`) and on update
(`autonomous` as a pointer so "unchanged" and "off" are distinct;
`context_repos` replaces the whole list, `clear_context_repos` empties it),
with `--autonomous`, `--context-repo name=…,url=…,ref=…` and
`--clear-context-repos` on the CLI. Both are documented in full in
[autonomy-and-dispatch.md](autonomy-and-dispatch.md).

`DefaultWorkflow` is validated on create and update against the tenant's
workflow registry: a set-but-unknown name is refused (`ProjectInvalidError`,
field `default_workflow`). Workflows are tenant-scoped — stored at
`_orchestration/workflows/<tenant>/<name>` — so two tenants may register the
same workflow name without colliding, and a project can only name a workflow
its own tenant has.

```go

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

**Where the matrix is enforced.** Two writes hand an agent a project's work, and
both consult the matrix through one helper, so they cannot drift apart:

- `ClaimTask` — the agent asking for a task. Requires at least `worker`.
- The dispatch path (`SpawnTask` for the dispatcher's sweep, `autoSpawnTask` at
  creation time). A dispatch hands a fleet agent the work with no claim
  afterwards, so it narrows the online fleet to the agents the project admits as
  workers before the routing matcher picks one. Narrowing rather than vetoing
  the pick means a project whose own worker is online still dispatches; when the
  project admits none of the online agents the refusal names the `project_role`
  constraint rather than blaming routing.

**A project the store cannot read is a refusal**, not a pass:
`ProjectUnresolvedError` (sentinel `ErrProjectUnresolved`, HTTP 403). A check
that could not read the rule must never answer "allowed" — and because project
records live in their owning tenant's routed namespace, a lookup made under the
wrong tenant looks exactly like a project that never existed.

**Verbs the matrix does not gate.** It is keyed on an agent ID, so it only
applies where a write names one. `DeleteTask`, `BlockTask`, `ReadmitTask` and
`CompleteTask` carry no agent principal and are gated by the tenant-level RBAC
PDP alone (`orchestrator:task_*`).

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
  "default_workflow": "software-dev",
  "git_author_name": "agent-backend",
  "git_author_email": "agent-backend@acme.internal",
  "git_signing_enabled": true,
  "git_signing_algorithm": "ED25519",
  "git_signing_key_name": "backend-signing",
  "git_signing_key_scope": "tenant",
  "git_signing_key_owner": "acme",
  "git_ssh_key_name": "backend-access",
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
  "project": {
    "id": "proj-uuid",
    "tenant_id": "acme",
    "name": "backend-service",
    "description": "REST API backend",
    "workspace_agent_id": "agent-prod-01",
    "workspace_path": "/home/dev/projects/backend",
    "host_affinity": "gpu-host-02",
    "default_lead_agent": "claude-1",
    "default_workflow": "software-dev",
    "autonomous": true,
    "context_repos": [{"name": "handbook", "repo_url": "…", "ref": "main"}],
    "git_repo_url": "ssh://git@forge.example:2222/acme/backend.git",
    "git_default_branch": "main",
    "git_current_sha": "…",
    "git_author_name": "agent-backend",
    "git_author_email": "agent-backend@acme.internal",
    "git_signing_enabled": true,
    "git_signing_algorithm": "ED25519",
    "git_signing_key_name": "backend-signing",
    "git_signing_key_scope": "tenant",
    "git_signing_key_owner": "acme",
    "git_ssh_key_name": "backend-access",
    "git_ssh_key_scope": "",
    "git_ssh_key_owner": "",
    "agent_roles": {"claude-1": "lead"},
    "token_budget": 10000000,
    "gpu_minutes": 1440,
    "max_concurrency": 2,
    "created_at": "2026-09-01T00:00:00Z",
    "updated_at": "2026-09-02T00:00:00Z"
  },
  "timestamp": "2026-09-02T00:00:00Z"
}
```

#### The project shape is FLAT, on read and on write

Every git, workspace, quota and permission field is a top-level key —
`git_repo_url`, `workspace_path`, `token_budget`, `agent_roles` — on the
response as well as on the create and update bodies. There is no nested
`git`, `workspace`, `quotas` or `permissions` member and there never has
been: `pkg/api/rest.ProjectRESTResponse` is what every transport serializes,
and it mirrors `orchestration.Project` field by field with flat JSON tags.

This is stated because a consumer got it wrong in a way nothing caught. The
TypeScript SDK's `Project` type declared a nested mirror, so the desktop and
web project edit dialog hydrated from `project.git.repo_url` — a member the
API has never sent — and every existing project's Git settings opened blank,
while the write path correctly sent `git_repo_url` (go-quicbaas#797). The SDK
type is now the wire shape, and `cmd/quicbaas/frontend/test/projectForm.test.ts`
hydrates from a verbatim `GET /api/v1/projects/{id}` body so a re-nesting
fails a test rather than an operator's next edit.

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
  "gpu_minutes": 2880,
  "git_signing_enabled": false,
  "clear_default_workflow": true
}
```

Update semantics for the new fields: string fields apply when non-empty;
`git_signing_enabled` is tri-state (absent = unchanged, `false` = off,
`true` = on); `clear_default_workflow` removes the default workflow. The
resulting git metadata must still validate — an update that enables signing
without a key, or names an unruled algorithm, is refused and nothing is stored.

```

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

`project_create` and `project_update` take the same flat `default_workflow` and
`git_author_*` / `git_signing_*` / `git_ssh_key_*` arguments as the REST body;
`project_update` additionally takes `clear_default_workflow`, and its
`git_signing_enabled` is tri-state (omit it to leave signing unchanged).

All tools validate the caller's authorization before proceeding. Implementation: `pkg/api/mcp/project_tools.go`.

## CLI Usage

Every verb and every flag is in [cli.md](cli.md#quicbaas-project); these are
the shapes an operator uses most.

```bash
# Create a project
quicbaas project create \
  --tenant acme \
  --name backend-service \
  --workspace-agent agent-prod-01 \
  --workspace-path /home/dev/projects/backend \
  --git-repo https://github.com/acme/backend.git \
  --git-branch main \
  --default-lead-agent claude-1 \
  --default-workflow software-dev \
  --git-author-name agent-backend \
  --git-author-email agent-backend@acme.internal \
  --git-signing --git-signing-key backend-signing \
  --git-signing-key-scope tenant --git-signing-key-owner acme \
  --git-ssh-key backend-access \
  --token-budget 10000000 \
  --gpu-minutes 1440

# List projects
quicbaas project list --tenant acme

# Get project details
quicbaas project get proj-uuid

# Update project
quicbaas project update proj-uuid \
  --host-affinity gpu-host-03

# Turn commit signing off and drop the default workflow. --git-signing is
# tri-state: not passing it leaves signing unchanged.
quicbaas project update proj-uuid \
  --git-signing=false --clear-default-workflow

# Grant agent role
quicbaas project grant-role proj-uuid \
  --agent agent-id \
  --role lead

# Revoke agent role
quicbaas project revoke-role proj-uuid \
  --agent agent-id

# Delete project
quicbaas project delete proj-uuid
```

## Service Implementation

The `ProjectService` interface in `pkg/service/project_service.go` handles all project operations. It enforces:

- Tenant isolation (agents can only access projects in their tenant)
- Workspace validation (agent must be online and path accessible)
- Quota enforcement (token budget, GPU minutes, concurrency checks)
- Permission consistency (valid role values, no orphaned references)

All mutations are audited. See [../audit-logging/architecture.md](../audit-logging/architecture.md)
for audit event types.

*Correction (2026-09-10): the five cross-references below pointed at
`docs/orchestration/*.md` and `docs/security/audit.md`, neither of which has
existed since the 2026-08-19 documentation migration. They are repointed at
the sections that absorbed them.*

## Cross-References

- Workspace isolation: [architecture.md](architecture.md#workspace-isolation)
- Job spawning from projects: [engineering.md](engineering.md#sub-job-spawning--typed-artifacts)
- Permission matrix details: [architecture.md](architecture.md#agent-orchestration-security)
- Quota enforcement: [engineering.md](engineering.md#cost-controls)
- Autonomy and dispatch: [autonomy-and-dispatch.md](autonomy-and-dispatch.md)
- Binding a project to a forge: [sdlc.md](sdlc.md)
- Audit trail: [../audit-logging/architecture.md](../audit-logging/architecture.md)

## Source Files

- `pkg/orchestration/project.go:26-131` — Project types
- `pkg/service/project_service.go` — ProjectService implementation
- `pkg/api/rest/handlers_project.go` — REST handlers
- `pkg/api/grpc/server_project.go` — gRPC service
- `pkg/api/mcp/project_tools.go` — MCP tool definitions

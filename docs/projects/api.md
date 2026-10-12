# Projects — API

> **Status: NORMATIVE** · Last verified: 2026-10-11
> The Project capability on every transport, with the permission each
> operation requires. Field semantics are in [engineering.md](engineering.md);
> design is in [architecture.md](architecture.md). Conflicts with this
> document are defects.

## Operations and permissions

One permission per operation, identical on every transport. REST takes it from
`pkg/api/rest/routes_permissions_generated.go`, gRPC from the
`(qrdb.v1.permission)` option in `api/proto/project.proto`, MCP from
`pkg/api/mcp/authz_qrdb.go`.

| Operation | Permission | REST | gRPC (`ProjectService`) | MCP tool |
| --- | --- | --- | --- | --- |
| Create | `project:create` | `POST /api/v1/projects/` | `CreateProject` | `project_create` |
| Get | `project:read` | `GET /api/v1/projects/{id}` | `GetProject` | `project_get` |
| Get by name | `project:read` | — (see [Known gaps](engineering.md#known-gaps)) | `GetProjectByName` | — |
| List | `project:list` | `GET /api/v1/projects/` | `ListProjects` | `project_list` |
| Update (incl. park, resume) | `project:update` | `PUT /api/v1/projects/{id}` | `UpdateProject` | `project_update` |
| Delete | `project:delete` | `DELETE /api/v1/projects/{id}` | `DeleteProject` | `project_delete` |
| Grant role | `project:grant` | `PUT /api/v1/projects/{id}/permissions/{node_id}` | `GrantFleetNodeRole` | `project_grant_fleet_node` |
| Revoke role | `project:revoke` | `DELETE /api/v1/projects/{id}/permissions/{node_id}` | `RevokeFleetNodeRole` | `project_revoke_fleet_node` |

Transports: REST is `pkg/api/rest/handlers_project.go` (routes in
`routes.go`); QUIC serves the same REST surface over HTTP/3; gRPC is
`pkg/api/grpc/server_project.go`; Unix is the gRPC wire over a domain socket;
MCP is `pkg/api/mcp/project_tools.go` and `fleet_admin_tools.go`; the
embedded transport registers all eight SDK methods in-process. The SDK client
is `sdk/go/client/project` (`quicbaas.Project`).

## REST

Base URL in examples: `https://quicbaas.example.com`. Every route except
`POST` requires the `tenant_id` query parameter; `POST` carries it in the body.

### The wire shape is flat

Every git, workspace, quota, storage and permission field is a top-level key —
`git_repo_url`, `workspace_path`, `gpu_minutes`, `agent_roles` — on reads and
writes alike. There is no nested `git`, `workspace`, `quotas` or `permissions`
member. `ProjectRESTResponse` is the read shape on every transport.

| Group | Keys |
| --- | --- |
| Identity | `id` (read), `tenant_id`, `name`, `description`, `metadata`, `created_at`, `updated_at` (read) |
| Workspace | `workspace_agent_id` (a fleet node id), `workspace_path`, `host_affinity` |
| Work | `default_lead_agent` (`{"agent_id": …}` or `{"pool_id": …}`), `default_workflow`, `autonomous`, `context_repos` (`[{name, repo_url, ref}]`) |
| Parking | `parked`, `parked_at` (read), `parked_by` (read) |
| Git | `git_repo_url`, `git_default_branch`, `git_current_sha`, `git_author_name`, `git_author_email` |
| Signing | `git_signing_enabled`, `git_signing_algorithm`, `git_signing_key_{name,scope,owner}`, `git_ssh_key_{name,scope,owner}`, `git_signing_retired` (read) |
| Limits | `agent_roles` (`{node_id: role}`), `spend` (`{"windows": [...]}`), `gpu_minutes`, `max_concurrency` |
| Storage | `workspace_tier`, `workspace_tmpfs_mb`, `container_memory_mb` |
| Sessions | `session_rotation` |

### Create

```http
POST /api/v1/projects/
Content-Type: application/json

{
  "tenant_id": "acme",
  "name": "backend-service",
  "git_repo_url": "https://git.quicbaas.example.com/acme/backend.git",
  "git_default_branch": "main",
  "git_author_name": "agent-backend",
  "git_author_email": "agent-backend@quicbaas.example.com",
  "git_signing_enabled": true,
  "git_signing_key_name": "backend-signing",
  "git_signing_key_scope": "tenant",
  "git_signing_key_owner": "acme",
  "default_lead_agent": {"pool_id": "leads"},
  "agent_roles": {"node-lead-01": "lead", "node-build-02": "worker"},
  "spend": {"windows": [{"name": "weekly", "size": 604800000000000, "max_equivalent_micros": 250000000}]},
  "max_concurrency": 2,
  "session_rotation": "fresh"
}
```

`201 Created`:

```json
{
  "project": {
    "id": "3f2b6c1e-8d4a-4b7e-9a10-2c5d7e8f9a01",
    "tenant_id": "acme",
    "name": "backend-service",
    "...": "…every stored field, flat…",
    "created_at": "…",
    "updated_at": "…"
  },
  "timestamp": "…"
}
```

### Get

`GET /api/v1/projects/{id}?tenant_id=acme` → `200` with `{"project": {…}, "timestamp"}`.

### List

`GET /api/v1/projects/?tenant_id=acme` → `200` with `{projects, total, limit,
offset, next_offset, facets, timestamp}`. Query parameters: the shared paging
parameters (`limit` default 25, capped at 100; `offset`; `sort`; `q`) and
`autonomous=true|false` to filter. `name` is not a filter.

### Update

`PUT /api/v1/projects/{id}?tenant_id=acme` with any subset of the create keys,
plus:

| Key | Meaning |
| --- | --- |
| `clear_description`, `clear_default_workflow`, `clear_context_repos` | empty the field |
| `parked` | `true` parks, `false` resumes, absent leaves it |
| `git_signing_retire_key_{name,scope,owner}`, `git_signing_retired_at` | add a key to the trusted signing history; owner defaults to the tenant, time to now |

Presence decides: absent leaves a field alone. `autonomous`, `parked` and
`git_signing_enabled` are tri-state; `gpu_minutes`, `max_concurrency`,
`workspace_tmpfs_mb` and `container_memory_mb` treat `0` as a value; `spend`
absent leaves the limits and `{}` clears them; `context_repos` replaces the
whole list. Response: `200` with `{"project": {…}, "timestamp"}`.

```http
PUT /api/v1/projects/3f2b6c1e-8d4a-4b7e-9a10-2c5d7e8f9a01?tenant_id=acme
Content-Type: application/json

{"parked": true}
```

### Delete

`DELETE /api/v1/projects/{id}?tenant_id=acme` → `200` with
`{project_id, deleted, timestamp}`.

### Grant and revoke

```http
PUT /api/v1/projects/{id}/permissions/{node_id}?tenant_id=acme
Content-Type: application/json

{"role": "worker"}
```

`200` with `{project_id, node_id, role, timestamp}`. Revoke is
`DELETE /api/v1/projects/{id}/permissions/{node_id}?tenant_id=acme` → `200`
with `{project_id, node_id, timestamp}`; it stores `denied`.

## gRPC and Unix

`api/proto/project.proto`, service `ProjectService`: `CreateProject`,
`GetProject`, `GetProjectByName`, `UpdateProject`, `DeleteProject`,
`ListProjects`, `GrantFleetNodeRole`, `RevokeFleetNodeRole`. Fields mirror the
flat REST keys. `tenant_id` is required on every request; `project_id`,
`name`, `node_id` and `role` where the operation needs them
(`InvalidArgument` otherwise). The authorization map is
`pkg/api/grpc/authz_mapping.go`.

## MCP

| Tool | Required arguments | Notes |
| --- | --- | --- |
| `project_create` | `tenant_id`, `name` | the flat create keys except `agent_roles` and `metadata`; grant roles with `project_grant_fleet_node` |
| `project_get` | `tenant_id`, `project_id` | |
| `project_list` | `tenant_id` | `limit` (default 25, max 100), `offset`, `sort`, `q` (matches id and name); newest first |
| `project_update` | `tenant_id`, `project_id` | the flat update keys, including `parked` and the retirement keys |
| `project_delete` | `tenant_id`, `project_id` | |
| `project_grant_fleet_node` | `tenant_id`, `project_id`, `node_id`, `role` | `role` ∈ `lead`, `worker`, `observer`, `denied` |
| `project_revoke_fleet_node` | `tenant_id`, `project_id`, `node_id` | sets `denied` |

The update handler reads more keys than its input schema advertises (for
example `gpu_minutes`, `max_concurrency`, `spend`, `host_affinity`,
`agent_roles`); a client driven by the schema alone will not offer them.
There is no get-by-name tool, and the SDK's MCP transport answers
`GetByName` with `Unimplemented`.

## Errors

| Condition | REST | gRPC / Unix |
| --- | --- | --- |
| Missing `tenant_id` / id / name / role | `400` | `InvalidArgument` |
| Bad paging input | `400` | `InvalidArgument` |
| `ProjectNotFoundError` | `404` | `NotFound` |
| `ProjectAlreadyExistsError` (name taken) | `409` | `AlreadyExists` |
| `ProjectInvalidError` | `400` | `InvalidArgument` |
| `ProjectAccessDeniedError` | `403` | `Internal` (gap) |
| `ProjectUnresolvedError` | `403` | `Internal` (gap) |
| `ProjectRoleInvalidError` | `400` | `Internal` (gap) |
| Reference field names nothing / lookup unavailable | per [reference checks](../software-factory/api.md#reference-fields-are-checked-on-save) | same |
| Anything else | `500` | `Internal` |

REST mapping: `handleProjectError` (`pkg/api/rest/handlers_project.go`);
gRPC: `convertProjectError` (`pkg/api/grpc/server_project.go`).

## Related surfaces

- Binding a project to a forge, onboarding and the SDLC routes:
  [SDLC API](../sdlc/api.md).
- Task, job and dispatch APIs that take a `project_id`:
  [orchestration](../orchestration/).
- Fleet node enable/disable (`quicbaas_fleet_*`): [agent fleet](../agent-fleet/api.md#mcp).
- Pooled provider accounts with a project default:
  [accounts](../accounts/architecture.md).

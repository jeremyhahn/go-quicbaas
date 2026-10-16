# Software Factory — API

> **Status: NORMATIVE** · Last verified: 2026-10-15
> The Software Factory's transport matrix, reference checks on save and the
> agent catalog. Projects, pooled provider accounts and the SDLC module are in
> [../projects/api.md](../projects/api.md),
> [../accounts/api.md](../accounts/api.md) and [../sdlc/api.md](../sdlc/api.md);
> the orchestration engine's API is in
> [../orchestration/api.md](../orchestration/api.md). Feature folder:
> `docs/software-factory/`. Conflicts with this document are defects.

Prompt sets are served on every transport; see [Prompt sets](#prompt-sets).

The orchestration engine's own surface (jobs, sessions, backends, the
list-paging contract, the MCP tool reference) is in
[../orchestration/api.md](../orchestration/api.md); the fleet's is in
[../agent-fleet/api.md](../agent-fleet/api.md).

## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.

### `quicbaas.Project` — SDK package `sdk/go/client/project`

Every operation, route, permission and error mapping is in
[projects/api.md](../projects/api.md).

### `quicbaas.Account` — SDK package `sdk/go/client/account`

`List` is paged, sorted and searched per
[List paging, sorting and search](../orchestration/api.md#list-paging-sorting-and-search).

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 9; MCP tools registered server-side: 9.

MCP tools: `quicbaas_agent_account_create`, `quicbaas_agent_account_delete`, `quicbaas_agent_account_get`, `quicbaas_agent_account_list`, `quicbaas_agent_account_revoke`, `quicbaas_agent_account_rotate`, `quicbaas_agent_account_set_default`, `quicbaas_agent_account_update`, `quicbaas_agent_account_usage`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Delete` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `List` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Usage` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |

## Reference fields are checked on save

A field that names another object the platform controls is checked for
existence by the service layer when it is saved, so every transport refuses a
dangling reference the same way. Stored data is not re-checked on read, and an
update checks only the references it changes.

| Save | Field | Must name |
|---|---|---|
| SDLC bind | `forge.token_secret_ref`, `forge.webhook_secret_ref` | a secret in the tenant |
| SDLC bind | `reviewer.token_secret_ref`, `reviewers[i].token_secret_ref` | a secret in the tenant |
| Project create / update | `git_signing_key_{name,scope,owner}`, `git_ssh_key_{name,scope,owner}` | an SSH key in the KMS; scope defaults to `user`, owner to the tenant |
| Project create / update | `default_workflow` | a workflow registered in the tenant |
| Project create / update | `host_affinity` | a fleet node in the tenant, by agent ID, name or host |
| Project create | `git_credential_secret_ref` | a secret in the tenant |
| Job start | `workflow_name` | a workflow registered in the tenant (a resumed job is not re-checked) |
| Agent register | `tenant_id` | a tenant IAM knows; the system tenant always exists. Nodes without IAM skip this check |

A check fails in one of two ways, and they are never confused:

| Error | Meaning | REST | gRPC | Code |
|---|---|---|---|---|
| `ReferenceNotFoundError` | the store answered and the object is not there | 400 | `InvalidArgument` | `reference_not_found` |
| `ReferenceUnavailableError` | the store could not answer (sealed barrier, no store attached, I/O failure) | 503, retryable | `Unavailable` | `reference_unavailable` |

The message names the field, the kind and the value, e.g.
`forge.token_secret_ref: secret "sdlc/forge/api/token" does not exist`. MCP
returns the same message as the tool error.

### `quicbaas.Agents` — SDK package `sdk/go/client/agents`

The tenant agent catalog: Agents and pools. See [agents.md](agents.md).

| SDK method | REST route | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|
| `ListAgents` | `GET /api/v1/agents` | `ListAgents` | `quicbaas_agents_list` | yes | `agents:read` |
| `GetAgent` | `GET /api/v1/agents/{id}` | `GetAgent` | `quicbaas_agents_get` | yes | `agents:read` |
| `CreateAgent` | `POST /api/v1/agents` | `CreateAgent` | `quicbaas_agents_create` | yes | `agents:write` |
| `UpdateAgent` | `PUT /api/v1/agents/{id}` | `UpdateAgent` | `quicbaas_agents_update` | yes | `agents:write` |
| `DeleteAgent` | `DELETE /api/v1/agents/{id}` | `DeleteAgent` | `quicbaas_agents_delete` | yes | `agents:write` |
| `AgentUsedBy` | `GET /api/v1/agents/{id}/used-by` | `AgentUsedBy` | `quicbaas_agents_used_by` | yes | `agents:read` |
| `ListPools` | `GET /api/v1/agent-pools` | `ListAgentPools` | `quicbaas_agent_pools_list` | yes | `agents:read` |
| `GetPool` | `GET /api/v1/agent-pools/{id}` | `GetAgentPool` | `quicbaas_agent_pools_get` | yes | `agents:read` |
| `CreatePool` | `POST /api/v1/agent-pools` | `CreateAgentPool` | `quicbaas_agent_pools_create` | yes | `agents:write` |
| `UpdatePool` | `PUT /api/v1/agent-pools/{id}` | `UpdateAgentPool` | `quicbaas_agent_pools_update` | yes | `agents:write` |
| `DeletePool` | `DELETE /api/v1/agent-pools/{id}` | `DeleteAgentPool` | `quicbaas_agent_pools_delete` | yes | `agents:write` |
| `PoolUsedBy` | `GET /api/v1/agent-pools/{id}/used-by` | `AgentPoolUsedBy` | `quicbaas_agent_pools_used_by` | yes | `agents:read` |

Refusals map the same way on every transport:

| refusal | REST | gRPC |
|---|---|---|
| invalid request or role mismatch | 400 | InvalidArgument |
| not found | 404 | NotFound |
| already exists | 409 | AlreadyExists |
| in use | 409 | FailedPrecondition |
| catalog unavailable | 503 | Unavailable |
| pool exhausted | 503 | ResourceExhausted |

## Prompt sets

A tenant's overrides of the embedded prompts; see [prompt-sets.md](prompt-sets.md).
Each verb addresses a prompt with `tenant_id` (defaults to the caller's own;
naming another is `403` / PermissionDenied), `scope` (`tenant`, `project` or
`system`; `system` is read-only) and `project_id`. On REST they are query
parameters. gRPC is `quicbaas.prompt.v1.PromptService`, which Unix serves too.
The Go SDK is `Client.Prompts()` (`sdk/go/client/prompt`, service
`quicbaas.Prompts`); QUIC sends its REST routes over HTTP/3 and the embedded
transport calls `service.PromptService` in process. The CLI is
`quicbaas prompts`; its flags are in [cli.md](cli.md#quicbaas-prompts).

| Verb | REST route | gRPC | MCP | SDK | CLI | Permission |
|---|---|---|---|---|---|---|
| List | `GET /api/v1/prompts/{set}` | `ListPrompts` | `prompt_list` | `List` | `list <set>` | `prompts:read` |
| Get (`?version=N`) | `GET /api/v1/prompts/{set}/{name}` | `GetPrompt` | `prompt_get` | `Get` | `get <set> <name>` | `prompts:read` |
| Effective | `GET /api/v1/prompts/{set}/{name}/effective` | `GetEffectivePrompt` | `prompt_effective` | `Effective` | `effective <set> <name>` | `prompts:read` |
| Diff | `GET /api/v1/prompts/{set}/{name}/diff` | `DiffPrompt` | `prompt_diff` | `Diff` | `diff <set> <name>` | `prompts:read` |
| History | `GET /api/v1/prompts/{set}/{name}/history` | `GetPromptHistory` | `prompt_history` | `History` | `history <set> <name>` | `prompts:history` |
| Put | `PUT /api/v1/prompts/{set}/{name}` | `PutPrompt` | `prompt_put` | `Put` | `put <set> <name> -f FILE` | `prompts:write` |
| Rollback | `POST /api/v1/prompts/{set}/{name}/rollback` | `RollbackPrompt` | `prompt_rollback` | `Rollback` | `rollback <set> <name> --version N` | `prompts:write` |
| Reset | `DELETE /api/v1/prompts/{set}/{name}` | `ResetPrompt` | `prompt_reset` | `Reset` | `reset <set> <name>` | `prompts:write` |

Put takes `content`, `note` (required) and `expected_version` (0 when
nothing is overridden at that scope). Rollback takes `to_version`, `note` and
`expected_version`. Both are JSON bodies on REST. Reset takes `note` and
`expected_version`, which are query parameters on REST. Get answers not-found
when nothing is overridden at that scope. Effective always answers, with
the resolution chain and the `origin` layer. History lists identifiers,
hashes, authors and notes. Its bodies are read with Get and a version.

| refusal | REST | gRPC | MCP |
|---|---|---|---|
| missing marker (the body names it), unknown set or name, write at `system` | 400 | InvalidArgument | invalid input |
| not found | 404 | NotFound | resource not found |
| `expected_version` stale | 409 | Aborted | resource conflict |
| no prompt store mounted, or the store unreachable | 503 | Unavailable | service unavailable |

The SDK keeps each class: `errors.IsInvalidInput`, `IsNotFound`, `IsConflict`
and `IsUnavailable` answer on every transport.

## The SDLC module

The SDLC module's REST, gRPC, MCP and SDK surface, and the attention
inbox, are in [../sdlc/api.md](../sdlc/api.md).

## Agent Tools and MCP reference

The `quicbaas_orchestrator_*` agent tools, the RAG and cache tools, the MCP
naming convention and the observability metrics are in
[../orchestration/api.md](../orchestration/api.md).

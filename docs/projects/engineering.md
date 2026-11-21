# Projects — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Exported surface, invariants, errors, configuration and tests of the Project
> record. Design rationale is in [architecture.md](architecture.md). Conflicts
> with this document are defects.

## Exported surface

### `pkg/orchestration`

| Symbol | File | Purpose |
| --- | --- | --- |
| `Project` | `project.go` | the record (fields below) |
| `ProjectWorkspace{FleetNodeID, Path}` | `project.go` | an operator-named workspace |
| `ProjectProvisioning{Managed, State, Error, UpdatedAt}` | `project.go` | managed-workspace progress |
| `ProvisioningStatePending`, `…Cloning`, `…Ready`, `…Failed` | `project.go` | provisioning states |
| `ProjectPermissions{FleetNodeRoles}` | `project.go` | the matrix; JSON `agent_roles` |
| `ProjectRole`: `ProjectRoleLead`, `…Worker`, `…Observer`, `…Denied` | `project.go` | roles |
| `ProjectQuotas{GPUMinutes, MaxConcurrency}` | `project.go` | `0` is unlimited |
| `ContextRepo{Name, RepoURL, Ref}`, `ValidateContextRepos` | `project.go` | repositories the worker clones beside the checkout |
| `ProjectGit{RepoURL, DefaultBranch, CurrentSHA, Author, Signing, SSHKey}`, `AccessKey()` | `project.go` | git identity |
| `GitAuthor`, `CommitSigning{Enabled, Algorithm, Key, Retired}`, `RetiredSigningKey`, `KMSKeyRef{Name, Scope, Owner}` | `project_git.go` | commit identity and signing policy |
| `SigningAlgorithmEd25519`, `SigningAlgorithmECDSAP256` | `project_git.go` | the two accepted algorithms; empty means ED25519 |
| `ValidateProjectID(field, id)` | `project_id.go` | refuses a set, non-UUID id |
| `ProjectStore`, `NewProjectStore`, `NewInMemoryProjectStore` | `project_store.go` | persistence and the name index |
| `ProjectAuthz`, `NewProjectAuthz`, `AuthorizeFleetNode`, `AuthorizeFleetNodeForTenant`, `RoleAtLeast` | `project_authz.go` | the matrix check |
| `StartedWorkRunner` | `project_park.go` | a runner that finishes started work and is admitted while parked |

`Project` fields: `ID`, `TenantID`, `Name`, `Description`, `Workspace`,
`Storage` (`ProjectStorage`: `WorkspaceTier`, `WorkspaceTmpfsMB`,
`ContainerMemoryMB`), `HostAffinity`, `DefaultLeadAgent` (`AgentRef`: an agent
or a pool), `DefaultWorkflow`, `Autonomous`, `Parked`, `ParkedAt`, `ParkedBy`,
`Git`, `ContextRepos`, `Provisioning`, `Permissions`, `Quotas`, `Spend`
(`SpendLimits`), `SessionRotation`, `Metadata`, `CreatedAt`, `UpdatedAt`. `Spend` is enforced
per project with the per-tenant cap deferred to the account ledger (go-quicbaas#1058);
see [../accounts/architecture.md](../accounts/architecture.md#projects-and-tenants-deferred-at-admission).

### `pkg/service`

`ProjectService` (`project_service.go`): `CreateProject`, `GetProject`,
`GetProjectByName`, `UpdateProject`, `DeleteProject`, `ListProjects`,
`AuthorizeFleetNode`, `GrantFleetNodeRole`, `RevokeFleetNodeRole`.

Dependencies are injected after construction: `SetWorkspaceProvisioner`,
`SetWorkflowResolver`, `SetSecrets`, `SetSSHKeys`, `SetFleetNodes`,
`SetAgentRefChecker`. A managed create on a service with no provisioner is
refused (`ProjectProvisioningUnavailableError`, `project_provisioner.go`),
never silently downgraded to an unmanaged project.

`CreateProjectRequest` carries every create field, including two that no
transport sets: `WorkspaceManaged` and `GitCredentialSecretRef` (plus
`GitCredentialUsername`). `UpdateProjectRequest` uses pointers for
`GPUMinutes`, `MaxConcurrency`, `Spend`, `Autonomous`, `Parked`,
`GitSigningEnabled`, `WorkspaceTmpfsMB` and `ContainerMemoryMB`; a
`GitSigningRetire *GitSigningRetirement` for rotation; and the
`ClearDescription`, `ClearDefaultWorkflow` and `ClearContextRepos` flags.

Managed workspaces: `ProjectWorkspaceProvisioner` (`project_provisioner.go`),
`GitProvisioner` (`project_git.go`: `Clone`, `Branch`, `Commit`, `Push`,
`CommitForProject`), `RequireSignedCommit` (`project_git_read.go`). The server
wires them in `pkg/server/lifecycle_project_workspaces.go`
(`wireProjectWorkspaces`).

### SDK and CLI

`sdk/go/client/project` is the `quicbaas.Project` client: `Create`, `Get`,
`GetByName`, `Update`, `Delete`, `List`, `GrantFleetNodeRole`,
`RevokeFleetNodeRole`. Per-transport handlers live in
`sdk/go/transport/{rest,grpc,mcp}/project_handler.go`; the QUIC transport
delegates to the REST transport (`sdk/go/transport/quic/transport.go`) and Unix
is gRPC over a domain socket. The CLI is `cmd/quicbaas/project.go`
([cli.md](cli.md)); the desktop and web form is
`cmd/quicbaas/frontend/src/lib/projectForm.ts` and `projectPark.ts`.

## Invariants

1. **The id is a UUID minted by the service** (`uuid.New()`), immutable. A
   caller never chooses it.
2. **The name is unique per tenant**, decided by `PutIfAbsent` on the name
   index; the record is written before its reservation, and delete removes the
   record before releasing the reservation with a guarded `CompareAndDelete`.
3. **Every operation is tenant-scoped.** A wrong-tenant lookup is "not found".
4. **Validation precedes every write.** A refused create or update stores
   nothing.
5. **Updates are presence-based.** Omitted fields are unchanged; pointer fields
   distinguish zero from absent; `clear_*` flags empty a field.
6. **An enabled signing policy names a key**; the algorithm is ED25519 or
   ECDSA-P256. A retired key is read from the KMS, may not be the current key,
   may not be retired in the future, and retiring it twice keeps the first
   retirement.
7. **`agent_roles` empty ⇒ every node is `lead`; non-empty ⇒ unlisted nodes
   are `denied`.** An unreadable project is `ProjectUnresolvedError`, never a
   pass.
8. **Parking gates intake only** (`admitIntake`). `Parked` and `Autonomous` are
   independent.
9. **`workspace_path` and managed are mutually exclusive**, as are a credential
   secret and an SSH key; credentials need a repository URL; a credential
   secret needs managed.
10. **The wire shape is flat.** `ProjectRESTResponse` is what every transport
    serializes; there is no nested `git`, `workspace`, `quotas` or
    `permissions` member.

## Validation rules

| Field | Rule | Refusal |
| --- | --- | --- |
| `name` | required on create; unique in the tenant | `ProjectInvalidError` / `ProjectAlreadyExistsError` |
| `default_workflow` | registered in the tenant's workflow registry | `ProjectInvalidError` (field `default_workflow`) or a reference error |
| `default_lead_agent` | an agent or pool that exists | reference error |
| `host_affinity` | a fleet node in the tenant, by agent id, name or host | reference error |
| `git_signing_key_*`, `git_ssh_key_*` | an SSH key in the KMS; scope defaults to `user`, owner to the tenant | reference error |
| `git_signing_algorithm` | empty, `ED25519` or `ECDSA-P256` | `ProjectInvalidError` |
| `git_signing_enabled` | requires a signing key | `ProjectInvalidError` |
| `context_repos` | each entry has a name, URL and ref | `ContextRepoInvalidError` |
| `spend` | well-formed windows | `ProjectInvalidError` |
| `workspace_tier`, sizes | known tier; sizes ≥ 0; tmpfs only on `ephemeral` | input validation error |
| `session_rotation` | empty, `resume` or `fresh` | `ProjectInvalidError` (field `session_rotation`) |
| `git_signing_retire_*` | a KMS key, not the current one; `retired_at` not in the future | `ProjectInvalidError` |

Reference checks are shared with other services; their two failure modes
(missing vs. lookup unavailable) are in the software factory
[API reference](../software-factory/api.md#reference-fields-are-checked-on-save).

## Errors

| Error | Sentinel | Raised by | Meaning |
| --- | --- | --- | --- |
| `service.ProjectNotFoundError` | — | service | no such project in the tenant |
| `service.ProjectAlreadyExistsError` | `orchestration.ErrProjectAlreadyExists` | service | name taken in the tenant |
| `service.ProjectBoundError` | — | service | delete of a project with an SDLC binding; unbind first |
| `service.ProjectBindingCheckError` | wraps the store error | service | delete refused: the binding store could not be read |
| `service.ProjectInvalidError` | `orchestration.ErrProjectInvalid` | service | a field failed validation |
| `service.ProjectAccessDeniedError` | `orchestration.ErrProjectAccessDenied` | service, authz | role below the required one |
| `service.ProjectUnresolvedError` | `orchestration.ErrProjectUnresolved` | service, authz | the project could not be read; a refusal |
| `service.ProjectRoleInvalidError` | `orchestration.ErrProjectRoleInvalid` | service | unknown role |
| `ProjectProvisioningUnavailableError` | — | service | managed create on a server that cannot provision or clone |
| `orchestration.ProjectStoreNotFoundError`, `ProjectStoreError`, `ProjectKeySegmentError` | `ErrProjectStore` | store | persistence failures; a name or id that is not a valid key segment |
| `orchestration.InvalidProjectIDError` | `ErrInvalidProjectID` | `CreateTask`, `StartJob` | a set `project_id` that is not a UUID |
| `orchestration.ProjectParkedError` | `ErrProjectParked` | `admitIntake` | intake refused while parked |
| `orchestration.ProjectWorkspaceNotReadyError` | — | spawn | managed workspace not yet `ready` |
| `orchestration.ContextRepoInvalidError` | — | validation | malformed context repository |
| `sdlc.TaskSourceRepoMismatchError` | `ErrTaskSourceRepoMismatch` | SDLC runner | the task's source issue is in a different repository than the binding |
| `sdlc.ProjectRecordMissingError` | `ErrRunner` | SDLC runner | the job's project record is gone; parked for an operator |
| `UnsignedCommitError` | — | `RequireSignedCommit` | a commit without an accepted signature |

The CLI adds `InvalidProjectRoleError`, `InvalidContextRepoFlagError` and
`ProjectSelectorRequiredError` (`cmd/quicbaas/project.go`).

Transport mapping is in [api.md](api.md#errors).

## Configuration

A project has no daemon configuration of its own. Its behaviour is the record.
Two server facts affect it:

- **Managed workspaces** are enabled when the server wires a
  `ProjectWorkspaceProvisioner`, rooted at the orchestration workspace root;
  git-over-SSH host-key pins are kept under the same root.
- **The SDLC binding** (`repo_hosts`, forge, webhook) is SDLC configuration;
  see [the project binding](../sdlc/engineering.md#the-project-binding).

## Known gaps

Every project operation round-trips alike on REST, gRPC, Unix, MCP and the
embedded transport (QUIC serves the REST router), pinned by
`pkg/apimodule/module_project_parity_test.go`. One mismatch remains,
documented so it is not mistaken for intended behaviour:

| Gap | Effect |
| --- | --- |
| `ProjectUnresolvedError` (the authorizer could not read the project) | REST and QUIC answer `403`; gRPC, Unix, MCP and embedded answer not-found. |

## Tests

Unit tests run in memory: `make test-orchestration`, `make test-service`,
`make test-sdlc`, `make test-api` cover the packages below.

| Area | Tests |
| --- | --- |
| Store, name index, create race | `pkg/orchestration/project_store_test.go`, `project_create_race_test.go`, `project_store_tenant_context_test.go` |
| Id | `pkg/orchestration/project_id_test.go` |
| Matrix | `pkg/orchestration/project_authz_test.go`, `pkg/service/project_service_authz_test.go` |
| Parking | `pkg/orchestration/project_park_test.go`, `pkg/service/project_park_test.go`, `pkg/orchestration/sdlc/project_park_test.go` |
| Git and signing history | `pkg/orchestration/project_git_test.go`, `project_git_retired_test.go`, `pkg/service/project_git_test.go`, `project_git_signing_test.go`, `project_signing_history_test.go`, `project_git_redaction_test.go` |
| Service validation | `pkg/service/project_service_test.go`, `…_workflow_test.go`, `…_references_test.go`, `…_storage_test.go`, `…_session_rotation_test.go`, `…_autonomy_test.go`, `…_quota_test.go`, `…_tenant_store_test.go` |
| Managed workspace | `pkg/service/project_provisioner_test.go` |
| MCP | `pkg/api/mcp/project_fleet_admin_tools_test.go`, `project_storage_tools_test.go`, `project_session_rotation_tools_test.go`, `project_account_page_test.go` |
| SDK transports | `sdk/go/transport/{rest,grpc,mcp}/project_handler_test.go` and the `project_storage_test.go` / `project_session_rotation_test.go` siblings |
| Cross-transport parity | `pkg/apimodule/module_project_parity_test.go` (create, get, get by name, update, list, delete), `module_sdlc_parity_test.go` (the SDLC binding, reviewer and conformance verbs) |
| Frontend | `cmd/quicbaas/frontend/test/projectForm.test.ts` (hydrates from a verbatim `GET /api/v1/projects/{id}` body), `test/e2e/projects.spec.ts` |

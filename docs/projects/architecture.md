# Projects — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-11
> The Project record: what it is, which components own it, how a project is
> created and resolved, and the design choices behind its identity, naming,
> parking, permission matrix and signing history. Conflicts with this
> document are defects.

A **project** is the tenant-scoped unit that groups agentic work: a repository
and its commit identity, a workspace, an agent permission matrix, quotas and
spend limits, and the switches that decide whether the factory may start work
for it on its own. Jobs and Tasks carry a `project_id`; everything the factory
counts, admits, parks or publishes for a project is keyed by it.

```
Tenant ─▶ Project ─▶ Job ─▶ Task
             │
             ├─ Git:   repo, branch, author, signing policy, access key
             ├─ Workspace: fleet node + path, or a managed checkout
             ├─ Permissions: fleet node → lead | worker | observer | denied
             ├─ Quotas, Spend, Storage, SessionRotation
             └─ Autonomous, Parked, DefaultWorkflow, ContextRepos
```

## Component boundaries

| Component | Package | Owns |
| --- | --- | --- |
| Record and store | `pkg/orchestration` (`project.go`, `project_store.go`) | the `Project` type, its validation helpers, the tenant-routed keyspace and the name index |
| Identity | `pkg/orchestration/project_id.go` | `ValidateProjectID`: a project id is a UUID |
| Permission matrix | `pkg/orchestration/project_authz.go` | `ProjectAuthz`, `RoleAtLeast` |
| Intake gate | `pkg/orchestration/project_park.go` | `admitIntake`, `Park`, `Resume`, `StartedWorkRunner` |
| Git identity | `pkg/orchestration/project_git.go` | `GitAuthor`, `CommitSigning`, `RetiredSigningKey`, `KMSKeyRef` |
| Service | `pkg/service/project_service.go` | `ProjectService`: request validation, reference checks, id minting, presence-based updates, grants |
| Managed workspace | `pkg/service/project_provisioner.go`, `project_git.go` | `ProjectWorkspaceProvisioner`, `GitProvisioner` |
| Transports | `pkg/api/rest/handlers_project.go`, `pkg/api/grpc/server_project.go`, `pkg/api/mcp/project_tools.go`, `pkg/api/mcp/fleet_admin_tools.go` | wire shapes only; every rule lives in the service |
| SDK | `sdk/go/client/project`, `sdk/go/transport/*/project_handler.go` | `quicbaas.Project` client |
| CLI | `cmd/quicbaas/project.go` | `quicbaas project` |

Consumers read the record and never write it except through the service:
the task-source poller and task dispatcher (gated by `Autonomous`), job
admission (`Quotas.MaxConcurrency`), the SDLC module (binding, workspace,
publish), and the worker runtime (storage tier, context repositories, session
rotation). Their mechanics are documented with them:
[orchestration](../orchestration/), [SDLC](../sdlc/architecture.md),
[agent fleet](../agent-fleet/architecture.md), and in the software factory's
[autonomy and dispatch](../orchestration/autonomy-and-dispatch.md) and
[storage tiers](../software-factory/storage-tiers.md), and in the accounts
folder's [job admission](../accounts/architecture.md#the-lease) and
[cost enforcement](../accounts/architecture.md#spend-limits).

## Data flow

### Create

```
transport ─▶ ProjectService.CreateProject
               1. validateWorkspaceRequest      (managed vs path, credentials)
               2. default_workflow              (registered in the tenant)
               3. default_lead_agent            (an agent or pool that exists)
               4. context_repos                 (ValidateContextRepos)
               5. git                           (author, signing, ssh key)
               6. spend, storage, session_rotation
               7. reference fields              (KMS keys, host_affinity, secret ref)
               8. mint ID (UUID v4)
               9. managed? → provision workspace dir, state = pending
              10. ProjectStore.Create           (record, then name reservation)
              11. managed? → background clone   (cloning → ready | failed)
```

Validation runs before anything is written, so a refused create leaves no
record, no reservation and no directory. The order is
`pkg/service/project_service.go` `CreateProject`.

### Resolve

Every read and write is tenant-scoped. Records live in the owning tenant's
routed namespace, so a lookup made under the wrong tenant finds nothing — the
same answer as a project that never existed. That is why an authorization
check that cannot read the project refuses rather than allows
([Permission matrix](#permission-matrix)).

### Update

Updates are presence-based: a field the request omits is left alone. Fields
where zero or `false` is a meaningful value — quotas, sizes, `autonomous`,
`parked`, `git_signing_enabled`, `spend` — travel as pointers, so "unchanged"
and "set to zero/off" are distinguishable. Fields that can be emptied have an
explicit `clear_*` flag (`clear_description`, `clear_default_workflow`,
`clear_context_repos`). The merged record is validated as a whole before it is
stored; a merge that would leave signing enabled without a key is refused and
nothing changes.

### Delete

Delete removes the record and its name reservation. A managed project's
workspace directory is torn down with it.

A project with an SDLC binding is refused with `ProjectBoundError`, before
anything is torn down. Delete never cascades into offboarding: unbind first
(`quicbaas sdlc unbind <project-id>`), then delete. A binding store that
cannot be read also refuses the delete (`ProjectBindingCheckError`). The
binding reader is wired on every node with a tenant store resolver, whether
or not the SDLC module is enabled. A node without a resolver has no binding
store, so no binding can exist, and the delete goes ahead.

Known gap: the check and the delete are separate writes. A bind that commits
between them leaves a binding on a deleted project.

## Identity: id, name and source repository

### A project id is a UUID

A project has two identifiers and only one of them is an id. `Project.ID` is
the UUID `CreateProject` mints; `Project.Name` is free text the operator chose.
Passing the name where the id belongs does not fail on its own: the project
listing filters on the id, the concurrency quota counts leases under the id's
key subtree, and a lease writes its project segment verbatim. Each of those
quietly finds nothing.

So the value is checked where it is accepted. `CreateTask` and `StartJob`
refuse a `project_id` that is set but is not a UUID with
`InvalidProjectIDError` (`ErrInvalidProjectID`), naming the value and the
field. An **empty** `project_id` is accepted and means unscoped — not every
task or job belongs to a project. The id is checked verbatim, never trimmed or
case-folded, because it is stored and keyed verbatim.

### Name uniqueness

A project name is unique within a tenant, and the store decides it: the name
index entry is claimed with a guarded create (`PutIfAbsent`), so exactly one of
any number of concurrent creators wins.

> **A name reservation is published only after its project's record exists.**

`Create` writes the record first; a rename re-points the index of a record
that already exists. The heal that reclaims a stranded reservation fires on one
predicate — *the reservation names a project with no record* — and the
ordering is what makes that predicate decidable: a live holder always has a
record by the time its reservation is visible, so a holder without one is a
deleted project, never a creator that is mid-write.

> **A creator that has not crashed cannot have its reservation taken.**

The residue is an **orphan record**: a crash between the two writes leaves a
project reachable by id and by `ListByTenant` but absent from the name index,
so a later create may claim the name. That residue refuses nobody and is
visible to an operator.

### Source-repository identity

A project bound to a forge by the SDLC module has one repository:
`sdlc.ProjectBinding.Repo()`. A Task adopted from a forge issue names its
source issue as `owner/repo#number`. Before the SDLC runner acts on a task —
on the hook path, at outcome reporting, in the unblock sweep and in the
attention service — `ensureTaskSourceRepoMatchesBinding`
(`pkg/sdlc/issue_source.go`) compares the two and refuses a mismatch with
`TaskSourceRepoMismatchError` (`ErrTaskSourceRepoMismatch`), naming the task,
the project, the binding's repository and the source's. A task can therefore
never write to, label or close an issue in a repository its project is not
bound to, even if the binding was changed after the task was adopted.

The binding itself — the forge, the repository, onboarding and what binding
provisions — is the SDLC module's and is documented in [SDLC](../sdlc/architecture.md#onboarding-a-repository). From
the project's side, binding requires a signing policy and a repository inside
the binding's allowed hosts, and **turns the project autonomous**; it neither
reads nor sets `default_workflow`. A job whose project record cannot be read
is parked for an operator (`sdlc.ProjectRecordMissingError`) rather than
charged an attempt.

## Parking

Parking closes a project's **intake** and stops nothing. Resume reopens it.

**A task is started once it has been dispatched at least once**
(`Task.Attempts > 0`, charged by the spawn claim). A started task keeps its
whole lifecycle while the project is parked: worker turns, pull request,
review, merge train, and any requeue or readmission. A task whose spawn claim
was released because its job never launched is refunded to zero attempts and
is not started.

| While parked | Behaviour |
| --- | --- |
| Dispatch of a task that has not started (dispatcher sweep, auto-spawn at creation, `StartJob` with a `task_id`) | Refused with `*orchestration.ProjectParkedError` (`ErrProjectParked`). The task stays `pending`, uncharged; the dispatcher records the park as its `dispatch_reason`. |
| Dispatch of a started task | Admitted. |
| Adoption of new issues (task-source poller, interval and webhook push) | Nothing is polled or adopted. The push path returns `ProjectParkedError`. |
| Triage (webhook wake and the periodic backstop) | Refused; the backstop does not count it as a failed pass. |
| Review and merge-train jobs | Admitted: their runners implement `StartedWorkRunner` because they finish pull requests that are already open. |
| Any other job bound to no task | Refused. A child job a running job spawns is part of its parent and is admitted. |
| Unblock sweep, owner rulings, label reconciliation | Unchanged: they act on started tasks. |
| Webhook deliveries | Acknowledged (`202`, `"parked": true`) and the refusal logged; the queue is still swept for started tasks, no triage starts, nothing is queued for replay. |
| Running jobs, containers, fleet slots, pull requests, branches, labels | Untouched. Parking writes nothing to the forge. |

The single gate is `admitIntake` in `pkg/orchestration/project_park.go`, called
on the agent service's dispatch paths. The sweeps that list projects read
`Project.Parked` only to skip a question whose answer is known.

`Parked` and `Autonomous` are independent: parking does not turn autonomy off
and resuming does not turn it on. Park and resume ride the ordinary update
(`parked: true|false`), so every transport carries them; the service stamps
`parked_at` and `parked_by`, and parking an already parked project keeps the
original stamp.

## Permission matrix

`Permissions.FleetNodeRoles` (`agent_roles` on every wire) maps a fleet node id
to a role:

| Role | Claim worker tasks | Receive dispatched work | Spawn child jobs |
| --- | --- | --- | --- |
| `lead` | yes | yes | yes |
| `worker` | yes | yes | no |
| `observer` | no | no | no |
| `denied` | no | no | no |

Roles are ordered `denied < observer < worker < lead` (`RoleAtLeast`); an
unrecognized role counts as `denied`. An **empty** matrix admits every fleet
node as `lead`. **Once the matrix has any entry, a node it does not list is
denied** — so the first grant on a project closes it to every node not
granted.

Two writes hand a fleet node a project's work, and both consult the matrix
through `ProjectAuthz`, so they cannot drift apart:

- `ClaimTask` — the node asks for a task. Requires at least `worker`.
- The dispatch path (`SpawnTask` for the dispatcher sweep, `autoSpawnTask` at
  creation). The online fleet is narrowed to the nodes the project admits as
  workers *before* the routing matcher picks one, so a project whose own worker
  is online still dispatches, and a project that admits none of the online
  nodes is refused naming the `project_role` constraint.

**A project the store cannot read is a refusal:** `ProjectUnresolvedError`
(`ErrProjectUnresolved`, HTTP 403). A check that could not read the rule must
never answer "allowed".

The matrix is keyed on a fleet node id, so it applies only where a write names
one. `DeleteTask`, `BlockTask`, `ReadmitTask` and `CompleteTask` carry no node
principal and are gated by tenant RBAC alone. Revoking a node's role writes
`denied` rather than removing the entry, so a revoke on a project with an
empty matrix also makes the matrix non-empty and closes the project to every
unlisted node.

## Git identity and signing history

`ProjectGit` carries the repository, branch, last observed commit, the
`GitAuthor` recorded on commits, the `CommitSigning` policy and an optional
SSH access key. `CommitSigning` is a policy, not a key path: the algorithm is
`ED25519` (the default) or `ECDSA-P256` (FIPS), and the key is a `KMSKeyRef`
into the KMS. An enabled policy must name a key; it never degrades to
unsigned. `ProjectGit.AccessKey()` returns the SSH key when set and the signing
key otherwise, so one KMS key can serve as both. `GitProvisioner.CommitForProject`
overwrites a request's author and signing with the project's, so a caller
cannot substitute an identity.

A key rotation must not orphan the commits the old key signed: the factory
never rewrites a pushed branch. `CommitSigning.Retired` is the project's
**trusted signing history** — the public half of each earlier key and the
moment it was retired. The publisher (`VerifySigned`, `pkg/sdlc/publish.go`)
accepts a commit when git reports a good signature and the signer is

- the current key, for any commit; or
- a retired key, only for a commit the project's own identity committed
  **before** that key's retirement.

The retirement is enforced twice: the `allowed_signers` file carries
`valid-before="<retired_at>"` on each retired key, and the publisher compares
the times itself. The server reads a retired key's public half from the KMS,
so the history can only hold a key the tenant actually held.

## Managed workspaces

A project either names an existing workspace (`Workspace.FleetNodeID` and
`Workspace.Path`) or asks the server to manage one (`WorkspaceManaged`). A
managed workspace is created under the server's workspace root at
`<root>/<tenant>/<project_id>/workspace` (mode `0700`) and the repository is
cloned into it in the background. `Provisioning.State` moves
`pending → cloning → ready` or to `failed` with `Provisioning.Error`; a spawn
against the project before it is ready is refused with
`ProjectWorkspaceNotReadyError`. Clone credentials come from a tenant secret
(`GitCredentialSecretRef`) or the project's SSH key, never both. A server
without a provisioner, or one that cannot clone, refuses a managed create with
`ProjectProvisioningUnavailableError`.

Every transport's create request carries `workspace_managed` and
`git_credential_secret_ref`.

## Rejected alternatives

| Alternative | Why it was rejected |
| --- | --- |
| Use the project **name** as its key | Names are operator-chosen and renamable; keying leases, quotas and listings by a renamable string would move or orphan them on rename. The UUID is immutable and the name is an index. |
| Reserve the name **before** writing the record | A creator that has not yet written its record is indistinguishable from a crashed one; a rival heals the live reservation and both creates succeed. No confirmation read closes the window. |
| Fail open when the project cannot be read | Records are tenant-routed, so a wrong-tenant lookup looks like a missing project; allowing on "not found" would let any cross-tenant mistake bypass the matrix. |
| Veto the router's pick instead of narrowing the fleet first | A project whose own worker is online would be refused whenever the router happened to pick another node. |
| Parking cancels in-flight work | Parking exists to stop new spend without losing work already paid for; cancelling would discard running turns and open pull requests. |
| Parking turns `autonomous` off | It would lose the operator's autonomy setting; resume would have to guess it. The two switches stay independent. |
| Trust only the current signing key | Every pre-rotation commit on an open branch or merge train would be refused for ever, since pushed branches are never rewritten. |
| Store a signing key path or the key itself on the project | Key material belongs in the KMS; the project stores a reference and the KMS enforces who may use it. |
| Plain scalar quotas on update | Zero is a value (unlimited), so an update could not tell "lift the cap" from "leave it alone". |
| A nested `git` / `workspace` / `quotas` wire shape | One flat shape (`ProjectRESTResponse`) is what every transport serializes; a nested mirror in one client silently read blanks. |

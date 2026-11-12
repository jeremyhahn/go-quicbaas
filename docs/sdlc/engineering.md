# SDLC — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-11
> The SDLC module's exported surface, invariants, typed errors, the project
> binding and configuration. The design is [architecture.md](architecture.md).
> Conflicts with this document are defects.

The engine's own contract is
[../software-factory/engineering.md](../software-factory/engineering.md). The
`sdlc` module adds one service, two records and one closed set of typed errors
on top of it.

## Exported surface

Symbols are listed by file, not by line, so the table does not go stale when
a file is edited.

| Symbol | File | What it is |
|---|---|---|
| `sdlc.ProjectBinding` | `pkg/sdlc/binding.go` | The project's autonomous-development configuration. See [the fields](#the-project-binding) |
| `sdlc.ForgeConfig` | `pkg/sdlc/binding.go` | The `forge:` half: type, API root, owner/repo, trigger label, label map, secret refs, pinned `SSHHostKey` |
| `sdlc.ReviewPolicy` / `sdlc.RankingPolicy` | `pkg/sdlc/binding.go` | The review gate, and the project's contribution to dependency ranking |
| `orchestration.AgentRef` | `pkg/orchestration` | The binding's `worker` and `triage` slots: an Agent or a pool, which carries the backend, the model and the image |
| `sdlc.MergeTrainPolicy` / `sdlc.ClusteringPolicy` / `sdlc.OnboardingConfig` | `merge_train.go`, `triage_cluster.go`, `onboarding_config.go` | The binding's `merge_train`, `clustering` and `onboarding` blocks |
| `sdlc.Delivery` | `pkg/sdlc/delivery.go` | What one attempt delivered: branch, base, target, head SHA, `DeliveryPullRequest`, `[]ReviewRecord`, `[]CheckRecord`, CI reruns and the last turn |
| `sdlc.IssueSource` | `pkg/sdlc/issue_source.go` | The `forge_issue` `TaskSource`; `NewIssueSource(bindings, forges, clusters, pageLimit)` |
| `sdlc.Hooks` | `pkg/sdlc/hooks.go` | The lifecycle moments; `NewHooks(HooksConfig)` |
| `sdlc.SurfacePolicy` | `pkg/sdlc/blocked.go` | Retry attempts, interval and post timeout for surfacing a park to the issue |
| `sdlc.UnblockCondition` / `sdlc.UnblockKind` | `pkg/sdlc/readmission.go` | The typed vocabulary a park is expressed in, and what a sweep can decide for itself |
| `sdlc.DependencyAnalyzer` / `sdlc.DependencyManifest` | `pkg/sdlc/analyzer.go` | Blast-radius ranking inputs; `NewStaticManifestSource` for a declared graph |
| `sdlc.WebhookRouteIndex` | `pkg/sdlc/webhook_index.go` | The system-plane index that routes an unauthenticated webhook to a tenant |
| `sdlc.AttentionService` | `pkg/sdlc/attention_service.go` | The attention inbox: parked tasks that need a person, and their resolution |
| `sdlc.IssueAdopter` | `pkg/sdlc/adopt.go` | Adopts an issue no task holds, optionally with its open pull request and an owner ruling |
| `service.SDLCService` | `pkg/service/sdlc_service.go`, `sdlc_operator.go`, `sdlc_worker_image.go` | `Bind`, `Unbind`, `Binding`, `GetBinding`, `ListBindings`, `Onboard`, `Conformance`, `OpenWork`, `ReviewerSelection`, `SetReviewerOverride`, `ClearReviewerOverride`, `Delivery`, `AdoptIssue`, `Attention`, `FileIssue`, `Collaborators`, `WorkerImageStatus`, `RefreshWorkerImage`, `NudgeProject`, `ForgeFor`, `ForgeToken`, `WebhookSecret` |

## Invariants

- **`Bind` turns the project autonomous** (`enableAutonomy`) and registers no
  workflow: an `sdlc` job has no workflow, and `default_workflow` belongs to
  the lead-driven `workflow` runner.
- **The binding stores secret *references*, never secret values.** `Bind`
  proves each reference resolves in the tenant's secret store
  (`checkSecrets`, and `checkReviewerSecrets` for each review seat) before the
  record is written.
- **A binding is refused for a project the pipeline could not deliver**
  (`checkProject`): no signing policy, an invalid one, or a repository host
  outside `sdlc.repo_hosts`.
- **Tenancy is the caller's own.** The tenant bound to the request by the
  authenticator wins; a `tenant_id` query naming another tenant is refused
  rather than honoured (`sdlcTenant`, `pkg/api/rest/handlers_sdlc.go`, and its
  gRPC twin in `pkg/api/grpc/server_sdlc.go`).

## Error semantics and the status mapping

`respondSDLCError` (`pkg/api/rest/handlers_sdlc.go`) and its gRPC twin
`convertSDLCError` (`pkg/api/grpc/server_sdlc.go`) are the two mappings; Unix
inherits the gRPC one verbatim and QUIC the REST one. Both first answer the
service layer's reference errors: `ReferenceNotFoundError` is `400` /
`InvalidArgument` and `ReferenceUnavailableError` is `503`, naming the field
the operator has to fix.

| Error | HTTP | gRPC |
|---|---|---|
| `sdlc.ErrNotFound`, an orchestration not-found error, `service.ProjectNotFoundError` | `404` | `NotFound` |
| `sdlc.ErrAlreadyExists`, `sdlc.ErrVersionConflict`, `sdlc.ErrWebhookRouteConflict`, `orchestration.ErrTaskSourceAlreadyAdopted` | `409` | `AlreadyExists` |
| `sdlc.ErrAdoptionRefused`, `orchestration.ErrTaskInvalidState` (an attention item another caller already resolved) | `409` | `FailedPrecondition` |
| `sdlc.ErrInvalidInput`, `sdlc.ForgeNotRegisteredError`, `sdlc.StrategyNotRegisteredError`, `orchestration.ErrWorkflowValidation`, `orchestration.ErrInvalidInput` | `400` | `InvalidArgument` |
| `sdlc.ErrReview` | `500` | `InvalidArgument` |
| `sdlc.ErrAdoptionIncomplete` — the task exists and runs; a forge write after it did not land | `502` | `Unavailable` |
| `rest.ErrSDLCTenantMismatch` — `tenant_id` is not the caller's own | `403` | `PermissionDenied` |
| `sdlc.ErrUnavailable`, `rest.ErrSDLCServiceUnavailable` — the module is not running on this node | `503` | `Unavailable` |
| anything else | `500` | `Internal` |

**Fail-closed paths.** `NewForgeBackend` refuses a forge type this build
carries no adapter for rather than degrading; a webhook whose project,
secret or signature does not check answers the same `401 unauthenticated`
for all three, so the response distinguishes nothing an attacker could use;
a verification criterion with no containerized session is refused with
`VerificationNotContainerizedError` rather than run on the daemon; an
`ssh://` repository with no pinned host key is refused before the worker
spawns (`orchestration.SSHHostKeyError`).

## Persistence

The module persists two tenant-scoped records — the binding and the
delivery — plus one system-plane webhook route index. Everything else a reader
sees is the engine's Project, Task, Job and Session.

## The project binding

A **binding** attaches an orchestration Project to a forge repository and is
that project's autonomous-development configuration: which repository it
delivers to, which SDLC it follows, which agent writes the code, what verifies
the work, and how its pull requests are judged. It is `sdlc.ProjectBinding`
(`pkg/sdlc/binding.go`), stored per tenant, and it is the only record the
module owns besides the delivery.

- The full wire shape, and every transport it travels on, is in
  [api.md](api.md).
- Binding a live project step by step is in
  [user.md](user.md#bind-a-project-and-follow-one-issue).

| Field | What it is | Where it is documented |
|---|---|---|
| `project_id` | The orchestration Project the binding attaches to | — |
| `forge` | The repository and how the module reaches it | [below](#forge-fields) |
| `strategy` | The registered SDLC the project follows; resolved fail-closed | [The SDLC strategy](architecture.md#the-sdlc-strategy) |
| `worker` | The Agent or pool that writes the code; every Agent it resolves to declares the coding role | [../software-factory/agents.md](../software-factory/agents.md) |
| `worker_prompt_append` | Run-time prompt material after the worker's charter | below |
| `verification` | Shell commands that must pass before review, run in the worker's checkout | [Verification](architecture.md#verification-judges-the-merge-result-not-the-branch-tree) |
| `review`, `reviewer`, `reviewers` | The review gate, the default review seat and the alternate profiles | [review.md](review.md) |
| `modules` | How the worker reaches private Go modules | [below](#modulesrewrite_prefix--private-go-modules-resolve-at-the-forge) |
| `max_review_rounds` | Review rounds before escalation to a person; zero takes 3 | [review.md](review.md) |
| `ranking` | The project's contribution to task ordering | [Issue priority](architecture.md#issue-priority-blast-radius-shifted-by-priority-labels) |
| `triage`, `triage_prompt_append`, `triage_may_close` | The triage Agent (empty takes `sdlc.triage_agent`, then the worker), its prompt material, and whether it may close an already-done issue | [triage.md](triage.md) |
| `discovery` | `poll`, `webhook` or `both` (the default) | [architecture.md](architecture.md) |
| `readiness` | What an issue must satisfy before it becomes a Task | [../software-factory/admission-and-readiness.md](../software-factory/admission-and-readiness.md) |
| `onboarding` | What provisioning writes to the forge and what conformance checks | [Onboarding a repository](architecture.md#onboarding-a-repository) |
| `merge_train` | Whether approved pull requests land through the train; on by default | [merge-train.md](merge-train.md) |
| `clustering` | Whether triage adopts related ready issues as one task; on by default | [triage.md](triage.md#clustering--related-ready-issues-become-one-task) |
| `rulers` | Forge logins with owner-ruling authority for this project | below |

The rest of this section documents the **forge half** of the record —
`forge:` — and, in detail, the one field of it that decides whether a worker
can check out at all over ssh.

`rulers:` (a list of forge logins) adds owner-ruling authority for this
project on top of the repository owner and the node's `sdlc.rulers`. Each is
validated at bind time: a malformed login is `400`, and a login that is neither
the owner nor a collaborator of the repository is refused with
`RulerNotCollaboratorError`. See
[user.md](user.md#answering-on-the-issue-automatic-readmission).

Each slot may carry run-time prompt material that is appended after its role's
charter, separated by a horizontal rule. `worker_prompt_append` applies to every
worker session and `triage_prompt_append` to every triage session. A review
seat's own `prompt_append` (on `reviewer` and on each of `reviewers[]`) applies
to every review that seat runs, so it follows the seat through a failover. The
charter itself is never replaced. Each block is bounded at 64 KiB, and an
oversized block is refused at bind time with `400` (`InvalidArgument`) naming
the field. The rules for composing these blocks are in
[agents.md](../software-factory/agents.md#run-time-prompt-material).

### `forge:` fields

| Field | Required | What it is |
| --- | --- | --- |
| `type` | yes | The adapter: `gitea`, `github` or `gitlab`. Only an adapter this build carries is accepted; `GET /api/v1/sdlc/forges` lists them. |
| `base_url` | yes | The forge's **API root**, version segment included (`/api/v1` for Gitea) — not the site origin. |
| `owner` / `repo` | yes | The repository on the forge. |
| `trigger_label` | yes | The label an OPEN issue must carry before the module adopts it as a Task. Compared exactly, case-sensitively. |
| `labels` | no | The module's label keys mapped to the forge labels that display them. Display output only. |
| `token_secret_ref` | yes | Names the tenant secret holding the forge API token. The value never enters this record. |
| `webhook_secret_ref` | when `discovery` is `webhook` | Names the tenant secret holding the webhook HMAC key. Without it no delivery verifies, so the project is discovered by the periodic passes alone; a `webhook` binding, which runs none, is refused without it. |
| `ssh_host_key` | for an ssh checkout | The forge's own SSH **host public key**, pinned. See below. |

The binding also carries an `onboarding:` block, outside `forge:`, declaring
what provisioning writes to the forge and what conformance checks — see
[Onboarding a repository](architecture.md#onboarding-a-repository).

The token and webhook secrets are **references**. The values stay in the
tenant's secret store; the binding carries their names.

### `ssh_host_key` — the forge is pinned, never learned

A project whose `git.repo_url` is an `ssh://` (or `git@host:path`) remote is
cloned inside the worker container by a real OpenSSH client, and ssh will not
talk to a host it cannot recognise. The worker image ships **no**
`known_hosts` and the container's `$HOME` is fresh per attempt, so there is
nothing for ssh to recognise the forge from unless the operator says what the
forge is.

`ssh_host_key` is that statement. It holds **one OpenSSH public-key line** —
exactly what `ssh-keyscan` prints after the host field:

```
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA…
```

Get it, once, from the forge host itself:

```
ssh-keyscan -t ed25519 -p 2222 <forge-host> | awk '{print $2, $3}'
```

Paste the two fields (`<type> <base64>`, a trailing comment is allowed) into
the binding. Do **not** paste the whole `ssh-keyscan` line: the host pattern
in front of the key is derived from the project's repository URL, and a line
that still carries one is refused on bind with `forge.ssh_host_key`.

#### What the daemon does with it

Per attempt, before any container starts
(`pkg/orchestration/workspace_spec.go`):

1. The host pattern is derived from the project's repository URL — bare
   hostname on port 22, `[host]:port` otherwise; both the `ssh://` and the
   `git@host:path` spellings are recognised.
2. The pinned key is re-rendered from its parsed form and written as a
   single-entry `known_hosts` beside the workspace spec, mode `0400`, in the
   directory the container mounts **read-only** at
   `/run/quicbaas/workspace`.
3. The spawn carries

   ```
   GIT_SSH_COMMAND=ssh -o UserKnownHostsFile=/run/quicbaas/workspace/known_hosts -o StrictHostKeyChecking=yes
   ```

   so the worker's `git clone`, `git fetch` and `git push` verify the forge
   against that file and nothing else.

**`StrictHostKeyChecking=yes` is not a tunable and `accept-new` is never
used.** A worker holds a live socket to a KMS-backed signing agent for as long
as its attempt runs; trust-on-first-use would hand that socket to whatever
machine answered at the forge's address.

#### Refusals

| Situation | What happens |
| --- | --- |
| `ssh_host_key` is not one OpenSSH public-key line | Refused **on bind**, `forge.ssh_host_key`, so a typo surfaces while the operator is looking at it. |
| A whole `known_hosts` line, or several lines | Refused on bind; the message names the host pattern to drop. |
| Repository URL is `ssh://…` and no key is pinned | The attempt is refused **before the worker spawns**, with `orchestration.SSHHostKeyError` naming `forge.ssh_host_key`. No container is started. |
| Repository URL is `https://…` | No key is needed; the forge token authenticates the clone. A key pinned anyway is harmless and ignored. |
| The forge presents a key other than the pinned one | The clone fails inside the container with ssh's own `Host key verification failed`, and the attempt fails with the git output as evidence on the job. |

A **context repository on a different ssh host** is not covered: a binding
pins one forge, so only the project's own host gets an entry and a clone from
elsewhere fails host-key verification. Put context repositories on the forge
the project is bound to, or reach them over https.

#### The worker image's half

None of the above helps if the container has no `ssh` binary — git has no
built-in ssh transport and execs one. The worker image
(`deployments/agents/software-factory-worker/Dockerfile`) installs
`openssh-client` alongside the `openssh-keygen` that `gpg.format=ssh` signing
needs, and asserts both at BUILD time as the uid the spawn runs as:

```
RUN set -eux; git --version; ssh -V
```

`make test-worker-image` re-checks the same tools on the built image. No host
key and no forge address is ever baked into the image.

### `modules.rewrite_prefix` — private Go modules resolve at the forge

A Go module is named by its import path (`github.com/<owner>/<repo>`); a
private one is fetched by git from wherever the code lives — here, the forge.
`go build` in a fresh container asks the public proxy for that path, is told
it does not exist, and the verification fails before one line of the change
compiles. Measured on go-quicbaas#53 (2026-09-21): three review rounds of an
identical dependency-resolution failure, then an escalation, for work that
was never at fault. The first two factory projects never hit it because
neither depends on a private module.

`modules.rewrite_prefix` names the import-path prefix whose modules the forge
hosts under the project's own owner, as an https URL with a trailing slash:

```json
"modules": {"rewrite_prefix": "https://github.com/example/"}
```

From it the runner puts four variables in the **worker's** container
environment, and nothing anywhere else:

| Variable | Value | Why |
| --- | --- | --- |
| `GOPRIVATE` | `github.com/example/*` | the proxy and the checksum database are bypassed for the owner's modules |
| `GIT_CONFIG_COUNT` | `1` | git reads the rewrite below without a config file being written |
| `GIT_CONFIG_KEY_0` | `url.ssh://git@forge.example.com:2022/example/.insteadOf` | the rewrite target: the project's own `git.repo_url` with the repository dropped — same scheme, user, host, port and owner |
| `GIT_CONFIG_VALUE_0` | `https://github.com/example/` | the prefix being rewritten |

This is the mechanism the handbook's dependency-resolution convention
prescribes for containers, and nothing secret travels in it: the rewrite is
to the same ssh remote the project itself is cloned from, so the access key
already in the container's ssh-agent and the host key already pinned in its
`known_hosts` reach the sibling modules exactly as they reach the project.
`GOFLAGS`, `GONOSUMDB` and `GOINSECURE` are never set.

A prefix that is not `https://<host>/<owner>/` is refused at bind time. A
project whose `git.repo_url` carries no owner segment to derive the target
from is refused at spawn, before a worker is charged. An empty policy injects
nothing, so a project with no private modules gets the image's Go untouched.

## Configuration

The module is configured under `sdlc:`. Everything generic about a job —
its attempt budget, lease, stage timeouts, idle thresholds, the poller's
cadence — is the engine's and lives under `orchestration:`.

```yaml
sdlc:
  enabled: true
  # Forge hosts a bound project's repository may live on (hostname or
  # host:port). Empty refuses every repository.
  repo_hosts: ["git.example.com"]   # the deployment's own forge host
  allow_local_repos: false          # file:// and bare paths; test rigs only
  # DECLARED AND UNREAD (issue #625). What the sdlc runner runs is the
  # binding's own `verification` list, and what admits a command is the
  # engine's ExecutionGuard allowlist below. This key is validated at load
  # and then read by nothing; set orchestration.verification_commands.
  verification_commands: ["make build", "make test", "make lint"]
  merge_attempts: 6                 # retries while the forge says not-ready; still not mergeable after = conflict
  issue_page_limit: 50              # issues per project sweep
  # Where the worker's container finds its checkouts: the project at
  # <checkout_root>/<project name> and each context repository beside it.
  # It is the path INSIDE the container — the daemon clones nothing — and it
  # is what the runner's clone commands name. Name it only if the worker image
  # lays its filesystem out differently.
  checkout_root: /workspace
  # Who, beyond each repository's owner and its binding's `rulers`, may rule
  # on a parked task by commenting on its issue. File-only. A tenant entry
  # replaces `default` for that tenant; unset `default` takes company-agent;
  # `default: []` names nobody. A malformed login refuses startup. Tenant IDs
  # are map keys, so viper lowercases them.
  rulers:
    default: [company-agent]
    tenants:
      example-tenant: [company-agent, lead-agent]
  # The node's default triage Agent or pool, per tenant that defines it.
  # Empty falls back to each project's worker. See triage.md.
  triage_agent: {agent_id: triage-reader}
  # Backstop sweeps; each is ordinarily woken by a webhook or a queue event.
  triage_sweep_interval: 15m        # re-triage every bound project
  review_sweep_interval: 5m         # review green, unjudged pull requests
  unblock_sweep_interval: 10m       # readmit blocked tasks whose condition cleared
  train_sweep_interval: 5m          # start a merge train where a change waits
  worker_image:                     # the scheduled, gated refresh; see "The worker image"
    enabled: true
    repository: quicbaas-software-factory-worker
    registry: ""                    # empty = local mode; host[:port]/owner/name to push
    registry_username: ""           # these three together, or none
    registry_token_secret: ""       # a tenant secret name, never a file
    registry_token_tenant: ""
    npm_registry_url: https://registry.npmjs.org
    refresh_interval: 24h
    pass_timeout: 1h
    smoke_model: claude-opus-5-5
    smoke_host: ""
  surfacing:                        # the Blocked hook's post to the issue
    retry_attempts: 5
    retry_interval: 2m
    post_timeout: 30s
  label_reconcile:                  # the repair sweep; see "Reconciliation"
    interval: 5m                    # how often the forge is re-read
    forge_interval: 200ms           # minimum spacing between its forge calls
                                    # env: QUICBAAS_SDLC_LABEL_RECONCILE_INTERVAL
                                    #      QUICBAAS_SDLC_LABEL_RECONCILE_FORGE_INTERVAL
                                    # the module's own scalars are bound too, incl.
                                    # QUICBAAS_SDLC_ENABLED — an env-only
                                    # deployment must be able to switch it on.
                                    # verification_commands and repo_hosts are
                                    # file-only: viper will not decode an env
                                    # list into []string through Unmarshal.
  webhook:
    max_body: 1048576               # bytes; the signature check buffers the body
    # THIS NODE's externally reachable ORIGIN: scheme, host, optional path.
    # REQUIRED when the module is enabled and deliberately not defaulted:
    # onboarding provisions forge webhooks pointing at
    # <public_url>/api/v1/sdlc/webhook/<projectID>, and a guessed origin would
    # provision every managed repository to deliver into the void and report
    # each one provisioned.
    #
    # A QUERY, A FRAGMENT AND USERINFO ARE REFUSED AT LOAD. The route above is
    # APPENDED to this value, so "https://factory.example.com?x=1" yields a
    # delivery URL whose path is "/" with the route buried in a query string —
    # and nothing notices: the hook is provisioned, read back, compared to the
    # same malformed string, and reported conformant while every delivery
    # misses the endpoint. A PATH is allowed, for a node behind a reverse
    # proxy: "https://factory.example.com/quicbaas" appends correctly.
    # env: QUICBAAS_SDLC_WEBHOOK_PUBLIC_URL
    public_url: https://factory.example.com
  forge:                            # every forge call; see "Forge transport"
    read_timeout: 30s               # one attempt at a read
    write_timeout: 1m               # a light write: label, comment, status
    heavy_write_timeout: 5m         # merging or opening a pull request
    read_attempts: 3                # attempts at an unanswered read or a
                                    # write proven safe to repeat
    retry_backoff: 1s               # first wait between attempts; doubles
                                    # env: QUICBAAS_SDLC_FORGE_* (e.g.
                                    #      QUICBAAS_SDLC_FORGE_HEAVY_WRITE_TIMEOUT)
    # token_commands:               # per forge type (github, gitea); optional
    #   github:
    #     argv: ["/usr/local/bin/forge-mint", "--app", "factory"]
    #     refresh_before: 5m        # run again this long before expires_at
    #     timeout: 30s              # one run
    #     allowed_base_urls: ["https://api.github.com"]   # required
    #
    # A token command is node configuration only: no tenant, project or binding
    # can name one, and a binding of a listed forge type never reads its
    # token_secret_ref. The command runs only for a binding whose base URL is one
    # of allowed_base_urls; any other is refused before the command runs, so a
    # binding cannot send the node's credential to a host it chose. A reviewer
    # with its own token_secret_ref keeps that identity. The command runs without a shell, with the daemon's
    # environment plus QUICBAAS_FORGE_TYPE and QUICBAAS_FORGE_BASE_URL, and
    # prints one JSON object on stdout: {"token": "...", "expires_at":
    # "<RFC 3339>"}. The token is cached per forge base URL and the command runs
    # again, once for all concurrent callers, at expires_at - refresh_before;
    # the token never reaches a log, an error or the command's argv.

orchestration:
  sweep_interval: 30s               # task-source poller cadence
  reap_lease_ttl: 90s               # dead-job detection
  # The ExecutionGuard allowlist: the operator decides, on the DAEMON, which
  # commands a criterion may run. An admitted sdlc verification command then
  # runs INSIDE the worker's container, in its checkout — never on the daemon
  # — so it needs the worker image's toolchain, not the daemon host's.
  verification_commands: ["make build", "make test", "make lint", "make security"]
  stage_timeouts: {prepare: 10m, reviewable: 10m, complete: 10m, cleanup: 2m, blocked: 1m}
  idle_thresholds: {reviewing: 24h}
  # Where the daemon binds each attempt's KMS-backed signing-agent socket.
  # Empty derives $XDG_RUNTIME_DIR/quicbaas/<instance>, or <storage.data_dir>/s
  # when there is no runtime directory. <instance> is per-DAEMON, so two
  # daemons of one user do not share a root. See "The signing socket's root".
  sockets_root: /run/user/1000/quicbaas/3f0a1c9d24b7
```

### The signing socket's root

The worker's commit is signed inside its container against an ephemeral
agent the daemon holds; what is mounted in is a unix socket, and the key
never leaves the KMS. **That socket is bound under
`orchestration.sockets_root`, not under the project's workspace.**

A unix socket address is a fixed-size kernel field — `sun_path` is 108
bytes, so 107 is the longest path that can be bound — and a managed
workspace is `<projects_root>/<tenant_id>/<project_id>/workspace`, which
spends 88 bytes on the tenant and project alone. Under the default projects
root that came to 148 bytes on a real node, so the workspace placement
failed on every realistic deployment.

| | |
|---|---|
| key | `orchestration.sockets_root` (absolute path; refused otherwise) |
| default | `$XDG_RUNTIME_DIR/quicbaas/<instance>`, else `<storage.data_dir>/s` |
| `<instance>` | 12 hexadecimal characters, the leading bytes of the SHA-256 of the **absolute** `storage.data_dir` |
| longest usable root | 83 bytes — the root, plus a 12-character session directory, plus `/agent.sock` |
| created | at startup, mode `0700`, by the daemon |
| per attempt | `<sockets_root>/<12 hex characters>/agent.sock`, created `0700` by the agent and removed when the attempt ends |

The root's rules — the budget, the refusals and the startup sweep — are
`orchestration.EnsureSigningSocketRoot`, shared with a fleet node's relay root
(`quicbaas fleet serve --sockets-root`, go-quicbaas#1013); the daemon adds only
its signing-root marker (`sdlc.EnsureSigningSocketRoot`).

Three properties are load-bearing:

- **It is checked at startup, not at the first delivery.** The arithmetic is
  fixed, so a root past the budget can never work. The daemon refuses to
  start with a `SigningSocketRootError` naming the root, the longest path it
  yields and the limit — instead of starting, looking configured, and
  failing every job at `initializing` with a listen error that names nothing.
  A root that cannot hold a socket safely — world-writable (which is what
  the OS temporary directory is), a symlink, not a directory — is refused
  the same way, with a `SigningSocketRootUnusableError`.
- **The mount policy admits it by construction.** There is no allowlist
  entry to remember: `orchestration.sandbox.allowed_mount_roots` is for the
  operator's own directories. The grant is the narrowest one that works —
  the socket file itself, one directory below the root, read-only, and
  actually a socket on disk. The root, the session directory, and anything
  beside or below the socket stay denied.
- **It is swept at startup.** An attempt that ends releases its agent, but a
  daemon *killed* between the agent's bind and its release cannot, and the
  root outlives the daemon — so without a sweep every hard kill costs the
  node one dead socket for ever. Preparing the root removes the session
  directories left behind and the daemon logs the count at INFO
  (`sdlc: sockets root <root> swept: <n> orphaned signing agent session(s)
  removed`). A sweep that cannot be completed — the root refusing to be
  enumerated, or an orphan refusing to be removed — is a
  `SigningSocketSweepError` and a startup failure, for the same reason an
  unusable root is.

  What is swept is only what the daemon mints: a directory named exactly 12
  hexadecimal characters, holding nothing but `agent.sock`, and that socket
  answering nobody. **Liveness is decided by the socket, not by the clock** —
  an agent's listener dies with the process that held it, so a socket that
  refuses a connection belongs to no running attempt. The simpler invariant
  ("the daemon has only just started, so nothing here can be live") holds for
  one daemon per root; the derived root is now one daemon's, but an operator
  who points two daemons at one explicit `sockets_root` gets the probe rather
  than a wrong assumption. Anything else under the root — a foreign name, a
  file, a symlink, a directory with other contents in it, a directory the
  daemon may not read — is left exactly where it is, and a root the refusals
  above reject is never swept at all.

#### The derived root is per-daemon, not per-user

`$XDG_RUNTIME_DIR` is a **per-user** directory. A root derived from it alone
is reached by every `quicbaasd` the user runs, so two daemons with different
data directories bound their session sockets under one root — and one
daemon's startup sweep could reach the other's live sessions. The derived
root therefore carries a per-daemon segment:

```
$XDG_RUNTIME_DIR/quicbaas/<instance>/<12 hex characters>/agent.sock
         per-user       per-daemon        per-attempt
```

- **`<instance>` is derived from the absolute `storage.data_dir`** — the one
  setting two instances on one host cannot share. There is no node identity
  to borrow: `raft.node_id` defaults to `1` for every standalone daemon, and
  the node name the orchestrator uses is deliberately stable across restarts
  and shared by every daemon on a host. It is the leading 12 hexadecimal
  characters of a SHA-256 digest, truncated so the segment stays fixed-width
  and inside the socket budget; it names a directory in the user's own
  private runtime directory and authorises nothing.
- **The path is resolved first**, so two daemons started from different
  working directories with the same relative `data_dir` are still different
  daemons. A working directory that cannot be read is a startup failure
  (`config.SocketsRootIdentityError`), not a shared root.
- **A daemon with no `storage.data_dir` derives no root at all.** It has
  nothing to be identified by, and the runner refuses the empty root by name
  rather than letting it share the user's.
- **The data-directory derivation takes no segment.** `<storage.data_dir>/s`
  is already inside the directory that distinguishes the daemons, and a
  segment there would spend socket budget for nothing.
- **An explicit `orchestration.sockets_root` is used exactly as written.** It
  names one deployment already; appending a segment would silently move the
  directory the operator provisioned.

### Removed keys

The following keys are refused **by name** at load (`config.RefuseRemovedKeys`
over the `config.RemovedKeys` map, failing with `RemovedKeyError`, which wraps
`ErrRemovedKey`), from the file and from the environment. Pre-v1: there is no
migration path; rename by hand. The same map also refuses the removed
`orchestration.lead_agent.*` tuple keys, documented with the engine.

| removed | where it went |
|---|---|
| `sdlc.triage_backend`, `sdlc.triage_model` | `sdlc.triage_agent.agent_id` or `.pool_id` — a tenant Agent or pool, which carries the backend and the model |
| `sdlc.worker_image.registry_token_file` | `sdlc.worker_image.registry_token_secret` and `registry_token_tenant`: the push token is a tenant secret, never a plaintext file |
| `sdlc.attempt_budget` | the engine's per-task budget (`Task.AttemptBudget`, default `orchestration.DefaultAttemptBudget`) |
| `sdlc.reap_lease_ttl` | `orchestration.reap_lease_ttl` |
| `sdlc.stage_timeouts` | `orchestration.stage_timeouts` |
| `sdlc.idle_thresholds` | `orchestration.idle_thresholds` |
| `sdlc.dispatch_cap` | the project's `quotas.max_concurrency` (live job leases per project) |
| `sdlc.run_timeout` | `orchestration.idle_thresholds` |
| `sdlc.sweep_interval` | `orchestration.sweep_interval` |
| `sdlc.mirror_lease_ttl` | nothing; adoption is the task store's guarded source index |
| `sdlc.queue_capacity`, `sdlc.delivery_memory` | nothing; a verified webhook nudges the poller, re-polling is idempotent |
| `sdlc.max_webhook_body` | `sdlc.webhook.max_body` |
| `sdlc.stage_event_flush_timeout` | nothing; job events are the engine's own stream |
| `sdlc.agent_config_dir` | `orchestration.agent_config_dir` — the ONE knob; the engine puts it on every session it spawns (`JobLifecyclePolicy.AgentConfigDir`) |
| `sdlc.worktree_root`, `sdlc.companions` | nothing; the daemon does no clone, copy or worktree. The worker's container clones the project and the project's `context_repos` itself — see [Autonomous projects](../orchestration/autonomy-and-dispatch.md) |

### The worker image

The worker's [Agent](../software-factory/agents.md) names the container image the job's one worker
session runs in. Naming one requires the sandbox for that project's workers.

This repository builds one, and the daemon keeps it current by itself — see
[the worker image refresh](../agent-fleet/worker-image.md):

```
make build-worker-image      # quicbaas-software-factory-worker:<VERSION>-cc<claude-code>-cx<codex>
make test-worker-image       # builds it, then runs the refresh gate's probes against it
make push-worker-image WORKER_IMAGE_REGISTRY=<host>/<owner>/software-factory-worker
```

```yaml
# the Agent the binding's worker names
id: claude-coder
roles: [coding]
backend: claude
model: claude-opus-5-5
image: quicbaas-software-factory-worker:0.11.0-alpha-cc2.1.280-cx0.156.0
```

An Agent always has a model. The claude Agents the boot migration seeds get
`claude-opus-5-5` (`orchestration.DefaultClaudeModel`, the coding tier). The
worker image's Claude Code must be new enough to know the model: Claude Code
2.1.278 rejects `claude-opus-5-5` as `unrecognized_model`, and 2.1.280 accepts
it.

The image (`deployments/agents/software-factory-worker/Dockerfile`) carries the
claude-code agent, **git**, **ssh-keygen** — git's `gpg.format=ssh` signing
invokes it, so a commit cannot be signed without it — **make**, a C toolchain
and the **Go toolchain** the projects pin, and no secret of any kind. The forge
token arrives as a read-only file under `/run/quicbaas/workspace`; the signing
key never arrives, only a socket to an agent that holds it.

**It carries the verification toolchain too**, because the binding's
verification commands run inside this container: **golangci-lint**, **gosec**
and **govulncheck**, which is what a Go project's `make lint`, `make check`
(fmt vet lint gosec vuln) and `make security` are made of. Without them
`make lint` exits non-zero on a clean tree — observed live as go-truststrap's
Makefile reporting *"golangci-lint is required but not installed"*; this
repository's own Makefile says *"golangci-lint not installed. Run: make
install-tools"* — so every review round fails and every Go job escalates
(go-quicbaas#772). They are installed at `$(go env GOPATH)/bin` — where a Go
Makefile actually looks for the linter, which is not the same as the `PATH` —
and at the versions this repository pins:

```
make build-worker-image  # passes GOLANGCI_VERSION / GOSEC_VERSION / GOVULNCHECK_VERSION
```

Those pins live in `pkg/version/toolchain.mk` — the repository's **only** copy,
which the Makefile includes and the daemon embeds (so its worker image refresh
builds with the same toolchain):
`make install-tools` installs them, CI installs them by reading them
(`make print-GOLANGCI_VERSION`), and the image bakes them. A worker linting with
a version CI does not use produces a review verdict the merge then contradicts.
`make test-worker-image` asserts the tools resolve where a Makefile looks, that
`golangci-lint` reports the pinned version, and that it lints a vendored module
clean and reports a finding on a bad one — with the container on `--network none`.

Every other place that installs these tools reads the same file. The
devcontainer and the integration-test image (`test/integration/Dockerfile`)
are built with the repository root as context, so each `COPY`s
`pkg/version/toolchain.mk` and installs golangci-lint (and, in the
devcontainer, the protoc plugins) at its pin; `sdk/go/Makefile` `include`s it,
installs the v2 module at the pin with `make tools`, and fails `make lint` when
the linter is absent rather than skipping it (go-quicbaas#391).
`make install-proto-tools` installs protoc from its checksum-verified release
zip into `PROTOC_PREFIX` (default `~/.local`) and the two Go plugins at their
pins, and `make proto-gen` runs `make check-proto-tools` first, which refuses
— naming each tool, the version found and the pin — unless `protoc --version`,
`protoc-gen-go --version` and `protoc-gen-go-grpc --version` all match
(go-quicbaas#481). The seal integration images build in a `go.work` written by
`test/integration/mkworkspace.sh`, whose `go` line is the image toolchain's
`go env GOVERSION`; `make check-workspace-script` (part of `make test`) fails if
a Dockerfile writes one inline again (go-quicbaas#399).

### CI stages in the devcontainer image

CI runs its stages in the project's own devcontainer image (handbook
`software/conventions/ci-standard.md`, `devcontainer.md`), built from
`build/docker/devcontainer/Dockerfile` at the commit under test. The `toolchain`
stage carries Go (`golang:<GO_VERSION>-trixie` by digest) and every pinned
tool: golangci-lint, gosec, govulncheck, actionlint, shellcheck and trivy. Each
pin, including the base image digest and the trivy and shellcheck SHA-256s,
comes from `pkg/version/toolchain.mk`, the same file the Makefile includes.

The `integration` stage is the integration stack's node image: `base` plus the
`quicbaasd` and `quicbaas` binaries, `test/integration/node-entrypoint.sh` and
the node config, with no source and no dev tools. The build-only
`integration-build` stage compiles the binaries from the source at the commit,
with the tags and flags `test/integration/Dockerfile.node` uses (`COVER=1` gives
a coverage-instrumented daemon). Its devcontainer compose service
(`build/docker/devcontainer/compose.yaml`) is hardened: a read-only filesystem
with a `/tmp` tmpfs, `network_mode: none`, every capability dropped,
`no-new-privileges`, and `memlock` and `core` ulimits.

The Makefile drives both images with the handbook's target names:

```
make docker-build                       # build the toolchain and integration images
make docker-run CMD='make build'        # build
make docker-run CMD='make test'         # unit tests
make docker-run CMD='make security'     # gosec, govulncheck, go mod verify, trivy fs
make docker-run CMD='make deps-check'   # latest-version check
make toolchain-image                    # print the toolchain image tag
```

`make vuln` fails on a reachable advisory unless `scripts/vuln-allowlist.txt`
lists its ID with a reason, and prints each allowed advisory with that reason.
An entry is only for an advisory with no fixed release, and an entry that
matches no reachable finding fails as stale, so it is removed once the fix
ships.

`make toolchain-image` prints a tag hashed from the Dockerfile, the pin file and
both modules' `go.mod`/`go.sum`. The integration image, which holds the source,
is tagged `integration-<commit>`, plus `-dirty-<hash>` of the uncommitted
changes when the tree is not clean, so any source change gives a new tag.
`make docker-run` builds the toolchain image
when that tag is missing, then runs `CMD` in the compose `toolchain` service as
the calling uid, with the source mounted at `/src`. The product image's own
targets are `make image-build`, `image-build-dev`, `image-build-fips`,
`image-run`, `image-run-fips`, `image-stop` and `image-push`.

The build mounts the empty `gomodauth` secret (see
[private-modules.md](../testing/private-modules.md)) and uses
`DEVCACHE_GOPROXY` when the caller sets it. Both images
run with `GOPROXY=off`.

`make integration-test` runs the integration stack
(`test/integration/docker-compose.integration.yml`) on the `integration` image.
It first builds the image with `make docker-build-integration-image`, then exports
the tag as `QUICBAAS_INTEGRATION_IMAGE` for compose. `make integration-bench`
and `make cluster-down-integration` export the tag too. What runs where:

- The node services (`test-node1` to `test-node5` and `test-selfreg`) run the
  `integration` image. Each node is read-only with a `/tmp` tmpfs, has every
  capability dropped, runs with `no-new-privileges`, and has the devcontainer's
  `memlock` and `core` ulimits. The nodes stay on the compose bridge network,
  because they talk to each other, and their data, log, socket and cert paths
  are writable named volumes. `test-node1` to `test-node5` add back
  `NET_ADMIN` and `NET_RAW` for multicast discovery. The nodes run as root, as
  they did under `Dockerfile.node`: the per-run node keys are root-owned with
  mode 0600, and a process with no capabilities can read only the files it owns.
- The test driver (`integration-test`) and `certs-init` still build
  `test/integration/Dockerfile` and are not hardened. The driver passes the
  node image tag on to the compose calls it makes through the Docker socket.
- The other compose files (`cluster`, `seal`, `kms-*`, `test`) still build
  `test/integration/Dockerfile.node`.

`make integration-test-forge-gitea` and `make integration-test-forge-github`
run the live forge suites against a scratch repository, outside the
`integration-test` aggregate. The GitHub suite needs `GITHUB_ITEST_TOKEN`,
`GITHUB_ITEST_OWNER`, `GITHUB_ITEST_REPO` and `GITHUB_ITEST_REVIEWER_TOKEN` (a
second account, since GitHub refuses self-approval), plus an optional
`GITHUB_ITEST_BASE_URL`. Each suite fails, naming every unset variable, rather
than skipping.

`make deps-check` fails when a direct requirement of `go.mod` or
`sdk/go/go.mod` is behind its latest release and has no comment on its
`require` or `replace` line giving the reason (handbook `devops.md` §9). It is
the one stage that needs a module proxy at run time: it sets
`DEPS_CHECK_GOPROXY` (the fleet cache if `DEVCACHE_GOPROXY` is set, then the
public proxy) and resolves `github.com/jeremyhahn/*` as private.

**No new `//nolint`.** Fix the cause the linter reports instead of suppressing
it. `nolintlint` (`.golangci.yml`) requires every directive to name its linter,
give a `// reason`, and still suppress something, and `make check-nolint` (part
of `make lint`) fails on any directive not listed as `path:linter` in
`scripts/nolint-allowlist.txt`, or on an entry whose directive is gone. An
exception is added to the allowlist only with the reviewer's agreement in the
PR (go-quicbaas#559).

**Trivy is deliberately absent.** It is the fourth member of `make security`,
but it downloads a vulnerability database on every run: in a worker sandbox
whose network policy is the operator's to decide, it would fail for the
environment rather than for the diff. A binding whose verification needs it must
name an image that carries it.

**It is built for the uid that will run it.** The spawn runs a sandboxed
session as the daemon's own uid:gid, and musl resolves users from `/etc/passwd`
alone — ssh-keygen calls `getpwuid` at start and exits without an entry, so an
image built for someone else's uid cannot sign anything. `make
build-worker-image` passes the building user's ids; building for a daemon that
runs as another user means overriding them:

```
make build-worker-image WORKER_IMAGE_UID=997 WORKER_IMAGE_GID=997
```

The image has **no meaningful entrypoint**, and does not need one: the spawn
clears it and runs the agent binary as the container's command, and the clone
is driven by the runner through the runtime's exec seam
(`pkg/sdlc/checkout.go`) rather than by anything in the image.

### The project side

The repository URL, default branch, commit author, commit signing key,
autonomy, the concurrency bound and the repositories the worker clones beside
the project (`context_repos`) are `Project` fields, set with
`quicbaas project create` / `update` (`--git-repo`, `--git-branch`,
`--git-signing` + `--git-signing-key`, `--autonomous`,
`--context-repo`, `--clear-context-repos`, `--max-concurrency`). Bind refuses a project without a signing policy or with a repository
outside `repo_hosts`. The repository half of that check is not only a bind-time
one: the forge access seam (`workspace.go`) repeats it every time a runner
opens a workspace, because the project record stays editable after the bind
and the allowlist can change under it. A refusal there is
`WorkspaceRepoRefusedError` — `ErrWorkspaceUnfit`, so the task is parked for
an operator with no attempt charged, naming the refused repository. It **turns the project autonomous** (`autonomous:
true`), and it neither reads nor sets `default_workflow` — that field belongs
to the lead-driven `workflow` runner, and an `sdlc` job has no workflow.

`context_repos` is what replaced the removed `companions`: the daemon puts
the list in the workspace spec it hands the container, and the runner clones
each entry beside the project checkout, inside that container. The spec's
shape and the checkout's obligations are in
[Autonomous projects](../orchestration/autonomy-and-dispatch.md).

# Web UI route audit

Every routed view, sidebar entry, SDK call and raw `fetch` in
`cmd/quicbaas/frontend/src`, checked against the routes the server registers
(`pkg/api/rest/routes.go`, `routes_iam.go`, `handlers_rag.go`). Line numbers
are 1-based. Paths are those the TypeScript SDK sends
(`sdk/typescript/src/rest.ts`), not the Go SDK's.

Status is one of `served`, `stubbed`, `not-configured`, `unregistered`,
`ui-placeholder`. A row marked `pending` has not been checked yet; see
[Audit progress](#audit-progress).

Trailing-slash note: `pkg/api/rest/server.go:614-620` registers a bare alias
for every pattern that ends in `/`. A call to `/api/v1/projects` is therefore
served by the `/api/v1/projects/` pattern. This applies to projects,
agent-accounts, tenants, users, roles, policies, kms/deks, secrets,
vector/namespaces and objects.

## Counts

Each count was taken with Serena `search_for_pattern` on
`cmd/quicbaas/frontend/src` (multiline off) unless stated.

| Count | Mine | Issue | Pattern / method |
|---|---|---|---|
| Routed views | 39 | 39 | `\{#if \|\{:else if ` with `routerStore.name ===` on `src/MainApp.svelte` (dispatch at :588-:1139, `ComingSoon` fall-through at :1144) |
| Sidebar entries | 38 | 40 | `implemented:` on `src/MainApp.svelte` |
| …of which `implemented: false` | 3 | 3 | same; nodes :270, backup :271, connection :277 |
| SDK call sites | 272 (254 via `getSDKClient`, 18 via standalone clients) | ~240 | see below |
| Distinct SDK methods | incomplete | 192 | not recomputed yet |
| Client families used | 30 (26 via `getSDKClient` + barrier, ceremony, tenantBarrier, bootstrap) | 26 | family names from the call-site pattern; `Client` interface `sdk/typescript/src/interfaces.ts:1327-1424` has 30 families, `RestClient` adds `bootstrap` (`rest.ts:5265`) |
| Standalone clients | 4 | 5 | `new Rest[A-Za-z]+\(\|create[A-Z][A-Za-z]*Client\(` |
| Raw `fetch` calls | 5 | 5 | `\bfetch\(` (`authFetch.ts` wraps `window.fetch`; not a call site) |

SDK call-site pattern:

```
\.(connection|orchestrator|auth|data|vectors|timeseries|objects|rag|embeddings|messaging|scheduler|projects|sdlc|accounts|agents|iam|kms|secrets|ca|trust|crypto|dek|dynamicSecrets|barrier|tenantBarrier|ceremony|bootstrap|cluster|seal|sshKeys|ssh)\.(keys\.|knownHosts\.|authorized\.)?[a-zA-Z]+\(
```

Five matches are not SDK calls and were removed: `WorkflowWizard.svelte:625`
(`agents.map`), `ObjectsView.svelte:68` (`resp.objects.filter`),
`SecretsView.svelte:102` (`resp.secrets.filter`), `TasksView.svelte:84`
(`referenceOptions.projects.map`), `DeployFleetNodeDialog.svelte:12` (comment).
Seven calls the pattern misses were added by hand: `ImportKeyModal.svelte:142`
(`ssh.keys.import_`), `SdlcReviewerPicker.svelte:60,63,86,87` and
`ProjectsView.svelte:359,373` (through `const sdlc = client.sdlc`). The 18
standalone-client calls are in CeremonyView (3), BarrierView (3),
TenantBarrierView (8) and BootstrapView (4).

Where mine differ from the issue:

- **Sidebar 38, not 40.** `implemented:` matches 38 entries; the 3 placeholders agree.
- **Call sites 272, not ~240.** The issue's figure left out the standalone
  clients and the `sdlc`/`ssh.keys` aliases.
- **Standalone clients 4, not 5.** Ceremony, Barrier, TenantBarrier and
  Bootstrap. The fifth in the issue is probably `installAuthFetch`, which wraps
  `fetch` and builds no client.

## Raw fetch calls

| Control (file:line) | Raw path | HTTP + route | Registered | Status |
|---|---|---|---|---|
| `src/lib/stores/auth.svelte.ts:1050` token refresh | `REFRESH_PATH` (:65) | POST `/api/v1/iam/refresh` | `routes_iam.go:273` | served |
| `src/views/CeremonyView.svelte:87` officer list | `/api/v1/init/officers` | GET `/api/v1/init/officers` | `routes.go:403` | not-configured (ceremony service before `quicbaasd init`; a 503 shows the notice at `CeremonyView.svelte:335`) |
| `src/views/SystemDashboard.svelte:113` liveness | `/api/v1/health/live` | GET | `routes.go:392` | served |
| `src/views/SystemDashboard.svelte:140` readiness | `/api/v1/health/ready` | GET | `routes.go:393` | served |
| `src/views/SystemDashboard.svelte:306` version | `/api/v1/version` | GET | `routes.go:685` | served |

## Sidebar placeholders

| Control (file:line) | SDK / path | Route | Registered | Status |
|---|---|---|---|---|
| Sidebar "Nodes" `src/MainApp.svelte:270` → `ComingSoon` | none | none (the Go SDK's cluster `Nodes` maps to POST `/api/v1/cluster/nodes`, served **no** per `docs/cluster/api.md:42`) | — | ui-placeholder |
| Sidebar "Backup" `src/MainApp.svelte:271` → `ComingSoon` | none | backups exist at `routes.go:765-770` but no view calls them | — | ui-placeholder |
| Sidebar "Connection" `src/MainApp.svelte:277` → `ComingSoon` | none | none (connection settings are client-local) | — | ui-placeholder |

## SDK calls

Prefix shorthands: `orch` = `/api/v1/orchestrator`, `fleet` = `/api/v1/fleet`.
Registered lines are in `pkg/api/rest/routes.go` unless another file is named.

### Orchestrator, fleet, agents, accounts, projects, SDLC

| Control (file:line) | SDK method | HTTP + route | Registered | Status |
|---|---|---|---|---|
| SessionList.svelte:99; sessionStore.ts:374,418; FleetNodeDetailView:51; orchestrator store:959 | orchestrator.listSessions / listSessionsPage | GET `orch/sessions` | :505 | served |
| SpawnDialog.svelte:178 | orchestrator.spawnSession | POST `orch/sessions` | :506 | served |
| sessionStore.ts:541 | orchestrator.closeSession | DELETE `orch/sessions/{id}` | :507 | served |
| sessionStore.ts:557 | orchestrator.getSessionHistory | GET `orch/sessions/{id}/history` | :510 | served |
| sessionStore.ts:731 | orchestrator.getSessionConversation | pending | pending | pending |
| sessionStore.ts:775 | orchestrator.subscribeSession | GET `orch/sessions/{id}/events` | :509 | served |
| SessionDetail.svelte:210; SpawnDialog:195; PromptComposer:87 | orchestrator.promptSession | POST `orch/sessions/{id}/prompt` | :511 | served |
| SessionDetail.svelte:173 | orchestrator.rotateSessionAccount | POST `orch/sessions/{id}/rotate-account` | :512 | pending (501 path when no rotation service not yet checked) |
| JobsView:106; FleetNodeDetailView:63; orchestrator store:927,1105 | orchestrator.listJobs / listJobsPage | GET `orch/jobs` | :498 | served |
| StartJobDialog.svelte:112 | orchestrator.startJob | POST `orch/jobs` | :499 | served |
| JobsView:183; JobDetailView:357 | orchestrator.cancelJob | DELETE `orch/jobs/{id}` | :500 | served |
| orchestrator store:1036 | orchestrator.getJobStatus | GET `orch/jobs/{id}` | :501 | served |
| job-events.ts:115 | orchestrator.subscribeJobEvents | GET `orch/jobs/{id}/events` | :503 | served |
| JobDetailView:249 | orchestrator.listRunners | GET `orch/runners` | :504 | served |
| TasksView:195; orchestrator store:975 | orchestrator.listTasks / listTasksPage | GET `orch/tasks` | :513 | served |
| CreateTaskDialog.svelte:98 | orchestrator.createTask | POST `orch/tasks` | :514 | served |
| TasksView:278 | orchestrator.decomposeTask | POST `orch/tasks/decompose` | :515 | served |
| TasksView:300 | orchestrator.claimTask | POST `orch/tasks/{id}/claim` | :521 | served |
| TasksView:320 | orchestrator.completeTask | POST `orch/tasks/{id}/complete` | :522 | served |
| JobDetailView:377 | orchestrator.readmitTask | POST `orch/tasks/{id}/readmit` | :524 | served |
| WorkflowsView:98,128; SystemDashboard:467; providers.ts:490 | orchestrator.listWorkflows / listWorkflowsPage | GET `orch/workflows` | :525 | served |
| WorkflowsView:264,328 | orchestrator.createWorkflow | POST `orch/workflows` | :526 | served |
| WorkflowsView:364 | orchestrator.deleteWorkflow | DELETE `orch/workflows/{name}` | :527 | served |
| WorkflowsView:255 | orchestrator.updateWorkflow | PUT `orch/workflows/{name}` | :529 | served |
| SpawnDialog:97; InferenceView:150; WorkflowsView:164; referenceOptions:117 | orchestrator.listBackends | GET `orch/backends` | :475 | served |
| InferenceView:181,222 | orchestrator.listBackendModels | GET `fleet/{node}/backends/{backend}/models` | :469 | served |
| InferenceView:281,297 | orchestrator.inferenceStream / inference | POST `orch/inference` | :497 | served |
| orchestrator store:989; SystemDashboard:226 | orchestrator.healthCheck | GET `orch/health` | :495 | served |
| attentionStore.ts:122 | orchestrator.listAttention | GET `orch/attention` | :473 | served |
| attentionStore.ts:151 | orchestrator.resolveAttention | POST `orch/attention/{id}/resolve` | :474 | served |
| CostView:160; JobDetailView:304 | orchestrator.getSpendReport | GET `orch/cost` | :476 | served |
| TenantSpendLimitsEditor:53,75 | orchestrator.get/setTenantSpendPolicy | GET/PUT `orch/spend-policy` | :477/:478 | served |
| CostSettingsPanel:56,79 | orchestrator.get/setCostSettings | GET/PUT `orch/cost-settings` | :479/:480 | served |
| fleetStore:148; CreateTaskDialog:45; InferenceView:194; WorkflowsView:165; ProjectsView:159; orchestrator store:943; referenceOptions:105; providers.ts:289 | orchestrator.listFleetNodes / listFleetNodesPage | GET `fleet` | :465 | served |
| RegisterFleetNodeDialog:85 | orchestrator.registerFleetNode | POST `fleet` | :466 | served |
| FleetList:185; FleetNodeDetailView:146 | orchestrator.removeFleetNode | DELETE `fleet/{id}` | :467 | served |
| FleetNodeDetailView:171 / :177 | orchestrator.enable/disableFleetNode | POST `fleet/{id}/enable` / `disable` | :471/:470 | served |
| DeployFleetNodeDialog:150 | orchestrator.deployFleetNode | pending | pending | pending |
| ProjectsView:540,564 | orchestrator.grant/revokeFleetNodeRole | pending | pending | pending |
| AgentSpendLimitsEditor:57,79; StartJobDialog:62 | agents.getAgent / updateAgent / listAgents | `/api/v1/agents…` | :482-487 | served |
| StartJobDialog:62 | agents.listPools | GET `/api/v1/agent-pools` | :488 | served |
| AccountsView:144; providers.ts:246 | accounts.listAccounts / listAccountsPage | GET `/api/v1/agent-accounts` | :290 | served |
| AccountsView:278 | accounts.createAccount | POST `/api/v1/agent-accounts` | :291 | served |
| AccountEditDialog:53; AccountsView:356,384,399,424,452 | accounts.update / rotate / setDefault / revoke / delete / getUsage | `/api/v1/agent-accounts/{id}…` | :292-301 | served |
| ProjectsView:158; TaskDetailView:156; providers.ts:319; referenceOptions:93 | projects.listProjects / listProjectsPage | GET `/api/v1/projects` | :538 | served |
| ProjectsView:218 / :243 / :269,492 | projects.create / delete / update | POST `/projects`, DELETE/PUT `/projects/{id}` | :539/:541/:543 | served |
| ProjectsView:373 | sdlc.forges | GET `/api/v1/sdlc/forges` | :595 | served |
| ProjectsView:359,399,417,436,497,503; SdlcReviewerPicker:60,63,86,87 | sdlc.binding / collaborators / fileIssue / conformance / bind / unbind / reviewer / setReviewer / clearReviewer | `/api/v1/sdlc/projects/{id}/…` | :596-609 | served |
| ProjectsView:460 | sdlc.onboard | POST `/api/v1/sdlc/repos/{repo}/onboard` | :600 | served |
| workerImage.ts:60,130 / :123 | orchestrator.getWorkerImageStatus / refreshWorkerImage | GET / POST `/api/v1/sdlc/worker-image[/refresh]` | :612/:613 | served |

### IAM, auth, tenants

| Control (file:line) | SDK method | HTTP + route | Registered | Status |
|---|---|---|---|---|
| LoginView:262 / :302 | auth.webAuthnBeginLogin / FinishLogin | POST `/api/v1/auth/login/begin` / `finish` | `routes_iam.go:436/437` | served |
| LoginView:333 | auth.loginPIV | POST `/api/v1/iam/login/piv` | `routes_iam.go:254` | served |
| RegisterView:106 / :147 | auth.registerBegin / registerFinish | POST `/api/v1/auth/register/begin` / `finish` | `routes_iam.go:438/439` | served |
| auth.svelte.ts:1225 | auth.authValidate | POST `/api/v1/iam/validate` | `routes_iam.go:301` | served |
| MainApp.svelte:467 | auth.logout | POST `/api/v1/iam/logout` | `routes_iam.go:302` | served |
| InviteActivateView:77 | iam.getInvite | GET `/api/v1/iam/{tenant}/invites/{token}` | `routes_iam.go:221` | served |
| InviteActivateView:151 | iam.activatePIV | POST `…/invites/{token}/activate/piv` | `routes_iam.go:224` | served |
| IAMView:109,347; MainApp:375; TenantBarrierView:172; providers.ts:349 | iam.listTenants | GET `/api/v1/tenants` | `routes_iam.go:99` | served |
| IAMView:547 | iam.createTenant | POST `/api/v1/tenants` | `routes_iam.go:100` | served |
| IAMView:581 / :601 | iam.updateTenant / deleteTenant | PUT / DELETE `/api/v1/tenants/{id}` | pending | pending |
| IAMView:318; providers.ts:385 / IAMView:435 | iam.listUsers / createUser | GET / POST `/api/v1/tenants/{t}/users` | `routes_iam.go:117/118` | served |
| IAMView:485 / :822 | iam.updateUser / deleteUser | PUT / DELETE `/api/v1/tenants/{t}/users/{u}` | pending | pending |
| IAMView:522 | iam.assignRoleToUser | POST `…/users/{u}/roles` | `routes_iam.go:122` | served |
| IAMView:363; providers.ts:417 / IAMView:632 | iam.listRoles / createRole | GET / POST `…/roles` | `routes_iam.go:187/188` | served |
| IAMView:713 | iam.reconcileRoles | POST `…/roles/reconcile` | `routes_iam.go:189` | served |
| IAMView:669 / :689 | iam.updateRole / deleteRole | PUT / DELETE `…/roles/{id}` | `routes_iam.go:191/192` | served |
| IAMView:382 / :747 | iam.listPolicies / createPolicy | GET / POST `…/policies` | `routes_iam.go:203/204` | served |
| IAMView:782 / :802 | iam.updatePolicy / deletePolicy | PUT / DELETE `…/policies/{name}` | `routes_iam.go:206/207` | served |

### Remaining families

Rows for data, vectors, timeseries, objects, RAG, embeddings, messaging,
scheduler, KMS, DEK, secrets, dynamic secrets, CA, trust, crypto, SSH, cluster,
barrier, tenant barrier, ceremony and bootstrap: **incomplete** (being added).

## Suspected gaps

| Suspect | Verdict | Evidence |
|---|---|---|
| DEK list/delete/safety "Served? no" (`docs/data-encryption-keys/api.md:39-47`) | cleared for the UI | those rows describe Go SDK paths; the TS SDK calls `/api/v1/kms/deks…`, registered `routes.go:410-416` with the bare alias from `server.go:614-620` |
| Vector CreateNamespace / ListNamespaces "no" (`docs/vector/api.md:44,56`) | cleared for the UI | TS paths `rest.ts:1597,1612` hit `routes.go:666/667` via the bare alias |
| Secrets list/get/store/delete/rotate "no" (`docs/secrets/api.md:41-51`) | cleared for the UI | TS paths `rest.ts:3622-3694` hit `routes.go:614-621` |
| CA GenerateCRL / IsRevoked "no" (`docs/certificate-authority/api.md:53,58`) | cleared for the UI | TS SDK uses `/api/v1/xkms/ca/crl` (`rest.ts:3944`) and `/api/v1/xkms/ca/revoked/{serial}` (`rest.ts:3964`), registered `routes.go:690/693` |
| Cluster Nodes / Rebalance "no" (`docs/cluster/api.md:42-43`) | cleared for the UI | ClusterDashboard calls only `cluster.status` (`ClusterDashboard.svelte:77`, `routes.go:364`); the Nodes sidebar entry is a placeholder |
| Liveness / readiness "no" (`docs/observability/api.md:39,41`) | cleared for the UI | the UI fetches `/api/v1/health/live` and `/ready`, `routes.go:392/393` |
| Ceremony officers | confirmed `not-configured` before init | `CeremonyView.svelte:100` maps 503 to the notice at :335 |
| rotateSessionAccount, messaging 501s, timeseries no-op build, tenant barrier routes, deployFleetNode | pending | — |

## Transports

incomplete — the per-transport `unsupportedClient` mapping (`sdk/typescript/src/unsupported.ts`, `factory.ts`) for REST, gRPC-web, MCP, QUIC, Unix and Wails is not written yet.

## Follow-ups

Proposed rows; none filed.

1. **Nodes view: replace the ComingSoon placeholder with a cluster-nodes view.** Caller: sidebar "Nodes" `MainApp.svelte:270`. Needs: a view over the served `/api/v1/cluster/nodes` routes (`routes.go:355-358`); capability gating is #2576.
2. **Backup view: replace the ComingSoon placeholder with a backups view.** Caller: sidebar "Backup" `MainApp.svelte:271`. Needs: a view and TS SDK methods over the served `/api/v1/admin/backups` routes (`routes.go:765-770`).
3. **Connection view: replace the ComingSoon placeholder with a connection-settings view.** Caller: sidebar "Connection" `MainApp.svelte:277`. Needs: a client-side view over the SDK's transport/address settings; no server route.
4. **Ceremony view: explain the pre-init state without a 503 round trip.** Caller: `CeremonyView.svelte:87`. Needs: nothing server-side; the 503 before `quicbaasd init` is by design. Record only.
5. **Audit rows still pending** (getSessionConversation, deployFleetNode, grant/revokeFleetNodeRole, tenant update/delete, user update/delete, the remaining families, transports, distinct-method count). Needs: this audit to continue.

## Audit progress

Commit 1 covers raw fetches, sidebar placeholders, orchestrator/fleet/agents/
accounts/projects/SDLC and IAM/auth. Rows marked `pending` or `incomplete` are
added in later commits.

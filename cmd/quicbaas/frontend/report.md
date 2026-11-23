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
| Routed views | 39 | 39 | `\{#if \|\{:else if ` with `routerStore.name ===` on `src/views/MainApp.svelte`; see [Routed views](#routed-views) |
| Sidebar entries | 38 | 40 | `implemented:` over the nav list `src/views/MainApp.svelte:195-277` |
| …of which `implemented: false` | 3 | 3 | same; nodes :269, backup :270, connection :276 |
| SDK call sites | 272 (254 via `getSDKClient`, 18 via standalone clients) | ~240 | see below |
| Distinct SDK methods | 218 | 192 | hand count of the distinct `family.method` names in the [SDK calls](#sdk-calls) tables; a name is counted once per family (`orchestrator.listJobs` and `scheduler.listJobs` are two), and `…Page` variants count separately |
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

- **Sidebar 38, not 40.** The nav list (`MainApp.svelte:195-277`) has 38
  entries, each with one `implemented:` flag; the 3 placeholders agree.
- **Call sites 272, not ~240.** The issue's figure left out the standalone
  clients and the `sdlc`/`ssh.keys` aliases.
- **Standalone clients 4, not 5.** Ceremony, Barrier, TenantBarrier and
  Bootstrap. The fifth in the issue is probably `installAuthFetch`, which wraps
  `fetch` and builds no client.
- **Distinct methods 218, not 192.** The issue's figure predates the
  standalone clients and the `…Page` variants.

`GET /api/v1/server/capabilities` (`routes.go:687`) has no UI caller yet; the
capability-gating follow-ups (#2576) will use it.

## Routed views

Counting method: the Grep tool on `src/views/MainApp.svelte` for
`\{#if \|\{:else if ` lines with `routerStore.name ===`. The dispatch runs from
`{#if routerStore.name === 'invite-activate'}` (:587) to
`{:else if routerStore.name === 'cluster'}` (:1136), with `ComingSoon` as the
fall-through at :1143. It holds 81 branches: 3 for the auth shell
(invite-activate :587, register :604, login :606; public routes, outside the
app shell), 39 for the breadcrumb (:657-:775), and 39 for the view switch
(:934-:1136). The routed views are the 39 view-switch branches, which matches
the issue's 39. Modals (the SSH key, deploy, spawn and task dialogs) are not
routes.

Status of the worst SDK call each view makes. On Wails, `bound` means the
family has a Wails client; `refuses` means the family is an
`unsupportedClient` and the call rejects with `TransportNotSupportedError` (a
TypeError on a `null` slot before this change, see [Wails](#wails-shipped-desktop-app));
`standalone REST` means the view builds its own REST client and does not use
the Wails client. Sidebar ids map to routes through `routeToViewID`
(`src/stores/navigation.svelte.ts:114`).

| # | Route | Component (file) | Sidebar entry | REST | Wails |
|---|---|---|---|---|---|
| 1 | dashboard | `SystemDashboard` (`src/views/SystemDashboard.svelte`) | dashboard | served | refuses (kms, scheduler, cluster) |
| 2 | attention | `AttentionView` (`src/views/orchestrator/AttentionView.svelte`) | attention | served | bound (orchestrator) |
| 3 | orchestration | `OrchestratorDashboard` (`src/views/orchestrator/OrchestratorDashboard.svelte`) | orchestration | served | bound (orchestrator) |
| 4 | jobs | `JobsView` (`src/views/orchestrator/JobsView.svelte`) | jobs | served | refuses (agents in StartJobDialog:62; projects in referenceOptions.ts:93) |
| 5 | job-detail | `JobDetailView` (`src/views/orchestrator/JobDetailView.svelte`) | none, linked from JobsView | served | bound (orchestrator) |
| 6 | fleet | `FleetList` (`src/components/orchestrator/FleetList.svelte`) | fleet | not-configured (deployFleetNode) | bound (orchestrator, sshKeys) |
| 7 | fleet-node | `FleetNodeDetailView` (`src/views/orchestrator/FleetNodeDetailView.svelte`) | none (highlights fleet), linked from FleetList | served | bound (orchestrator) |
| 8 | sessions | `SessionList` (`src/components/orchestrator/SessionList.svelte`) | sessions | served | refuses (projects in referenceOptions.ts:93) |
| 9 | session-detail | `SessionDetail` (`src/components/orchestrator/SessionDetail.svelte`) | none (highlights sessions), linked from SessionList | not-configured (rotateSessionAccount) | bound (orchestrator) |
| 10 | tasks | `TasksView` (`src/views/orchestrator/TasksView.svelte`) | tasks | served | refuses (projects in referenceOptions.ts:93) |
| 11 | task-detail | `TaskDetailView` (`src/views/orchestrator/TaskDetailView.svelte`) | none (highlights tasks), linked from TasksView | served | refuses (projects) |
| 12 | workflows | `WorkflowsView` (`src/views/orchestrator/WorkflowsView.svelte`) | workflows | served | refuses (agents in StartJobDialog:62; projects in referenceOptions.ts:93) |
| 13 | events | `EventStreamView` (`src/views/orchestrator/EventStreamView.svelte`) | events | served | bound (orchestrator) |
| 14 | cost | `CostView` (`src/views/orchestrator/CostView.svelte`) | costs | served | refuses (agents, AgentSpendLimitsEditor) |
| 15 | inference | `InferenceView` (`src/views/orchestrator/InferenceView.svelte`) | inference | served | bound (orchestrator) |
| 16 | rag | `RAGView` (`src/views/ai/RAGView.svelte`) | rag | served | refuses (rag) |
| 17 | embeddings | `EmbeddingsView` (`src/views/ai/EmbeddingsView.svelte`) | embeddings | served | refuses (embeddings) |
| 18 | messaging | `MessagingView` (`src/views/async/MessagingView.svelte`) | messaging | not-configured | refuses (messaging) |
| 19 | scheduler | `SchedulerView` (`src/views/async/SchedulerView.svelte`) | scheduler | served | refuses (scheduler) |
| 20 | kv | `KVStoreView` (`src/views/data/KVStoreView.svelte`) | kv | served | refuses (data) |
| 21 | vectors | `VectorsView` (`src/views/data/VectorsView.svelte`) | vectors | served | refuses (vectors) |
| 22 | timeseries | `TimeSeriesView` (`src/views/data/TimeSeriesView.svelte`) | timeseries | not-configured | refuses (timeseries) |
| 23 | objects | `ObjectsView` (`src/views/data/ObjectsView.svelte`) | objects | served | refuses (objects) |
| 24 | iam | `IAMView` (`src/views/security/IAMView.svelte`) | iam | served | refuses (iam) |
| 25 | kms | `KMSView` (`src/views/security/KMSView.svelte`) | kms | served | refuses (kms) |
| 26 | secrets | `SecretsView` (`src/views/security/SecretsView.svelte`) | secrets | served | refuses (secrets) |
| 27 | certificates | `CertificatesView` (`src/views/security/CertificatesView.svelte`) | certificates | served | refuses (ca) |
| 28 | trust | `TrustView` (`src/views/security/TrustView.svelte`) | trust | served | refuses (trust) |
| 29 | crypto | `CryptoView` (`src/views/security/CryptoView.svelte`) | crypto | served | refuses (crypto) |
| 30 | dek | `DEKView` (`src/views/security/DEKView.svelte`) | dek | served | refuses (dek) |
| 31 | dynamic-secrets | `DynamicSecretsView` (`src/views/security/DynamicSecretsView.svelte`) | dynamic-secrets | served | refuses (dynamicSecrets) |
| 32 | barrier | `BarrierView` (`src/views/security/BarrierView.svelte`) | barrier | served | standalone REST |
| 33 | tenant-barrier | `TenantBarrierView` (`src/views/security/TenantBarrierView.svelte`) | tenant-barrier | served | refuses (iam.listTenants :172; the barrier calls are standalone REST) |
| 34 | ceremony | `CeremonyView` (`src/views/security/CeremonyView.svelte`) | ceremony | not-configured before init | standalone REST |
| 35 | bootstrap | `BootstrapView` (`src/views/security/BootstrapView.svelte`) | bootstrap | served | standalone REST |
| 36 | ssh-keys | `SSHKeysView` (`src/views/security/SSHKeysView.svelte`) | ssh-keys | ui-placeholder (scope tabs not role-gated) | throws (ssh.knownHosts in KnownHostsPanel) |
| 37 | projects | `ProjectsView` (`src/views/orchestrator/ProjectsView.svelte`) | projects | served | refuses (projects, sdlc) |
| 38 | accounts | `AccountsView` (`src/views/orchestrator/AccountsView.svelte`) | accounts | served | refuses (accounts) |
| 39 | cluster | `ClusterDashboard` (`src/views/cluster/ClusterDashboard.svelte`) | cluster | served | refuses (cluster) |

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
| Sidebar "Nodes" `src/views/MainApp.svelte:269` → `ComingSoon` | none | none (the Go SDK's cluster `Nodes` maps to POST `/api/v1/cluster/nodes`, served **no** per `docs/cluster/api.md:42`) | — | ui-placeholder |
| Sidebar "Backup" `src/views/MainApp.svelte:270` → `ComingSoon` | none | backups exist at `routes.go:767-772` (`/api/v1/backups`) but no view calls them | — | ui-placeholder |
| Sidebar "Connection" `src/views/MainApp.svelte:276` → `ComingSoon` | none | none (connection settings are client-local) | — | ui-placeholder |

## SDK calls

Prefix shorthands: `orch` = `/api/v1/orchestrator`, `fleet` = `/api/v1/fleet`.
Registered lines are in `pkg/api/rest/routes.go` unless another file is named.

### Orchestrator, fleet, agents, accounts, projects, SDLC

| Control (file:line) | SDK method | HTTP + route | Registered | Status |
|---|---|---|---|---|
| SessionList.svelte:99; sessionStore.ts:374,418; FleetNodeDetailView:51; orchestrator store:959 | orchestrator.listSessions / listSessionsPage | GET `orch/sessions` | :505 | served |
| SpawnDialog.svelte:178 | orchestrator.spawnSession | POST `orch/sessions` | :506 | served |
| sessionStore.ts:541 | orchestrator.closeSession | DELETE `orch/sessions/{id}` | :507 | served |
| sessionStore.ts:557 | orchestrator.getSessionHistory | GET `orch/sessions/{id}/events` (JSON, `follow=0`; `rest.ts:1182`) | :509 | served |
| sessionStore.ts:731 | orchestrator.getSessionConversation | GET `orch/sessions/{id}/history` (`rest.ts:1211`) | :510 | served |
| sessionStore.ts:775 | orchestrator.subscribeSession | GET `orch/sessions/{id}/events` | :509 | served |
| SessionDetail.svelte:210; SpawnDialog:195; PromptComposer:87 | orchestrator.promptSession | POST `orch/sessions/{id}/prompt` | :511 | served |
| SessionDetail.svelte:173 | orchestrator.rotateSessionAccount | POST `orch/sessions/{id}/rotate-account` | :512 | not-configured when the agent service has no rotation service (`RotationServiceFrom` fails → 501, `handlers_orchestrator.go:332-334`); served otherwise |
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
| DeployFleetNodeDialog:150 | orchestrator.deployFleetNode | none: REST, gRPC-web, MCP, QUIC and Unix throw `DeployNotSupportedError` (`rest.ts:1179`, `grpc_web.ts:270`, `mcp.ts:496`, `quic.ts:279`, `unix.ts:210`) | — | not-configured (Wails transport only; deploy runs over SSH from the desktop app) |
| ProjectsView:540,564 | projects.grant/revokeFleetNodeRole | PUT / DELETE `/api/v1/projects/{id}/permissions/{node}` (`rest.ts:2708,2717`) | :545/:544 | served |
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
| IAMView:581 / :601 | iam.updateTenant / deleteTenant | PUT / DELETE `/api/v1/tenants/{id}` | `routes_iam.go:102/103` | served |
| IAMView:318; providers.ts:385 / IAMView:435 | iam.listUsers / createUser | GET / POST `/api/v1/tenants/{t}/users` | `routes_iam.go:117/118` | served |
| IAMView:485 / :822 | iam.updateUser / deleteUser | PUT / DELETE `/api/v1/tenants/{t}/users/{u}` | `routes_iam.go:120/121` | served |
| IAMView:522 | iam.assignRoleToUser | POST `…/users/{u}/roles` | `routes_iam.go:122` | served |
| IAMView:363; providers.ts:417 / IAMView:632 | iam.listRoles / createRole | GET / POST `…/roles` | `routes_iam.go:187/188` | served |
| IAMView:713 | iam.reconcileRoles | POST `…/roles/reconcile` | `routes_iam.go:189` | served |
| IAMView:669 / :689 | iam.updateRole / deleteRole | PUT / DELETE `…/roles/{id}` | `routes_iam.go:191/192` | served |
| IAMView:382 / :747 | iam.listPolicies / createPolicy | GET / POST `…/policies` | `routes_iam.go:203/204` | served |
| IAMView:782 / :802 | iam.updatePolicy / deletePolicy | PUT / DELETE `…/policies/{name}` | `routes_iam.go:206/207` | served |

### Tenant barrier (standalone `RestTenantBarrierClient`, `TenantBarrierView.svelte:76`)

| Control (file:line) | SDK method | HTTP + route | Registered | Status |
|---|---|---|---|---|
| TenantBarrierView:123,200,401 | tenantBarrierStatus | GET `/api/v1/tenants/{t}/barrier/status` | `routes_iam.go:113` | served |
| TenantBarrierView:128,238 | tenantBarrierInit | POST `…/barrier/init` | `routes_iam.go:109` | served |
| TenantBarrierView:136 | tenantBarrierUnsealShare | POST `…/barrier/unseal-share` | `routes_iam.go:111` | served |
| TenantBarrierView:272 | tenantBarrierUnseal | POST `…/barrier/unseal` | `routes_iam.go:110` | served |
| TenantBarrierView:279 | tenantBarrierSeal | POST `…/barrier/seal` | `routes_iam.go:112` | served |

### Data services

| Control (file:line) | SDK method | HTTP + route | Registered | Status |
|---|---|---|---|---|
| KVStoreView:70 | data.kvList | GET `/api/v1/kv/{type}/` | :442 | served |
| KVStoreView:122,166 | data.kvPut | POST/PUT `/api/v1/kv/{type}/{key}` | :445/:446 | served |
| KVStoreView:186 | data.kvDelete | DELETE `/api/v1/kv/{type}/{key}` | :443 | served |
| VectorsView:54 / :106 | vectors.listNamespaces / createNamespace | GET / POST `/api/v1/vector/namespaces` (`rest.ts:1597,1612`) | :666/:667 | served |
| VectorsView:133 | vectors.deleteNamespace | DELETE `/api/v1/vector/namespaces/{ns}` (`rest.ts:1624`) | :668 | served |
| VectorsView:169 | vectors.searchSimilar | POST `…/{ns}/search/similar` (`rest.ts:1635`) | :669-683 | served |
| TimeSeriesView:56 | timeseries.listMetrics | GET `/api/v1/timeseries/metrics` | :647 | not-configured without build tag `timeseries` (`timeseries_handlers_noop.go:1` `!timeseries` → 501 at :58) |
| TimeSeriesView:100 | timeseries.query | POST `/api/v1/timeseries/query` | :648 | not-configured without build tag `timeseries` (`timeseries_handlers_noop.go:38`) |
| TimeSeriesView:162 | timeseries.write | POST `/api/v1/timeseries/write` | :652 | not-configured without build tag `timeseries` (`timeseries_handlers_noop.go:28`) |
| TimeSeriesView:190 | timeseries.deleteSeries | DELETE `/api/v1/timeseries/series` | :651 | not-configured without build tag `timeseries` (`timeseries_handlers_noop.go:43`) |
| ObjectsView:65 / :129 / :151 / :177 | objects.list / put / get / delete | `/api/v1/objects/…` | :777-782 | served |
| ObjectsView:194; SystemDashboard:112,139,202,305; FleetList:123 | connection.getAddress / getTransport | none (client-local) | — | served (no request) |
| RAGView:58 / :194 | rag.query / buildContext | POST `/api/v1/rag/query` / `context` (`rest.ts:2200,2208`) | `handlers_rag.go:691-696` | served |
| RAGView:139 / :154 / :169 | rag.listDocuments / deleteDocument / reindexDocument | `/api/v1/rag/documents…`, `reindex` | `handlers_rag.go:691-696` | served |
| EmbeddingsView:65 / :141 | embeddings.listProviders / generate | GET `/api/v1/embedding/providers`, POST `generate` | :381/:380 | served |
| EmbeddingsView:169 / :184 | embeddings.setProviderStatus / setDefaultProvider | PUT `providers/{name}` / `providers/default` | :383/:382 | served |
| MessagingView:54 / :104 / :132 / :182 | messaging.listChannels / createChannel / deleteChannel / publish | `/api/v1/messaging/channels…`, `publish/{channel}` | :447-453 | not-configured when no messaging service is wired (`handlers_messaging.go:238-240` → 501 `MessagingServiceNotConfiguredError`); served otherwise |
| SchedulerView:54; SystemDashboard:291; providers.ts:458 | scheduler.listJobs | GET `/api/v1/scheduler/jobs` | :563 | served |
| SchedulerView:127 / :157 / :258 | scheduler.createJob / deleteJob / updateJob | POST / DELETE / PUT | :564/:565/:567 | served |
| SchedulerView:179 / :201 / :204 | scheduler.triggerJob / resumeJob / pauseJob | POST `…/{id}/trigger` / `resume` / `pause` | :572/:571/:570 | served |
| ClusterDashboard:77; SystemDashboard:435 | cluster.status | GET `/api/v1/cluster/status` (`rest.ts:4774`) | :364 | served |

### Security

| Control (file:line) | SDK method | HTTP + route | Registered | Status |
|---|---|---|---|---|
| KMSView:188; SystemDashboard:277; providers.ts:125 / KMSView:222 | kms.listKeys / createKey | GET / POST `/api/v1/kms/keys` | :418/:419 | served |
| KMSView:98 / :244 / :263 | kms.setDefaultKey / rotateKey / deleteKey | `/api/v1/kms/keys/{name}…` | :424/:432/:422 | served |
| KMSView:130 / :149 | kms.listKeyVersions / updateKeyStatus | GET `versions` / PUT `status` | :439/:438 | served |
| DEKView:95,153,183,213,237,273,294,325,373,388,408 | dek.list / create / status / rotate / setDefault / setSafety / clearSafety / delete / get / startReencryption / reencryptionProgress | `/api/v1/kms/deks[/…]` (`dek.ts:228-263`) | :410-416 | served |
| SecretsView:99; providers.ts:71 | secrets.listSecrets | GET `/api/v1/secrets?limit` (`rest.ts:3622`) | :617 | served |
| SecretsView:135,158,224; providers.ts:93,106 | secrets.store / get / delete | POST / GET / DELETE `/api/v1/secrets/{path}` | :618-621 | served |
| SecretsView:179 | secrets.rotateSecret | PUT/POST `/api/v1/secrets/{path}?action=rotate` (`rest.ts:3686`) | :618-621 | served |
| SecretsView:206 | secrets.listVersions | GET `/api/v1/secrets-versions/{path}` | :615 | served |
| DynamicSecretsView:101 / :183 / :212 | dynamicSecrets.listRoles / createRole / deleteRole | `/api/v1/secrets/dynamic/roles…` | :626-629 | served |
| DynamicSecretsView:132 / :282 / :307 | dynamicSecrets.listLeases / renewLease / revokeLease | `/api/v1/secrets/dynamic/leases…` | :623-625 | served |
| DynamicSecretsView:246 | dynamicSecrets.generateCredential | POST `…/credentials` | :622 | served |
| CertificatesView:103 / :360 | ca.caInfo / getCABundle | GET `/api/v1/certificate/ca/info` / `ca/bundle` | :340/:339 | served |
| CertificatesView:137; providers.ts:198 | ca.listCertificates | GET `ca/list` | :341 | served |
| CertificatesView:225 / :265 / :292 / :318 | ca.issue / signCSR / revoke / verify | POST `issue` / `sign-csr` / `revoke` / `verify` | :345/:347/:346/:349 | served |
| CertificatesView:341; providers.ts:223 | ca.isRevoked | GET `/api/v1/xkms/ca/revoked/{serial}` | :695 | served |
| CertificatesView:374 | ca.generateCRL | POST `/api/v1/xkms/ca/crl` | :692 | served |
| TrustView:50 / :98 / :129 | trust.crossSignedRootPool / autoImportAIA / publishDANE | `/api/v1/trust/…` | :656/:655/:657 | served |
| CryptoView:96,121,151,179,209,229,255 | crypto.encrypt / decrypt / sign / verify / hash / random / dataKey | `/api/v1/crypto/…` | :367-376 | served |
| SSHKeysView:121; providers.ts:159 | ssh.keys.listScoped | GET `/api/v1/ssh/keys?scope=&owner=` (`rest.ts:5020-5026`) | :636 | served |
| SSHKeysView:159 | ssh.keys.deleteScoped | DELETE `/api/v1/ssh/keys/{scope}/{owner}/{name}` | :639 | served |
| SSHKeysView:69-73 scope tabs; :104-109 owner | none (the tabs choose `scope`/`owner` for listScoped) | — | — | ui-placeholder: the three tabs are hard-coded `enabled: true` (comment :65-68: role gating waits on an auth store that exposes roles), and scope `user` sends the tenant ID as the owner (:106-108) |
| GenerateKeyModal:102 / ImportKeyModal:142 | ssh.keys.create / import_ | POST `/api/v1/ssh/keys` / `keys/import` | :637/:638 | served |
| DeployFleetNodeDialog:85 | sshKeys.list | GET `/api/v1/ssh/keys` | :636 | served |
| KnownHostsPanel:90 / :134 / :161 | ssh.knownHosts.list / add / remove | GET / POST / DELETE `/api/v1/ssh/known-hosts` | :643/:644/:645 | served |

### Barrier, ceremony, bootstrap (standalone clients)

| Control (file:line) | SDK method | HTTP + route | Registered | Status |
|---|---|---|---|---|
| BarrierView:137 | barrier.status | GET `/api/v1/barrier/status` | :325 | served |
| BarrierView:141 | barrier.initializeShamir | POST `/api/v1/barrier/shamir/initialize` | :322 | served |
| BarrierView:153 | barrier.unsealWithShare | POST `/api/v1/barrier/shamir/unseal-share` | :323 | served |
| CeremonyView:124 | ceremony.claimCertComplete | POST `/api/v1/init/claim-cert/complete` | :398 | not-configured before `quicbaasd init` (same gate as the officers fetch) |
| CeremonyView:141 | ceremony.claimShareComplete | POST `/api/v1/init/officer-share/complete` | :402 | not-configured before init |
| CeremonyView:154 | ceremony.enrollExternalOfficer | POST `/api/v1/init/enroll-external-officer` | :399 | not-configured before init |
| BootstrapView:100 / :103 / :106 / :113 | bootstrap.spki / dane / noise / auto | `/api/v1/bootstrap/…` | :330-333 | served |

## Suspected gaps

| Suspect | Verdict | Evidence |
|---|---|---|
| DEK list/delete/safety "Served? no" (`docs/data-encryption-keys/api.md:39-47`) | cleared for the UI | those rows describe Go SDK paths; the TS SDK calls `/api/v1/kms/deks…`, registered `routes.go:410-416` with the bare alias from `server.go:614-620` |
| Vector CreateNamespace / ListNamespaces "no" (`docs/vector/api.md:44,56`) | cleared for the UI | TS paths `rest.ts:1597,1612` hit `routes.go:666/667` via the bare alias |
| Secrets list/get/store/delete/rotate "no" (`docs/secrets/api.md:41-51`) | cleared for the UI | TS paths `rest.ts:3622-3694` hit `routes.go:614-621` |
| CA GenerateCRL / IsRevoked "no" (`docs/certificate-authority/api.md:53,58`) | cleared for the UI | TS SDK uses `/api/v1/xkms/ca/crl` (`rest.ts:3944`) and `/api/v1/xkms/ca/revoked/{serial}` (`rest.ts:3964`), registered `routes.go:692/695` |
| Cluster Nodes / Rebalance "no" (`docs/cluster/api.md:42-43`) | cleared for the UI | ClusterDashboard calls only `cluster.status` (`ClusterDashboard.svelte:77`, `routes.go:364`); the Nodes sidebar entry is a placeholder |
| Liveness / readiness "no" (`docs/observability/api.md:39,41`) | cleared for the UI | the UI fetches `/api/v1/health/live` and `/ready`, `routes.go:392/393` |
| Ceremony officers | confirmed `not-configured` before init | `CeremonyView.svelte:100` maps 503 to the notice at :335 |
| Tenant barrier routes | cleared | all five calls registered at `routes_iam.go:109-113` |
| deployFleetNode | confirmed `not-configured` off Wails | every non-Wails transport throws `DeployNotSupportedError` (`rest.ts:1179`) |
| rotateSessionAccount | confirmed `not-configured` without a rotation service | `handlers_orchestrator.go:332-334` returns 501 |
| Messaging | confirmed `not-configured` without a messaging service | `handlers_messaging.go:238-240` returns 501 |
| Time series | confirmed `not-configured` without build tag `timeseries` | `timeseries_handlers_noop.go:1` (`rest && !timeseries`) stubs every handler with 501 |
| SSH key scope tabs | confirmed `ui-placeholder` | `SSHKeysView.svelte:69-73` enables all three tabs for every user; :104-109 uses the tenant ID as the `user` owner (Follow-up 10) |
| Served stubs no control calls | confirmed `stubbed`, caller none | PUT `/api/v1/vector/namespaces/{namespace}` (`routes.go:670`, handler `vector_handlers.go:158`, always 501 at :188); the xkms PIN and lockout routes `routes.go:722-729`, eight handlers in `handlers_xkms.go:1491-1679`, each 501. Record only |

## Transports

The UI builds two transports only: `src/stores/sdkClient.ts:71` creates the
client with `transport: 'wails'` when `isWailsRuntime()` (the desktop app),
and :75 creates `transport: 'rest'` against `window.location.origin` (the
browser). gRPC-web, MCP, QUIC and Unix are SDK transports the UI does not
ship; they are listed for completeness.

Pattern: `unsupportedClient|: any = null;` on `sdk/typescript/src`.
`unsupportedClient` (`sdk/typescript/src/unsupported.ts:24`) returns a client
whose every method rejects with `TransportNotSupportedError`. A `null` family
slot instead fails with "Cannot read properties of null" at the first call.
`factory.ts` only picks the class. Separately, `deployFleetNode` throws
`DeployNotSupportedError` on every transport but Wails.

### REST (shipped: browser)

No family maps to `unsupportedClient` and none is `null`. `deployFleetNode`
throws (`rest.ts:1179`): DeployFleetNodeDialog:150.

### Wails (shipped: desktop app)

The desktop app binds Go structs for connection, orchestrator, auth and SSH
keys only. `projects`, `sdlc` and `accounts` were already `unsupportedClient`.
The other 22 families were `readonly X: any = null`; after this change each is
an `unsupportedClient` (`wails.ts:966-990`), so a call rejects with
`TransportNotSupportedError` (transport `wails`, reason "no Wails binding")
instead of a TypeError. The views still cannot use them on the desktop app
(Follow-up 11). UI callers per family, from the SDK calls tables:

| Family | UI callers |
|---|---|
| data | KVStoreView |
| vectors | VectorsView |
| timeseries | TimeSeriesView |
| objects | ObjectsView |
| rag | RAGView |
| embeddings | EmbeddingsView |
| messaging | MessagingView |
| scheduler | SchedulerView, SystemDashboard:291, providers.ts:458 |
| projects | ProjectsView, TaskDetailView:156, providers.ts:319, referenceOptions.ts:93 (SessionList, JobsView, TasksView, WorkflowsView) |
| sdlc | ProjectsView, SdlcReviewerPicker |
| accounts | AccountsView, AccountEditDialog:53, providers.ts:246 |
| agents | AgentSpendLimitsEditor:57,79 (CostView), StartJobDialog:62 (JobsView, WorkflowsView, SystemDashboard) |
| iam | IAMView, MainApp.svelte:375, TenantBarrierView:172, InviteActivateView:77,151, providers.ts:349,385,417 |
| kms | KMSView, SystemDashboard:277, providers.ts:125 |
| secrets | SecretsView, providers.ts:71,93,106 |
| ca | CertificatesView, providers.ts:198,223 |
| trust | TrustView |
| crypto | CryptoView |
| dek | DEKView |
| dynamicSecrets | DynamicSecretsView |
| barrier, tenantBarrier, ceremony | none through the shared client; BarrierView, TenantBarrierView and CeremonyView build standalone REST clients |
| cluster | ClusterDashboard:77, SystemDashboard:435 |
| seal | none |

`ssh.knownHosts` and `ssh.authorized` throw on Wails (`WailsSSHClient`), so
KnownHostsPanel (SSHKeysView:357) fails there. Wails is the only transport
that implements `deployFleetNode`.

### gRPC-web (not shipped by the UI)

`projects`, `sdlc`, `accounts` are `unsupportedClient` (`grpc_web.ts:363-365`).
Still `null` (`grpc_web.ts:348-397`): data, vectors, timeseries, objects, rag,
embeddings, messaging, scheduler, agents, iam, kms, secrets, ca, trust,
crypto, dek, dynamicSecrets, tenantBarrier, cluster, seal, sshKeys, ssh.
Deploy throws (`grpc_web.ts:271`).

### MCP (not shipped by the UI)

No family maps to `unsupportedClient`. Still `null` (`mcp.ts:1310-1359`): data,
vectors, timeseries, objects, rag, embeddings, messaging, scheduler, agents,
iam, kms, secrets, ca, trust, crypto, dek, dynamicSecrets, tenantBarrier,
cluster, seal, sshKeys, ssh. Deploy throws (`mcp.ts:497`).

### QUIC (not shipped by the UI)

`projects`, `sdlc`, `accounts` are `unsupportedClient` (`quic.ts:368-370`).
Still `null` (`quic.ts:353-402`): the same 22 slots as gRPC-web. Deploy throws
(`quic.ts:280`).

### Unix (not shipped by the UI)

`projects`, `sdlc`, `accounts` are `unsupportedClient` (`unix.ts:266-268`).
Still `null` (`unix.ts:251-301`): the gRPC-web list plus barrier and ceremony.
Deploy throws (`unix.ts:211`).

## Follow-ups

Proposed rows; none filed.

1. **Nodes view: replace the ComingSoon placeholder with a cluster-nodes view.** Caller: sidebar "Nodes" `src/views/MainApp.svelte:269`. Needs: a view over the served `/api/v1/cluster/nodes` routes (`routes.go:355-358`); capability gating is #2576.
2. **Backup view: replace the ComingSoon placeholder with a backups view.** Caller: sidebar "Backup" `src/views/MainApp.svelte:270`. Needs: a view and TS SDK methods over the served `/api/v1/backups` routes (`routes.go:767-772`).
3. **Connection view: replace the ComingSoon placeholder with a connection-settings view.** Caller: sidebar "Connection" `src/views/MainApp.svelte:276`. Needs: a client-side view over the SDK's transport/address settings; no server route.
4. **Ceremony view: explain the pre-init state without a 503 round trip.** Caller: `CeremonyView.svelte:87`. Needs: nothing server-side; the 503 before `quicbaasd init` is by design. Record only.
5. **Deploy dialog: hide or explain Deploy when the transport is not Wails.** Caller: `DeployFleetNodeDialog.svelte:150`. Needs: a UI-side transport check (capability gating, #2576); no server route.
6. **Sessions: explain account rotation when the server has no rotation service.** Caller: `SessionDetail.svelte:173`. Needs: the view to show the 501 as "not configured" (capability gating, #2576).
7. **Messaging view: show "not configured" when the server has no messaging service.** Callers: `MessagingView.svelte:54,104,132,182`. Needs: capability gating (#2576).
8. **Time series view: show "not enabled" on a build without the `timeseries` tag.** Callers: `TimeSeriesView.svelte:56,100,162,190`. Needs: capability gating (#2576).
9. **Projects, SDLC and accounts views: say the family is unavailable on gRPC-web, QUIC, Unix and Wails.** Callers: the views listed under Transports. Needs: capability gating (#2576); the server routes are served.
10. **SSH keys: role-gate the scope tabs and use the principal's uid as the `user` owner.** Callers: `SSHKeysView.svelte:69-73` (tabs), :104-109 (owner). Needs: an auth store that exposes the principal's roles and uid, and an owner decision on which roles gate which scope.
11. **Desktop SPA: route the families the Wails binding does not carry to REST against the embedded server.** Callers: every view listed under [Wails](#wails-shipped-desktop-app). Needs: `sdkClient.ts` to build a REST client for those families in desktop mode; the server routes are served.
12. **gRPC-web/MCP/QUIC/Unix: replace the remaining `null` family slots with `unsupportedClient`.** Callers: none in the UI (it does not ship these transports). Needs: the SDK change made here for Wails, per transport.

## Audit progress

Every raw fetch, sidebar entry, routed view and SDK call row is audited; no
row is `pending` and no count is `incomplete`.

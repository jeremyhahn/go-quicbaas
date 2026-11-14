# Console coverage matrix: desktop bindings and IAM routes

This file records, for the desktop app's Go bindings (`cmd/quicbaas/bindings_*.go`) and the IAM REST routes registered in `pkg/api/rest/routes_iam.go`, whether the console (`cmd/quicbaas/frontend/src`) reaches them. The frontend never calls a Wails binding by name: it calls the `@quicbaas/sdk` client, and on the `wails` transport `sdk/typescript/src/wails.ts` forwards each SDK method to one binding method (`window.go.main.<Type>.<Method>`). On the `rest` transport the same SDK methods build the REST paths in `sdk/typescript/src/rest.ts`. Each mark is decided by tracing that chain. A binding is `covered: <file>` when a frontend file calls the SDK method that wails.ts forwards to it, and `unused` when no SDK method forwards to it or no frontend file calls that SDK method. A route is `covered: <file>` when an SDK method builds its path and a frontend file calls that method. It is `not covered` when there is no such SDK method or no frontend caller. It is `not-UI: <reason>` when the route is not meant for a console. The SDK file column is the coordinator's path match, which is a hint and not proof. Where several frontend files call a method, one is named. No row is marked `covered` without naming the calling file.

## Desktop bindings (cmd/quicbaas/bindings_*.go)

### AgentCatalogBinding

No SDK transport forwards to this binding: `wails.ts` has no agent-catalog proxy, and the console's agent views go through `RestAgentsClient`.

| Method | File | Console |
|---|---|---|
| ListAgents | cmd/quicbaas/bindings_agent_catalog.go | unused |
| GetAgent | cmd/quicbaas/bindings_agent_catalog.go | unused |
| CreateAgent | cmd/quicbaas/bindings_agent_catalog.go | unused |
| UpdateAgent | cmd/quicbaas/bindings_agent_catalog.go | unused |
| DeleteAgent | cmd/quicbaas/bindings_agent_catalog.go | unused |
| AgentUsedBy | cmd/quicbaas/bindings_agent_catalog.go | unused |
| ListPools | cmd/quicbaas/bindings_agent_catalog.go | unused |
| GetPool | cmd/quicbaas/bindings_agent_catalog.go | unused |
| CreatePool | cmd/quicbaas/bindings_agent_catalog.go | unused |
| UpdatePool | cmd/quicbaas/bindings_agent_catalog.go | unused |
| DeletePool | cmd/quicbaas/bindings_agent_catalog.go | unused |
| PoolUsedBy | cmd/quicbaas/bindings_agent_catalog.go | unused |

### ConnectionBinding

`WailsConnectionClient.connect`/`disconnect` forward to Connect/Disconnect, but no frontend file calls `client.connection.connect` or `disconnect`. `isConnected`, `getAddress` and `getTransport` answer from SDK-side state and never reach the binding.

| Method | File | Console |
|---|---|---|
| Connect | cmd/quicbaas/bindings_connection.go | unused |
| Disconnect | cmd/quicbaas/bindings_connection.go | unused |
| IsConnected | cmd/quicbaas/bindings_connection.go | unused |
| GetAddress | cmd/quicbaas/bindings_connection.go | unused |
| GetTransport | cmd/quicbaas/bindings_connection.go | unused |

### EmbeddedServerBinding

There is no SDK proxy for this binding.

| Method | File | Console |
|---|---|---|
| Start | cmd/quicbaas/bindings_embedded.go | unused |
| Stop | cmd/quicbaas/bindings_embedded.go | unused |
| IsRunning | cmd/quicbaas/bindings_embedded.go | unused |
| ConnectionURL | cmd/quicbaas/bindings_embedded.go | unused |

### FleetBinding

There is no SDK proxy for this binding. The console's fleet views reach the fleet through `OrchestratorBinding.ListFleetNodes`.

| Method | File | Console |
|---|---|---|
| List | cmd/quicbaas/bindings_fleet.go | unused |
| Get | cmd/quicbaas/bindings_fleet.go | unused |
| Status | cmd/quicbaas/bindings_fleet.go | unused |
| Heartbeat | cmd/quicbaas/bindings_fleet.go | unused |

### FleetDeployBinding and orchestratorFleetNodeListerAdapter

| Method | File | Console |
|---|---|---|
| FleetDeployBinding.Deploy | cmd/quicbaas/bindings_fleet_deploy.go | covered: cmd/quicbaas/frontend/src/components/orchestrator/DeployFleetNodeDialog.svelte |
| orchestratorFleetNodeListerAdapter.ListFleetNodes | cmd/quicbaas/bindings_fleet_deploy.go | unused |

The adapter's method is not bound to Wails. Go code calls it from inside the deploy binding.

### OrchestratorBinding

| Method | File | Console |
|---|---|---|
| GetCostSettings | cmd/quicbaas/bindings_cost_settings.go | covered: cmd/quicbaas/frontend/src/components/orchestrator/CostSettingsPanel.svelte |
| SetCostSettings | cmd/quicbaas/bindings_cost_settings.go | covered: cmd/quicbaas/frontend/src/components/orchestrator/CostSettingsPanel.svelte |
| ListJobs | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/stores/orchestrator.svelte.ts |
| GetJobStatus | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/stores/orchestrator.svelte.ts |
| StartJob | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/components/orchestrator/StartJobDialog.svelte |
| CancelJob | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/views/orchestrator/JobsView.svelte |
| ReadmitTask | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/views/orchestrator/JobDetailView.svelte |
| ListFleetNodes | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/stores/fleetStore.svelte.ts |
| GetFleetNode | cmd/quicbaas/bindings_orchestrator.go | unused |
| ListSessions | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/stores/sessionStore.svelte.ts |
| ListTasks | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/stores/orchestrator.svelte.ts |
| AttentionList | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/stores/attentionStore.svelte.ts |
| AttentionResolve | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/stores/attentionStore.svelte.ts |
| GetTask | cmd/quicbaas/bindings_orchestrator.go | unused |
| ListBackends | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/views/orchestrator/InferenceView.svelte |
| ListRunners | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/views/orchestrator/JobDetailView.svelte |
| ListBackendModels | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/views/orchestrator/InferenceView.svelte |
| HealthCheck | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/views/SystemDashboard.svelte |
| Inference | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/views/orchestrator/InferenceView.svelte |
| SubscribeJobEvents | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/stores/job-events.svelte.ts |
| GetSpendReport | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/views/orchestrator/CostView.svelte |
| GetTenantSpendPolicy | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/components/orchestrator/TenantSpendLimitsEditor.svelte |
| SetTenantSpendPolicy | cmd/quicbaas/bindings_orchestrator.go | covered: cmd/quicbaas/frontend/src/components/orchestrator/TenantSpendLimitsEditor.svelte |
| GetWorkerImageStatus | cmd/quicbaas/bindings_worker_image.go | covered: cmd/quicbaas/frontend/src/stores/workerImage.svelte.ts |
| RefreshWorkerImage | cmd/quicbaas/bindings_worker_image.go | covered: cmd/quicbaas/frontend/src/stores/workerImage.svelte.ts |

`wails.ts` wraps GetFleetNode (`getFleetNode`) and GetTask (`getTask`), but no frontend file calls either SDK method.

### SessionBinding

| Method | File | Console |
|---|---|---|
| Spawn | cmd/quicbaas/bindings_sessions.go | covered: cmd/quicbaas/frontend/src/components/orchestrator/SpawnDialog.svelte |
| Prompt | cmd/quicbaas/bindings_sessions.go | covered: cmd/quicbaas/frontend/src/components/orchestrator/PromptComposer.svelte |
| Close | cmd/quicbaas/bindings_sessions.go | covered: cmd/quicbaas/frontend/src/stores/sessionStore.svelte.ts |
| List | cmd/quicbaas/bindings_sessions.go | unused |
| Get | cmd/quicbaas/bindings_sessions.go | unused |
| GetHistory | cmd/quicbaas/bindings_sessions.go | covered: cmd/quicbaas/frontend/src/stores/sessionStore.svelte.ts |
| GetConversation | cmd/quicbaas/bindings_sessions.go | covered: cmd/quicbaas/frontend/src/stores/sessionStore.svelte.ts |
| Subscribe | cmd/quicbaas/bindings_sessions.go | covered: cmd/quicbaas/frontend/src/stores/sessionStore.svelte.ts |
| Unsubscribe | cmd/quicbaas/bindings_sessions.go | unused |

Session listing goes through `OrchestratorBinding.ListSessions`. `subscribeSession` follows live events with `window.runtime.EventsOn` and stops by unsubscribing locally, so it never calls `Unsubscribe`.

### SSHKeysBinding

| Method | File | Console |
|---|---|---|
| List | cmd/quicbaas/bindings_ssh.go | covered: cmd/quicbaas/frontend/src/components/orchestrator/DeployFleetNodeDialog.svelte |
| Create | cmd/quicbaas/bindings_ssh.go | covered: cmd/quicbaas/frontend/src/views/security/ssh/GenerateKeyModal.svelte |
| Get | cmd/quicbaas/bindings_ssh.go | unused |
| Delete | cmd/quicbaas/bindings_ssh.go | unused |

`get` and `remove` forward to Get and Delete, but no frontend file calls them. SSHKeysView uses `listScoped`/`deleteScoped`, and on Wails those throw "not available".

## IAM routes (pkg/api/rest/routes_iam.go)

### Tenants (/api/v1/tenants)

SDK methods are on `RestIAMClient` (`client.iam`) in rest.ts.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | /api/v1/tenants/ | ListTenants | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| POST | /api/v1/tenants/ | CreateTenant | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| GET | /api/v1/tenants/{tenant_id} | GetTenant | interfaces.ts,rest.ts,types.ts | not covered |
| PUT | /api/v1/tenants/{tenant_id} | UpdateTenant | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| DELETE | /api/v1/tenants/{tenant_id} | DeleteTenant | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| PATCH | /api/v1/tenants/{tenant_id}/quotas | UpdateTenantEngineQuotas | - | not covered |
| PATCH | /api/v1/tenants/{tenant_id}/entitlements | UpdateTenantEngineEntitlements | - | not covered |
| POST | /api/v1/tenants/{tenant_id}/provision | ProvisionTenant | interfaces.ts,types.ts | not covered |

GetTenant and ProvisionTenant have no `RestIAMClient` method.

### Users (/api/v1/tenants/{tenant_id}/users, and the per-user subroutes)

The paths below `{user_id}` are relative to `/api/v1/tenants/{tenant_id}/users/{user_id}`. The coordinator's table leaves the pattern of the three `{user_id}` rows blank. The SDK builds them as `/users/{user_id}`.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | /api/v1/tenants/{tenant_id}/users/ | ListUsers | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| POST | /api/v1/tenants/{tenant_id}/users/ | CreateUser | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| GET | {user_id} (blank in source) | GetUser | - | not covered |
| PUT | {user_id} (blank in source) | UpdateUser | - | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| DELETE | {user_id} (blank in source) | DeleteUser | - | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| POST | /roles | AssignRole | dynamicsecrets.ts,interfaces.ts,rest.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| DELETE | /roles/{role} | RevokeRole | dynamicsecrets.ts,interfaces.ts,rest.ts | not covered |
| GET | /sessions/ | ListUserSessions | rest.ts | not covered |
| POST | /sessions/ | CreateSession | rest.ts | not covered |
| DELETE | /sessions/ | RevokeAllUserSessions | rest.ts | not covered |
| POST | /gdpr/consent | GrantConsent | - | not covered |
| GET | /gdpr/consents | ListUserConsents | - | not covered |
| GET | /gdpr/consent/{purpose} | GetConsent | - | not covered |
| PUT | /gdpr/consent/{purpose}/withdraw | WithdrawConsent | - | not covered |
| DELETE | /gdpr/consent/{purpose} | WithdrawConsent | - | not covered |
| POST | /gdpr/export | RequestDataExport | - | not covered |
| GET | /gdpr/export/{export_id} | GetExportStatus | - | not covered |
| GET | /gdpr/export/{export_id}/download | DownloadExport | - | not covered |
| POST | /gdpr/deletion | RequestDeletion | - | not covered |
| GET | /gdpr/deletion/{deletion_id} | GetDeletionStatus | - | not covered |
| DELETE | /gdpr/deletion/{deletion_id} | CancelDeletion | - | not covered |
| GET | /mfa/ | GetMFAStatus | - | not covered |
| DELETE | /mfa/ | DisableMFA | - | not covered |
| POST | /mfa/enroll | BeginMFAEnrollment | - | not covered |
| POST | /mfa/verify | CompleteMFAEnrollment | - | not covered |
| DELETE | /mfa/enroll/{enrollment_id} | CancelMFAEnrollment | - | not covered |
| POST | /mfa/challenge | BeginMFAChallenge | - | not covered |
| POST | /mfa/verify-challenge | VerifyMFAChallenge | - | not covered |
| GET | /mfa/backup-codes | GetBackupCodeStatus | - | not covered |
| POST | /mfa/backup-codes | GenerateBackupCodes | - | not covered |
| GET | /webauthn/credentials/ | ListUserWebAuthnCredentials | - | not covered |
| DELETE | /webauthn/credentials/ | DeleteUserWebAuthnCredentials | - | not covered |
| DELETE | /webauthn/credentials/{credential_id} | DeleteUserWebAuthnCredential | - | not covered |
| POST | /links/ | LinkAccount | - | not covered |
| GET | /links/ | GetUserLinks | - | not covered |
| DELETE | /links/{provider_id} | UnlinkAccount | - | not covered |

UpdateUser and DeleteUser are `iam.updateUser`/`iam.deleteUser`. AssignRole is `iam.assignRoleToUser`. RevokeRole is `iam.revokeRoleFromUser`, which no frontend file calls. The dynamicsecrets.ts match is a different `/roles` path. The `rest.ts` matches for `/sessions/` are orchestrator session paths, not the user-session subroute. No SDK method builds that subroute.

### Roles (/api/v1/tenants/{tenant_id}/roles)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | /api/v1/tenants/{tenant_id}/roles/ | ListRoles | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| POST | /api/v1/tenants/{tenant_id}/roles/ | CreateRole | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| POST | /api/v1/tenants/{tenant_id}/roles/reconcile | ReconcileRoles | interfaces.ts,rest.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| GET | /api/v1/tenants/{tenant_id}/roles/{role_id} | GetRole | interfaces.ts,rest.ts,types.ts | not covered |
| PUT | /api/v1/tenants/{tenant_id}/roles/{role_id} | UpdateRole | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| DELETE | /api/v1/tenants/{tenant_id}/roles/{role_id} | DeleteRole | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |

### Apps (/api/v1/tenants/{tenant_id}/apps)

`RestIAMClient` has no app methods. The rest.ts match is not an IAM app path.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | /api/v1/tenants/{tenant_id}/apps/ | ListApps | rest.ts | not covered |
| POST | /api/v1/tenants/{tenant_id}/apps/ | RegisterApp | rest.ts | not covered |
| GET | /api/v1/tenants/{tenant_id}/apps/{client_id} | GetApp | rest.ts | not covered |
| PUT | /api/v1/tenants/{tenant_id}/apps/{client_id} | UpdateApp | rest.ts | not covered |
| DELETE | /api/v1/tenants/{tenant_id}/apps/{client_id} | DeleteApp | rest.ts | not covered |
| POST | /api/v1/tenants/{tenant_id}/apps/{client_id}/rotate-secret | RotateSecret | - | not covered |

### Policies (/api/v1/tenants/{tenant_id}/policies)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | /api/v1/tenants/{tenant_id}/policies/ | ListPolicies | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| POST | /api/v1/tenants/{tenant_id}/policies/ | CreatePolicy | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| GET | /api/v1/tenants/{tenant_id}/policies/{policy_name} | GetPolicy | interfaces.ts,rest.ts,types.ts | not covered |
| PUT | /api/v1/tenants/{tenant_id}/policies/{policy_name} | UpdatePolicy | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| DELETE | /api/v1/tenants/{tenant_id}/policies/{policy_name} | DeletePolicy | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/IAMView.svelte |
| POST | /api/v1/tenants/{tenant_id}/policies/validate | ValidatePolicy | interfaces.ts,rest.ts | not covered |
| POST | /api/v1/tenants/{tenant_id}/policies/test | TestPolicy | interfaces.ts,types.ts | not covered |
| POST | /api/v1/tenants/{tenant_id}/policies/simulate | SimulatePolicy | - | not covered |
| POST | /api/v1/tenants/{tenant_id}/policies/effective-permissions | GetEffectivePermissions | - | not covered |

GetRole, GetPolicy, ValidatePolicy and TestPolicy have no `RestIAMClient` method.

### Identity providers (/api/v1/tenants/{tenant_id}/providers)

`RestIAMClient` has no provider methods. The rest.ts match is the embeddings `listProviders`.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | /api/v1/tenants/{tenant_id}/providers/ | RegisterProvider | rest.ts | not covered |
| GET | /api/v1/tenants/{tenant_id}/providers/ | ListProviders | rest.ts | not covered |
| GET | /api/v1/tenants/{tenant_id}/providers/{provider_id} | GetProvider | rest.ts | not covered |
| PUT | /api/v1/tenants/{tenant_id}/providers/{provider_id} | UpdateProvider | rest.ts | not covered |
| DELETE | /api/v1/tenants/{tenant_id}/providers/{provider_id} | DeleteProvider | rest.ts | not covered |

### Invites (/api/v1/iam/{tenant_id}/invites)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | /api/v1/iam/{tenant_id}/invites/{token} | GetInvite | interfaces.ts,rest.ts | covered: cmd/quicbaas/frontend/src/views/security/InviteActivateView.svelte |
| POST | /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/begin | ActivateFIDO2Begin | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/InviteActivateView.svelte |
| POST | /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/finish | ActivateFIDO2Finish | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/InviteActivateView.svelte |
| POST | /api/v1/iam/{tenant_id}/invites/{token}/activate/piv | ActivatePIV | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/security/InviteActivateView.svelte |

### User approval (/api/v1/iam/users)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | /api/v1/iam/users/{user_id}/approve | ApproveUser | - | not covered |
| POST | /api/v1/iam/users/{user_id}/reject | RejectUser | - | not covered |

### Access checks (/api/v1/iam/validate-client, /api/v1/iam/check)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | /api/v1/iam/validate-client | ValidateClient | - | not-UI: service-to-service authorization check |
| POST | /api/v1/iam/check | CheckAccess | - | not-UI: service-to-service authorization check |
| POST | /api/v1/iam/check/batch | BatchCheckAccess | - | not-UI: service-to-service authorization check |
| POST | /api/v1/iam/batch-check | BatchCheckAccess | - | not-UI: service-to-service authorization check |

### Audit (/api/v1/iam/audit)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | /api/v1/iam/audit | ListAuditEvents | - | not covered |
| GET | /api/v1/iam/audit/{id} | GetAuditEvent | - | not covered |
| POST | /api/v1/iam/audit | RecordAuditEvent | - | not-UI: services write audit events; an operator does not |
| DELETE | /api/v1/iam/audit | DeleteOldAuditEvents | - | not covered |

### IAM sessions (/api/v1/iam/sessions)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | /api/v1/iam/sessions | ListSessions | - | not covered |
| DELETE | /api/v1/iam/sessions | RevokeAllSessions | - | not covered |
| GET | /api/v1/iam/sessions/{id} | GetSession | - | not covered |
| DELETE | /api/v1/iam/sessions/{id} | RevokeSession | - | not covered |
| POST | /api/v1/iam/sessions/revoke-others | RevokeOtherSessions | - | not covered |

### IAM tokens (/api/v1/iam/login, refresh, validate, logout)

SDK methods are on `RestAuthClient` (`client.auth`): `loginPIV`, `authValidate` and `logout`. The rest.ts match for `/api/v1/iam/refresh` is a comment only. No SDK method posts to it.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | /api/v1/iam/login/piv | PIVLogin | interfaces.ts,rest.ts | covered: cmd/quicbaas/frontend/src/views/orchestrator/LoginView.svelte |
| POST | /api/v1/iam/refresh | RefreshToken | interfaces.ts,rest.ts | not covered |
| POST | /api/v1/iam/validate | ValidateToken | interfaces.ts,rest.ts | covered: cmd/quicbaas/frontend/src/stores/auth.svelte.ts |
| POST | /api/v1/iam/logout | Logout | interfaces.ts,rest.ts | covered: cmd/quicbaas/frontend/src/views/MainApp.svelte |

### Admin assignments and impersonation (/api/v1/admin)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | /api/v1/admin/assignments/ | CreateAdminAssignment | - | not covered |
| GET | /api/v1/admin/assignments/ | ListAdminAssignments | - | not covered |
| GET | /api/v1/admin/assignments/{assignment_id} | GetAdminAssignment | - | not covered |
| PUT | /api/v1/admin/assignments/{assignment_id} | UpdateAdminAssignment | - | not covered |
| DELETE | /api/v1/admin/assignments/{assignment_id} | DeleteAdminAssignment | - | not covered |
| POST | /api/v1/admin/check-permission | CheckAdminPermission | - | not-UI: service-to-service authorization check |
| GET | /api/v1/admin/impersonation/ | ListImpersonations | - | not covered |
| POST | /api/v1/admin/impersonation/start | StartImpersonation | - | not covered |
| POST | /api/v1/admin/impersonation/{session_id}/end | EndImpersonation | - | not covered |
| GET | /api/v1/admin/users/{user_id}/assignments | GetUserAssignments | - | not covered |
| GET | /api/v1/admin/users/{user_id}/impersonations | ListActiveImpersonations | - | not covered |

### OAuth 2.0 dynamic client registration (/oauth2/register)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | /oauth2/register/ | RegisterDynamicClient | - | not-UI: RFC 7591 registration, called by OAuth clients themselves |
| GET | /oauth2/register/{client_id} | GetDynamicClient | - | not-UI: RFC 7591 registration, called by OAuth clients themselves |
| PUT | /oauth2/register/{client_id} | UpdateDynamicClient | - | not-UI: RFC 7591 registration, called by OAuth clients themselves |
| DELETE | /oauth2/register/{client_id} | DeleteDynamicClient | - | not-UI: RFC 7591 registration, called by OAuth clients themselves |

### Discovery metadata (/.well-known)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | /.well-known/openid-configuration | GetOIDCConfig | - | not-UI: discovery document read by OAuth/OIDC clients |
| GET | /.well-known/oauth-authorization-server | GetOAuthASMetadata | - | not-UI: discovery document read by OAuth/OIDC clients |
| GET | /.well-known/oauth-protected-resource | GetProtectedResourceMetadata | - | not-UI: discovery document read by OAuth/OIDC clients |
| GET | /.well-known/jwks.json | GetJWKS | - | not-UI: discovery document read by OAuth/OIDC clients |

### OAuth 2.0 protocol endpoints (/oauth2)

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | /oauth2/authorize | Authorize | - | not-UI: OAuth 2.0 protocol endpoint for relying parties |
| POST | /oauth2/token | Token | - | not-UI: OAuth 2.0 protocol endpoint for relying parties |
| POST | /oauth2/token-mtls | TokenMTLS | - | not-UI: OAuth 2.0 protocol endpoint for relying parties |
| GET | /oauth2/userinfo | UserInfo | - | not-UI: OAuth 2.0 protocol endpoint for relying parties |
| POST | /oauth2/revoke | Revoke | - | not-UI: OAuth 2.0 protocol endpoint for relying parties |
| POST | /oauth2/introspect | Introspect | - | not-UI: OAuth 2.0 protocol endpoint for relying parties |

### Login and self-registration ceremonies (/api/v1/auth)

SDK methods are on `RestAuthClient`: `webAuthnBeginLogin`, `webAuthnFinishLogin`, `registerBegin` and `registerFinish`.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | /api/v1/auth/login/begin | CeremonyBeginLogin | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/orchestrator/LoginView.svelte |
| POST | /api/v1/auth/login/finish | CeremonyFinishLogin | interfaces.ts,rest.ts,types.ts | covered: cmd/quicbaas/frontend/src/views/orchestrator/LoginView.svelte |
| POST | /api/v1/auth/register/begin | RegisterSelfBegin | interfaces.ts,rest.ts | covered: cmd/quicbaas/frontend/src/views/orchestrator/RegisterView.svelte |
| POST | /api/v1/auth/register/finish | RegisterSelfFinish | interfaces.ts,rest.ts | covered: cmd/quicbaas/frontend/src/views/orchestrator/RegisterView.svelte |
| GET | /api/v1/auth/providers/{tenant_id}/{provider_name}/login | BeginFederatedLogin | - | not-UI: browser redirect target of federated login, reached by navigation, not an API call |
| GET | /api/v1/auth/providers/{tenant_id}/{provider_name}/callback | HandleFederatedCallback | - | not-UI: browser redirect target of federated login, reached by navigation, not an API call |

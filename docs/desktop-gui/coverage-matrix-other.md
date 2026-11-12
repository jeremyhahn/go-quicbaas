# Console coverage: REST routes registered outside the route tables

This file covers the REST routes that are not in `pkg/api/rest/routes.go`'s or
`routes_iam.go`'s route tables: the RAG routes (`handlers_rag.go`), the IAM
audit routes (`iam_audit.go`), the routes `IAMHandler.RegisterRoutes` in
`iam.go` registers with chi, and the fleet deploy operation the SDK's
`deployFleetNode` names. A Console cell is `covered: <file>` only when a file
under `cmd/quicbaas/frontend/src` calls the TypeScript SDK method that builds
the path; `not covered` when a console user would use the route and no console
code reaches it; `not-UI: <reason>` when the route is not meant for a console;
`unaudited` when it has not been checked yet (see "Still to audit").
Registered is `mounted` when production code calls the registering function,
`not mounted` when only tests do or nothing does.

## handlers_rag.go

Registered: `ragRouteSpecs()` is appended to the route table at
`routes.go:836`; `handlers_rag.go` builds only under `//go:build rest && rag`
(`rag_stub.go` returns no routes otherwise). SDK methods are on the RAG client
in `sdk/typescript/src/rest.ts` (lines 2197-2240).

| Method | Path | Handler | Registered | SDK method | Console |
|---|---|---|---|---|---|
| POST | /api/v1/rag/context | HandleRAGBuildContext | mounted (routes.go:836, `rag` tag) | `rag.buildContext` | covered: views/ai/RAGView.svelte |
| GET | /api/v1/rag/documents | HandleRAGListDocuments | mounted (routes.go:836, `rag` tag) | `rag.listDocuments` | covered: views/ai/RAGView.svelte |
| DELETE | /api/v1/rag/documents/{id} | HandleRAGDeleteDocument | mounted (routes.go:836, `rag` tag) | `rag.deleteDocument` | covered: views/ai/RAGView.svelte |
| GET | /api/v1/rag/documents/{id} | HandleRAGGetDocument | mounted (routes.go:836, `rag` tag) | `rag.getDocument` | not covered |
| POST | /api/v1/rag/documents/{id}/reindex | HandleRAGReindexDocument | mounted (routes.go:836, `rag` tag) | `rag.reindexDocument` | covered: views/ai/RAGView.svelte |
| POST | /api/v1/rag/query | HandleRAGQuery | mounted (routes.go:836, `rag` tag) | `rag.query` | covered: views/ai/RAGView.svelte |

## iam_audit.go

Registered: `RegisterAuditRoutes` (iam_audit.go:400) is called only by tests
(`audit_routes_test.go`, `iam_sessions_test.go`). The same four method/path
pairs are also registered by `iam.go` (rows 101-104 below).
`sdk/typescript/src/rest.ts` has no audit method.

| Method | Path | Handler | Registered | SDK method | Console |
|---|---|---|---|---|---|
| GET | /api/v1/iam/audit | ListAuditEvents | not mounted (tests only) | none | not covered |
| GET | /api/v1/iam/audit/{id} | GetAuditEvent | not mounted (tests only) | none | not covered |
| POST | /api/v1/iam/audit | RecordAuditEvent | not mounted (tests only) | none | not-UI: services record audit events, not operators |
| DELETE | /api/v1/iam/audit | DeleteOldAuditEvents | not mounted (tests only) | none | not covered |

## iam.go (`IAMHandler.RegisterRoutes`)

Registered: mounted (`rest` build tag). `RegisterRoutes` is called by
`Server.registerIAMRoutes` (iam_register.go:25), which `Server.setupRouter`
calls (server.go:562). `setupRouter` runs from `rest.NewServer`
(server.go:489) and again from `SetSealedWindowGate` (sealed_window.go:74).
Production reaches both through `pkg/server`: `newREST = restapi.NewServer`
(lifecycle_rest.go:61) in `buildRESTServer`, which `ModuleRESTServer`
(lifecycle_rest.go:365) calls and the apimodule wires in at
`apimodule.WithRESTServer` (qrdb_node.go:788), with
`restServer.SetSealedWindowGate` at lifecycle_rest.go:338; and
`startSealedRESTServer` (sealed_window_rest.go:61, :85, called from
sealed_window.go:252). Rows 22-109 are also in routes_iam.go's
`iamManagedRouteSpecs` (`duplicate of routes_iam.go`); rows 1-21 have not
been checked against routes_iam.go's other spec functions yet. Paths below
are the full paths chi composes.

| # | Method | Path | Handler | Registered | SDK method | Console |
|---|---|---|---|---|---|---|
| 1 | GET | /.well-known/openid-configuration | GetOIDCConfig | mounted (ModuleRESTServer) | unaudited | not-UI: OAuth/OIDC protocol endpoint for clients |
| 2 | GET | /.well-known/oauth-authorization-server | GetOAuthASMetadata | mounted (ModuleRESTServer) | unaudited | not-UI: OAuth/OIDC protocol endpoint for clients |
| 3 | GET | /.well-known/oauth-protected-resource | GetProtectedResourceMetadata | mounted (ModuleRESTServer) | unaudited | not-UI: OAuth/OIDC protocol endpoint for clients |
| 4 | GET | /.well-known/jwks.json | GetJWKS | mounted (ModuleRESTServer) | unaudited | not-UI: OAuth/OIDC protocol endpoint for clients |
| 5 | GET | /oauth2/authorize | Authorize | mounted (ModuleRESTServer) | unaudited | not-UI: OAuth/OIDC protocol endpoint for clients |
| 6 | POST | /oauth2/token | Token | mounted (ModuleRESTServer) | unaudited | not-UI: OAuth/OIDC protocol endpoint for clients |
| 7 | POST | /oauth2/token-mtls | TokenMTLS | mounted (ModuleRESTServer) | unaudited | not-UI: OAuth/OIDC protocol endpoint for clients |
| 8 | GET | /oauth2/userinfo | UserInfo | mounted (ModuleRESTServer) | unaudited | not-UI: OAuth/OIDC protocol endpoint for clients |
| 9 | POST | /oauth2/revoke | Revoke | mounted (ModuleRESTServer) | unaudited | not-UI: OAuth/OIDC protocol endpoint for clients |
| 10 | POST | /oauth2/introspect | Introspect | mounted (ModuleRESTServer) | unaudited | not-UI: OAuth/OIDC protocol endpoint for clients |
| 11 | POST | /oauth2/register/ | RegisterDynamicClient | mounted (ModuleRESTServer) | unaudited | not-UI: RFC 7591/7592 client self-registration |
| 12 | GET | /oauth2/register/{client_id} | GetDynamicClient | mounted (ModuleRESTServer) | unaudited | not-UI: RFC 7591/7592 client self-registration |
| 13 | PUT | /oauth2/register/{client_id} | UpdateDynamicClient | mounted (ModuleRESTServer) | unaudited | not-UI: RFC 7591/7592 client self-registration |
| 14 | DELETE | /oauth2/register/{client_id} | DeleteDynamicClient | mounted (ModuleRESTServer) | unaudited | not-UI: RFC 7591/7592 client self-registration |
| 15 | POST | /api/v1/auth/refresh | RefreshToken | mounted (ModuleRESTServer) | none | not-UI: on a composed node xkms owns /api/v1/auth refresh, logout and validate; the console uses the /api/v1/iam session plane |
| 16 | POST | /api/v1/auth/logout | Logout | mounted (ModuleRESTServer) | none | not-UI: on a composed node xkms owns /api/v1/auth refresh, logout and validate; the console uses the /api/v1/iam session plane |
| 17 | POST | /api/v1/auth/validate | ValidateToken | mounted (ModuleRESTServer) | none (`RestAuthClient.authValidate` posts /api/v1/iam/validate) | not-UI: on a composed node xkms owns /api/v1/auth refresh, logout and validate; the console uses the /api/v1/iam session plane |
| 18 | POST | /api/v1/auth/login/begin | CeremonyBeginLogin | mounted (ModuleRESTServer) | `RestAuthClient.webAuthnBeginLogin` | covered: views/orchestrator/LoginView.svelte |
| 19 | POST | /api/v1/auth/login/finish | CeremonyFinishLogin | mounted (ModuleRESTServer) | `RestAuthClient.webAuthnFinishLogin` | covered: views/orchestrator/LoginView.svelte |
| 20 | POST | /api/v1/auth/register/begin | RegisterSelfBegin | mounted (ModuleRESTServer) | `RestAuthClient.registerBegin` | covered: views/orchestrator/RegisterView.svelte |
| 21 | POST | /api/v1/auth/register/finish | RegisterSelfFinish | mounted (ModuleRESTServer) | `RestAuthClient.registerFinish` | covered: views/orchestrator/RegisterView.svelte |
| 22 | POST | /api/v1/iam/login/piv | PIVLogin | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestAuthClient.loginPIV` | covered: views/orchestrator/LoginView.svelte |
| 23 | POST | /api/v1/iam/refresh | RefreshToken | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 24 | POST | /api/v1/iam/validate | ValidateToken | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestAuthClient.authValidate` | covered: stores/auth.svelte.ts |
| 25 | POST | /api/v1/iam/logout | Logout | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestAuthClient.logout` | covered: views/MainApp.svelte |
| 26 | GET | /api/v1/tenants/ | ListTenants | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestIAMClient.listTenants` | covered: views/security/IAMView.svelte |
| 27 | POST | /api/v1/tenants/ | CreateTenant | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestIAMClient.createTenant` | covered: views/security/IAMView.svelte |
| 28 | GET | /api/v1/tenants/{tenant_id} | GetTenant | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 29 | PUT | /api/v1/tenants/{tenant_id} | UpdateTenant | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestIAMClient.updateTenant` | covered: views/security/IAMView.svelte |
| 30 | DELETE | /api/v1/tenants/{tenant_id} | DeleteTenant | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestIAMClient.deleteTenant` | covered: views/security/IAMView.svelte |
| 31 | PATCH | /api/v1/tenants/{tenant_id}/quotas | UpdateTenantEngineQuotas | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 32 | PATCH | /api/v1/tenants/{tenant_id}/entitlements | UpdateTenantEngineEntitlements | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 33 | POST | /api/v1/tenants/{tenant_id}/provision | ProvisionTenant | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 34 | POST | /api/v1/tenants/{tenant_id}/barrier/init | HandleTenantBarrierInit | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestTenantBarrierClient.tenantBarrierInit` | covered: views/security/TenantBarrierView.svelte |
| 35 | POST | /api/v1/tenants/{tenant_id}/barrier/unseal | HandleTenantBarrierUnseal | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestTenantBarrierClient.tenantBarrierUnseal` | covered: views/security/TenantBarrierView.svelte |
| 36 | POST | /api/v1/tenants/{tenant_id}/barrier/unseal-share | HandleTenantBarrierUnsealShare | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestTenantBarrierClient.tenantBarrierUnsealShare` | covered: views/security/TenantBarrierView.svelte |
| 37 | POST | /api/v1/tenants/{tenant_id}/barrier/seal | HandleTenantBarrierSeal | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestTenantBarrierClient.tenantBarrierSeal` | covered: views/security/TenantBarrierView.svelte |
| 38 | GET | /api/v1/tenants/{tenant_id}/barrier/status | HandleTenantBarrierStatus | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestTenantBarrierClient.tenantBarrierStatus` | covered: views/security/TenantBarrierView.svelte |
| 39 | POST | /api/v1/tenants/{tenant_id}/barrier/enable-encryption | HandleTenantBarrierEnableEncryption | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 40 | GET | /api/v1/tenants/{tenant_id}/users/ | ListUsers | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestIAMClient.listUsers` | covered: views/security/IAMView.svelte |
| 41 | POST | /api/v1/tenants/{tenant_id}/users/ | CreateUser | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestIAMClient.createUser` | covered: views/security/IAMView.svelte |
| 42 | GET | /api/v1/tenants/{tenant_id}/users/{user_id} | GetUser | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 43 | PUT | /api/v1/tenants/{tenant_id}/users/{user_id} | UpdateUser | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestIAMClient.updateUser` | covered: views/security/IAMView.svelte |
| 44 | DELETE | /api/v1/tenants/{tenant_id}/users/{user_id} | DeleteUser | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestIAMClient.deleteUser` | covered: views/security/IAMView.svelte |
| 45 | POST | /api/v1/tenants/{tenant_id}/users/{user_id}/roles | AssignRole | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestIAMClient.assignRoleToUser` | covered: views/security/IAMView.svelte |
| 46 | DELETE | /api/v1/tenants/{tenant_id}/users/{user_id}/roles/{role} | RevokeRole | mounted (ModuleRESTServer); duplicate of routes_iam.go | `RestIAMClient.revokeRoleFromUser` | not covered |
| 47 | GET | /api/v1/tenants/{tenant_id}/users/{user_id}/sessions/ | ListUserSessions | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 48 | POST | /api/v1/tenants/{tenant_id}/users/{user_id}/sessions/ | CreateSession | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 49 | DELETE | /api/v1/tenants/{tenant_id}/users/{user_id}/sessions/ | RevokeAllUserSessions | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 50 | GET | /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/ | GetMFAStatus | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 51 | DELETE | /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/ | DisableMFA | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 52 | POST | /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/enroll | BeginMFAEnrollment | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 53 | POST | /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/verify | CompleteMFAEnrollment | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 54 | DELETE | /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/enroll/{enrollment_id} | CancelMFAEnrollment | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 55 | POST | /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/challenge | BeginMFAChallenge | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 56 | POST | /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/verify-challenge | VerifyMFAChallenge | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 57 | GET | /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/backup-codes | GetBackupCodeStatus | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 58 | POST | /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/backup-codes | GenerateBackupCodes | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 59 | GET | /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/ | ListUserWebAuthnCredentials | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 60 | DELETE | /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/ | DeleteUserWebAuthnCredentials | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 61 | DELETE | /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/{credential_id} | DeleteUserWebAuthnCredential | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 62 | POST | /api/v1/tenants/{tenant_id}/users/{user_id}/links/ | LinkAccount | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 63 | GET | /api/v1/tenants/{tenant_id}/users/{user_id}/links/ | GetUserLinks | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 64 | DELETE | /api/v1/tenants/{tenant_id}/users/{user_id}/links/{provider_id} | UnlinkAccount | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 65 | GET | /api/v1/tenants/{tenant_id}/roles/ | ListRoles | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.listRoles` | covered: views/security/IAMView.svelte |
| 66 | POST | /api/v1/tenants/{tenant_id}/roles/ | CreateRole | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.createRole` | covered: views/security/IAMView.svelte |
| 67 | POST | /api/v1/tenants/{tenant_id}/roles/reconcile | ReconcileRoles | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.reconcileRoles` | covered: views/security/IAMView.svelte |
| 68 | GET | /api/v1/tenants/{tenant_id}/roles/{role_id} | GetRole | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 69 | PUT | /api/v1/tenants/{tenant_id}/roles/{role_id} | UpdateRole | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.updateRole` | covered: views/security/IAMView.svelte |
| 70 | DELETE | /api/v1/tenants/{tenant_id}/roles/{role_id} | DeleteRole | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.deleteRole` | covered: views/security/IAMView.svelte |
| 71 | GET | /api/v1/tenants/{tenant_id}/apps/ | ListApps | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 72 | POST | /api/v1/tenants/{tenant_id}/apps/ | RegisterApp | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 73 | GET | /api/v1/tenants/{tenant_id}/apps/{client_id} | GetApp | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 74 | PUT | /api/v1/tenants/{tenant_id}/apps/{client_id} | UpdateApp | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 75 | DELETE | /api/v1/tenants/{tenant_id}/apps/{client_id} | DeleteApp | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 76 | POST | /api/v1/tenants/{tenant_id}/apps/{client_id}/rotate-secret | RotateSecret | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 77 | GET | /api/v1/tenants/{tenant_id}/policies/ | ListPolicies | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.listPolicies` | covered: views/security/IAMView.svelte |
| 78 | POST | /api/v1/tenants/{tenant_id}/policies/ | CreatePolicy | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.createPolicy` | covered: views/security/IAMView.svelte |
| 79 | GET | /api/v1/tenants/{tenant_id}/policies/{policy_name} | GetPolicy | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 80 | PUT | /api/v1/tenants/{tenant_id}/policies/{policy_name} | UpdatePolicy | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.updatePolicy` | covered: views/security/IAMView.svelte |
| 81 | DELETE | /api/v1/tenants/{tenant_id}/policies/{policy_name} | DeletePolicy | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.deletePolicy` | covered: views/security/IAMView.svelte |
| 82 | POST | /api/v1/tenants/{tenant_id}/policies/validate | ValidatePolicy | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 83 | POST | /api/v1/tenants/{tenant_id}/policies/test | TestPolicy | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 84 | POST | /api/v1/tenants/{tenant_id}/policies/simulate | SimulatePolicy | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 85 | POST | /api/v1/tenants/{tenant_id}/policies/effective-permissions | GetEffectivePermissions | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 86 | POST | /api/v1/tenants/{tenant_id}/providers/ | RegisterProvider | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 87 | GET | /api/v1/tenants/{tenant_id}/providers/ | ListProviders | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 88 | GET | /api/v1/tenants/{tenant_id}/providers/{provider_id} | GetProvider | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 89 | PUT | /api/v1/tenants/{tenant_id}/providers/{provider_id} | UpdateProvider | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 90 | DELETE | /api/v1/tenants/{tenant_id}/providers/{provider_id} | DeleteProvider | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 91 | GET | /api/v1/iam/{tenant_id}/invites/{token} | GetInvite | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.getInvite` | covered: views/security/InviteActivateView.svelte |
| 92 | POST | /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/begin | ActivateFIDO2Begin | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.activateFIDO2Begin` | covered: views/security/InviteActivateView.svelte |
| 93 | POST | /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/finish | ActivateFIDO2Finish | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.activateFIDO2Finish` | covered: views/security/InviteActivateView.svelte |
| 94 | POST | /api/v1/iam/{tenant_id}/invites/{token}/activate/piv | ActivatePIV | mounted (ModuleRESTServer); duplicate of routes_iam.go | `iam.activatePIV` | covered: views/security/InviteActivateView.svelte |
| 95 | POST | /api/v1/iam/users/{user_id}/approve | ApproveUser | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 96 | POST | /api/v1/iam/users/{user_id}/reject | RejectUser | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 97 | POST | /api/v1/iam/validate-client | ValidateClient | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not-UI: services call it at request time to check a client or a permission |
| 98 | POST | /api/v1/iam/check | CheckAccess | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not-UI: services call it at request time to check a client or a permission |
| 99 | POST | /api/v1/iam/check/batch | BatchCheckAccess | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not-UI: services call it at request time to check a client or a permission |
| 100 | POST | /api/v1/iam/batch-check | BatchCheckAccess | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not-UI: services call it at request time to check a client or a permission |
| 101 | GET | /api/v1/iam/audit | ListAuditEvents | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 102 | GET | /api/v1/iam/audit/{id} | GetAuditEvent | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 103 | POST | /api/v1/iam/audit | RecordAuditEvent | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not-UI: services record audit events, not operators |
| 104 | DELETE | /api/v1/iam/audit | DeleteOldAuditEvents | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 105 | GET | /api/v1/iam/sessions | ListSessions | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 106 | DELETE | /api/v1/iam/sessions | RevokeAllSessions | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 107 | GET | /api/v1/iam/sessions/{id} | GetSession | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 108 | DELETE | /api/v1/iam/sessions/{id} | RevokeSession | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |
| 109 | POST | /api/v1/iam/sessions/revoke-others | RevokeOtherSessions | mounted (ModuleRESTServer); duplicate of routes_iam.go | none | not covered |

`RegisterRoutes` also calls `RegisterFederationAuthRoutes(router, h)`,
`RegisterGDPRRoutes` (under `/api/v1/tenants/{tenant_id}/users/{user_id}/gdpr`)
and `RegisterAdminRoutes(router, h)`; their routes are not listed yet.

## Fleet deploy

No REST route serves fleet deploy. `RestOrchestratorClient.deployFleetNode`
(`sdk/typescript/src/rest.ts:1178`) builds no path: it throws
`DeployNotSupportedError('rest')`, as the grpc-web, mcp, quic and unix clients
do. Only the Wails client (`sdk/typescript/src/wails.ts:586`) implements it,
through the desktop binding `FleetDeployBinding.Deploy`
(`cmd/quicbaas/bindings_fleet_deploy.go`). No `FleetDeploy` symbol exists
under `pkg/api/rest`.

| Method | Path | Handler | Registered | SDK method | Console |
|---|---|---|---|---|---|
| (none: Wails binding) | (none) | FleetDeployBinding.Deploy | not mounted (no REST registration; Wails binding only) | `orchestrator.deployFleetNode` (Wails only; REST throws) | covered: components/orchestrator/DeployFleetNodeDialog.svelte (desktop transport only) |

## Still to audit

- iam.go rows 1-14: SDK method (`sdk/typescript/src/rest.ts`).
- iam.go rows 1-21: whether `oidcDiscoveryRouteSpecs`, `oauth2CoreRouteSpecs`,
  `oauth2RegisterRouteSpecs` or `apiAuthPublicRouteSpecs` (routes_iam.go)
  register the same method and path (`duplicate of routes_iam.go`).
- Totals (iam.go table, each Console mark counted once per row): covered 34,
  not covered 53, not-UI 22, unaudited 0; 109 rows (POST /api/v1/iam/audit
  is row 103 only; the iam_audit.go table's row is the separate test-only
  registration). SDK cells `none`: rows 15, 16, 23, 28, 31-33, 39, 42, 47-64,
  68, 71-76, 79, 82-90 and 95-109.
- iam.go: the routes `RegisterFederationAuthRoutes`, `RegisterGDPRRoutes` and
  `RegisterAdminRoutes` register.

# Console coverage: federation, GDPR and admin route registrars

This matrix lists the REST routes registered by `RegisterFederationAuthRoutes`
(`pkg/api/rest/iam_federation.go`), `RegisterGDPRRoutes`
(`pkg/api/rest/iam_gdpr.go`) and `RegisterAdminRoutes`
(`pkg/api/rest/iam_admin.go`), which no other coverage matrix lists. Paths
are given in full, with the nested chi prefixes resolved. Registered is
`mounted` when production code calls the registering function: all three are
called from `IAMHandler.RegisterRoutes` (`pkg/api/rest/iam.go`), which
`Server.registerIAMRoutes` (`pkg/api/rest/iam_register.go`) calls, which the
server's router setup calls (`pkg/api/rest/server.go`). A Console cell is
`covered: <file>` only when a named console file calls the TypeScript SDK
method that builds the path; `not covered` when a console user would use the
route and no console code reaches it; `not-UI: <reason>` when the route is
not meant for a console; `unaudited` when the audit has not reached it yet.

## RegisterFederationAuthRoutes

| Method | Path | Handler | Registered | SDK method | Console |
|---|---|---|---|---|---|
| GET | /api/v1/auth/providers/{tenant_id}/{provider_name}/login | BeginFederatedLogin | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | /api/v1/auth/providers/{tenant_id}/{provider_name}/callback | HandleFederatedCallback | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |

## RegisterGDPRRoutes

Prefix: `/api/v1/tenants/{tenant_id}/users/{user_id}/gdpr`.

| Method | Path | Handler | Registered | SDK method | Console |
|---|---|---|---|---|---|
| POST | …/gdpr/consent | GrantConsent | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | …/gdpr/consents | ListUserConsents | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | …/gdpr/consent/{purpose} | GetConsent | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| PUT | …/gdpr/consent/{purpose}/withdraw | WithdrawConsent | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| DELETE | …/gdpr/consent/{purpose} | WithdrawConsent | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| POST | …/gdpr/export | RequestDataExport | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | …/gdpr/export/{export_id} | GetExportStatus | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | …/gdpr/export/{export_id}/download | DownloadExport | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| POST | …/gdpr/deletion | RequestDeletion | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | …/gdpr/deletion/{deletion_id} | GetDeletionStatus | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| DELETE | …/gdpr/deletion/{deletion_id} | CancelDeletion | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |

## RegisterAdminRoutes

| Method | Path | Handler | Registered | SDK method | Console |
|---|---|---|---|---|---|
| POST | /api/v1/admin/assignments/ | CreateAdminAssignment | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | /api/v1/admin/assignments/ | ListAdminAssignments | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | /api/v1/admin/assignments/{assignment_id} | GetAdminAssignment | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| PUT | /api/v1/admin/assignments/{assignment_id} | UpdateAdminAssignment | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| DELETE | /api/v1/admin/assignments/{assignment_id} | DeleteAdminAssignment | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| POST | /api/v1/admin/check-permission | CheckAdminPermission | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | /api/v1/admin/impersonation/ | ListImpersonations | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| POST | /api/v1/admin/impersonation/start | StartImpersonation | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| POST | /api/v1/admin/impersonation/{session_id}/end | EndImpersonation | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | /api/v1/admin/users/{user_id}/assignments | GetUserAssignments | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |
| GET | /api/v1/admin/users/{user_id}/impersonations | ListActiveImpersonations | mounted (IAMHandler.RegisterRoutes) | unaudited | unaudited |

## Still to audit

- SDK method and Console cells of all 24 rows: find the builder of each path
  in `sdk/typescript/src/` (`rest.ts` first), then its callers in
  `cmd/quicbaas/frontend/src/views/`, `src/lib` and `src/stores`.
- Whether `/api/v1/admin` is also served by a module route table on composed
  nodes (`pkg/api/rest/routes_iam.go` lists the delegated-admin and GDPR
  groups) does not change Registered, but may matter to the Console cell.

# Authorization — API

> **Status: NORMATIVE** · Last verified: 2026-10-16
> The capability on every transport, with permissions. Feature folder: `docs/authorization/`.

Authorization is go-qrdb's. go-qrdb owns the policy decision point, the
`resource:action` vocabulary, the built-in roles and the ABAC overlay; see
[go-qrdb auth](../../../go-qrdb/docs/auth/README.md),
[roles](../../../go-qrdb/docs/auth/roles.md) and
[ABAC](../../../go-qrdb/docs/auth/abac.md). The tenant is the subject's OU.
This page covers only what go-quicbaas adds: how each transport maps a request
onto a go-qrdb permission, the roles surface, and the permissions go-quicbaas
contributes to go-qrdb's registry.

## How each transport reaches the decision

| Transport | Mapping | Decision |
|---|---|---|
| REST | `frozenRoutePermissions` (`pkg/api/rest/routes_permissions_generated.go`) gives every route its `RouteAuth` class and `resource:action`; `applyFrozenPermissions` (`pkg/api/rest/routes.go`) applies it, and `pkg/apimodule` registers each route with go-qrdb's API server (`RegisterHTTP`) | go-qrdb's authorizer |
| QUIC | the REST route table carried over HTTP/3; see [multiprotocol-api](../multiprotocol-api/architecture.md#authentication--authorization) | as REST |
| gRPC | go-qrdb RPCs carry their permission on the wire descriptor, read by `pkg/apimodule/module_grants.go`; go-quicbaas RPCs map through `pkg/api/grpc/authz_mapping.go` | **Departure (SPEC D4):** `pkg/api/grpc/authz_interceptor.go` decides locally |
| Unix | delegates to the gRPC services | as gRPC |
| MCP | tool name → `resource:action` in `pkg/api/mcp/authz_qrdb.go`; OAuth 2.1 bearer and audience rules in [mcp-server/api.md](../mcp-server/api.md#authorization) | go-qrdb's authorizer |
| Embedded | none: linking the SDK into a process is the grant (go-quicbaas#329, go-quicbaas#301) | — |

A REST route with no entry in `frozenRoutePermissions` makes
`applyFrozenPermissions` panic, so a new route cannot ship unmapped. The table is hand-maintained;
`TestRoutes_FrozenPermissionsGolden` (`pkg/api/rest/routes_test.go`) diffs it
against `pkg/api/rest/testdata/route_permissions.golden` and
`route_surface.golden` (tag-gated routes go in the `_<tag>.golden` fragments).
Routes classed `RouteAuthPublic` — health and version, OAuth2 client
registration reads, PIV login, token refresh and invite activation — carry
their own credential or none.

Permission strings in the REST and MCP tables are literals rather than go-qrdb's
`Permission*` constants (SPEC D10).

## Roles: `quicbaas.RBAC` — SDK package `sdk/go/client/rbac`

Roles only; users are go-qrdb's and are administered over the IAM surface
([../iam/api.md](../iam/api.md)). Role records are go-qrdb's, reached through
`pkg/iam/qrdb_role_storage.go` (`QRDBRoleStorage` over go-qrdb's `RoleClient`).

| SDK method | REST route (SDK transport) | Served? | gRPC | MCP tool | Embedded |
|---|---|---|---|---|---|
| `AssignRole` | `POST /api/v1/rbac/users/{username}/roles` | **no** | yes | `qrdb_role_assign` | yes |
| `CreateRole` | `POST /api/v1/rbac/roles` | **no** | yes | `qrdb_role_create` | yes |
| `DeleteRole` | `DELETE /api/v1/rbac/roles/{name}` | **no** | yes | `qrdb_role_delete` | yes |
| `ListRoles` | `GET /api/v1/rbac/roles` | **no** | yes | `qrdb_role_list` | yes |
| `RevokeRole` | `DELETE /api/v1/rbac/users/{username}/roles/{role}` | **no** | yes | `quicbaas_role_revoke` | yes |

**Served? no** is a live SDK-to-server mismatch: `sdk/go/transport/rest` sends
these paths and the server registers none of them.

## Permissions go-quicbaas contributes to go-qrdb

go-qrdb does not know the broker's domain verbs. `pkg/permission` holds the one
list of go-quicbaas's contributions (`contributions`): each entry names the
permission, the built-in roles that hold it
(`qrdbsdk.RegisterPermissionRoleGrant`) and its FIPS 140-3 AS10.03 duty
(`qrdbsdk.RegisterPermissionDuty`). `server.NewServer`, the sole composition
root, calls `permission.Register()` before any component starts. A permission
missing from the registry cannot be held by any role: go-qrdb's role store
rejects it, and an endpoint mapped onto it is permanently denied.

Contributed today (the source is authoritative for roles and duties):

| Family | Permissions |
|---|---|
| `attestation` | `verify`, `attest`, `provision` |
| `audit_events` | `record` |
| `sdlc` | `read`, `bind`, `unbind`, `issue_create` |
| `agents` | `read`, `write` |
| `prompts` | `read`, `write`, `history` ([prompt-sets](../software-factory/prompt-sets.md#permissions)) |
| `cost_settings` | `write` |
| `worker_images` | `read`, `refresh` |
| `agent_accounts` | `read`, `list`, `materialize` |
| `spend_policy` / `spend_report` | `write` / `read` |
| `placement` | `read`, `place`, `manage` |
| `service_registry` | `register`, `read` |

Role grantability, scope and the Security Officer's separated duties are
go-qrdb's rules; see [roles](../../../go-qrdb/docs/auth/roles.md#separation-of-duties).

Guard tests make an unregistered mapping a test failure:

| Transport | Test |
|---|---|
| REST | `TestRESTPermissions_EveryMappingIsRegistered` (`pkg/api/rest/authz_permission_registration_test.go`) |
| gRPC | `TestGRPCPermissions_EveryMappingIsRegistered` (`pkg/api/grpc/authz_permission_registration_test.go`) |
| MCP | `TestMCPPermissions_EveryMappingIsRegistered` (`pkg/api/mcp/authz_qrdb_coverage_test.go`) |

`TestNewServer_RegistersQuicBaaSPermissions` (`pkg/server`) proves production
registers them too; the `TestRegister_*` and `TestContributions_*` tests in
`pkg/permission` read each grant and duty back from go-qrdb's live registry.

## See also

- [CLI](cli.md) · [architecture](architecture.md) · [ABAC policies](engineering.md)
- [MCP authorization](../mcp-server/api.md#authorization) — OAuth 2.1 for MCP

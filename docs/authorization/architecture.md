# Authorization — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-16
> Design, component boundaries and data flow. Feature folder: `docs/authorization/`.

Authorization is go-qrdb's. go-qrdb owns the policy decision point: the RBAC
authorizer, the roles and `resource:action` permissions, and the ABAC decorator
that overlays it (explicit deny overrides, explicit allow grants beyond RBAC,
no match falls through to RBAC). See
[go-qrdb auth](../../../go-qrdb/docs/auth/README.md#rbac-authorizer),
[ABAC decorator](../../../go-qrdb/docs/auth/abac.md#decorator-semantics) and
[layering](../../../go-qrdb/docs/auth/layering.md). The tenant is the subject's
OU.

This page covers only what go-quicbaas adds: where each transport hands the
request to go-qrdb, and the tenant-scoped ABAC policy store that feeds go-qrdb's
decorator.

## Where authorization is enforced

Every network transport is an authorization boundary; the in-process embedded
transport is not, because linking the SDK into a process is the grant
(go-quicbaas#329, go-quicbaas#301). How each transport maps a request onto a
go-qrdb permission is in [api.md](api.md#how-each-transport-reaches-the-decision).

```
REST · QUIC · MCP ─► resource:action ─► go-qrdb authorizer (RBAC + ABAC decorator)
gRPC · Unix       ─► resource:action ─► pkg/api/grpc/authz_interceptor.go   (SPEC D4)
```

**Departure (SPEC D4):** the gRPC interceptor decides locally instead of
calling go-qrdb's authorizer.

## The ABAC policy store

go-qrdb evaluates ABAC policies but does not store them per tenant. go-quicbaas
keeps them and pushes the full set into go-qrdb's live decorator:

| Piece | Source | Role |
|---|---|---|
| `iam.PolicyEngine` | `pkg/iam/policy.go` | policy CRUD, validation, test, simulate, effective permissions |
| `iam.PolicyStorage` | `pkg/iam/storage.go`, `BackendStorage` in `storage_backend.go` | policies persisted per tenant in the IAM store |
| `qrdbABACSync` | `pkg/server/abac_sync.go` | after every create, update and delete, and once at startup, reloads go-qrdb's decorator with every tenant's policies plus the system tenant's |
| `policyToABAC` | `pkg/server/abac_sync.go` | field-for-field copy of `iam.Policy` onto `qrdbsdk.ABACPolicy` |
| `qrdbPolicySimulator` | `pkg/server/abac_simulator.go` | evaluates one draft policy in a scratch decorator; nothing is stored |
| `qrdbPEPRBACAuthorizer` | `pkg/server/pep_rbac_authorizer.go` | `PolicyEngine.Evaluate` calls go-qrdb's `RBAC().Authorize` on the embedded node |

`server.initializeIAMComponents` (`pkg/server/iam_init.go`) wires the engine
and runs the initial `SyncABAC` after unseal, before the first request. A
reload go-qrdb rejects leaves the previous policy set in force
([live reload](../../../go-qrdb/docs/auth/abac.md#live-reload)).

The policy document is go-qrdb's schema
([policy schema](../../../go-qrdb/docs/auth/abac.md#policy-schema)).
`iam.Policy.Validate` adds go-quicbaas's own checks: `apiVersion`,
`metadata.name` and `metadata.tenant` are required, `kind` must be `Policy`,
and there must be at least one rule.

### What reaches the decision

`subjectToQRDBIdentity` passes go-qrdb the subject ID, the tenant, the roles
and the string-valued subject attributes. No request context (IP, time,
protocol, resource attributes) is passed, so policy conditions on request
context have nothing to match.

## Agent access control

Disabling and enabling a fleet node is the agent-fleet feature; see
[agent-fleet API](../agent-fleet/api.md).

## See also

- [API](api.md) · [CLI](cli.md) · [engineering](engineering.md) · [user guide](user.md)
- [IAM policy API](../iam/api.md#policy-api) — the policy and PEP routes

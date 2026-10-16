# Authorization — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-16
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/authorization/`.

The ABAC policy language, its condition operators, variables, resource
patterns and evaluation order are go-qrdb's; see
[policy schema](../../../go-qrdb/docs/auth/abac.md#policy-schema) and
[decorator semantics](../../../go-qrdb/docs/auth/abac.md#decorator-semantics).
This page covers the go-quicbaas policy engine that stores policies per tenant
and feeds them to go-qrdb ([architecture](architecture.md#the-abac-policy-store)).

## Exported surface: `iam.PolicyEngine` (`pkg/iam/policy.go`)

| Method | Contract |
|---|---|
| `CreatePolicy`, `UpdatePolicy`, `DeletePolicy` | validate (on create and update the policy's tenant must equal the request tenant), persist through `PolicyStorage`, call `ABACSync.Sync`, then invalidate the tenant's cache; if the sync fails, the storage write is undone and the sync error returned |
| `GetPolicy`, `ListPolicies` | read from `PolicyStorage` |
| `ValidatePolicy` | `nil` → `*InvalidPolicyError`; otherwise `Policy.Validate` |
| `Evaluate`, `TestPolicy` | delegate to `RBACAuthorizer.Authorize`, the go-qrdb decision; a missing subject ID or authorizer is a deny, not an error |
| `SimulatePolicy` | delegates to `PolicySimulator`; with none configured returns `*PolicySimulatorUnavailableError` |
| `GetEffectivePermissions` | one `RBACAuthorizer.Authorize` per resource pattern and action; actions default to `create`, `read`, `update`, `delete`, `list`, `admin`, `*` |
| `SyncABAC` | full reload of go-qrdb's decorator; `server.initializeIAMComponents` calls it once at startup |

The three seams carry no go-qrdb import into `pkg/iam`; `pkg/server` injects
them (`pkg/server/iam_init.go`):

| Seam | Production implementation |
|---|---|
| `RBACAuthorizer` | `qrdbPEPRBACAuthorizer` → the embedded node's `RBAC().Authorize` |
| `ABACSync` | `qrdbABACSync` → go-qrdb's ABAC reloader, every tenant plus `qrdbsdk.SystemTenantID` |
| `PolicySimulator` | `qrdbPolicySimulator` → a scratch decorator holding the one draft policy |

## Invariants

- go-qrdb makes every decision; `pkg/iam` never evaluates a rule itself.
- go-qrdb's decorator holds the full stored set after a successful mutation.
  A reload go-qrdb rejects leaves the previous set in force, and the engine
  undoes the storage write. The undo's own error is discarded.
- `policyToABAC` copies `iam.Policy` onto `qrdbsdk.ABACPolicy` field for field;
  a field added to one schema must be added to the other.
- Only the subject ID, tenant, roles and string-valued attributes reach go-qrdb
  (`subjectToQRDBIdentity`); there is no request context.

## Errors

| Error | When |
|---|---|
| `*InvalidPolicyError` | `ValidatePolicy(nil)`, or the policy's tenant differs from the request tenant |
| `*PolicyExistsError` | `CreatePolicy` with a name already stored for the tenant |
| `IAMError` `ErrPolicyInvalid` | `Policy.Validate`: no `apiVersion`, `kind` not `Policy`, no `metadata.name` or `metadata.tenant`, no rules, or an invalid rule (with `index` and `rule_name` context) |
| `IAMError` `ErrInvalidCheckRequest` | `GetEffectivePermissions` with no request, subject or resource patterns |
| `*PolicySimulatorUnavailableError` | `SimulatePolicy` with no simulator |
| `*QRDBNodeClientUnavailableError` | `qrdbPEPRBACAuthorizer` before the embedded node is up |

## Configuration

None. The engine has no authorization settings: the server always composes it
over the embedded go-qrdb node.

## Tests

- `pkg/iam/policy_test.go`: `TestPolicyEngine_*` for CRUD, validation,
  `Evaluate`, `TestPolicy` and cache invalidation.
- `pkg/iam/session_test.go`: `TestPolicyEngine_SimulatePolicy_*` and
  `TestPolicyEngine_GetEffectivePermissions_*`.
- The `pkg/server` adapters (`abac_sync.go`, `abac_simulator.go`,
  `pep_rbac_authorizer.go`) have no unit tests.

## See also

- [Architecture](architecture.md) · [API](api.md) · [CLI](cli.md) · [user guide](user.md)

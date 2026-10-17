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

## Write-Time Limits

go-qrdb's ABAC engine evaluates flat expression conditions only, and a stored
policy has one `spec.subjects` selector, no targets, no policy-level effect
and no enabled flag: it is enabled and denies by default. QuicBaaS refuses a
policy write it cannot hand to the engine whole, because storing part of a
policy would grant more than the policy says. A refused write stores nothing.
Request formats are in the [API reference](api.md).

Server transports are REST, gRPC, MCP and the SDK's embedded transport. The
SDK's QUIC transport rides REST and its Unix transport rides gRPC, so each
refuses what the transport under it refuses.

- **Nested conditions.** A `when` or `unless` entry that sets `and`, `or` or
  `not` fails on create, update and import on every transport: every write
  ends in `PolicyEngine` → `Policy.Validate`. The error is
  `ErrConditionNested` (`ErrValidation` class); `*iam.NestedConditionError`
  names the field and index. REST returns 400, gRPC `InvalidArgument`, MCP
  and embedded invalid input. Write the logic inline in one expression:
  `subject.role == admin or resource.owner == subject.id`.
- **Strict `spec_yaml`.** `iam.ParsePolicyYAML` decodes with known keys only,
  on REST create and update, gRPC create and `ValidatePolicy`, MCP create,
  validate and update, and embedded create. An unknown key, a typo, `all:`/`any:`/`none:` (also
  `none:` next to an `expression`), rule-level `subjects:` and a bare-string
  condition fail with `*iam.PolicyYAMLError` (`ErrPolicyInvalid`,
  `ErrValidation` class); the client status is as for nested conditions.
  The YAML is a full document only when it sets both `apiVersion` and
  `kind`, otherwise it is the `spec` alone; name, tenant and description
  always come from the request. gRPC `ValidatePolicy` returns a refused
  document as invalid, with the reason naming the key.
- **Stored records load strictly.** `BackendStorage` decodes a stored policy
  record with known keys only. A record with unknown keys, or one that does
  not decode, is never applied, so it denies. Storage `GetPolicy` returns
  `*iam.StoredPolicyDecodeError` (`ErrPolicyRecordInvalid`, `ErrInternal`
  class). The engine's `GetPolicy` and `ListPolicies` return a flagged entry
  in its place (its name, no rules, `Policy.LoadError` set) instead of
  dropping it. Each load logs a WARN naming the tenant, the policy and the
  unknown keys. The ABAC sync never converts a flagged entry's rules: it
  stands in a deny-all policy (rule `stored-record-not-applied`) scoped to
  the record's tenant, so every request in that tenant is denied until the
  record is repaired or deleted. A record whose tenant is unknown is scoped
  to the system tenant and denies everywhere. A simulate on a flagged entry
  denies, and an export refuses the tenant with the decode error. The engine
  refuses to update with a flagged entry itself (a get-then-patch writer
  cannot write it back); an update with a corrected document, a batch import
  of the same name with `UpdateExisting`, and a delete all still reach the
  record.
- **Finding and repairing a flagged record (operators).** Find it in two
  places. One is the WARN log line naming the tenant, the policy and the
  unknown keys. The other is the policy get and list responses, which carry
  `load_error` with `reason` and `unknown_keys` (key paths such as
  `spec.rules[0].bogus`, never values) on every transport: REST, gRPC
  (`Policy.load_error`), MCP, and embedded, and in the SDK as
  `Policy.LoadError`. A REST get of a flagged record returns 200 with the
  flagged entry. Repair it by sending a corrected document with a policy
  update (REST `PUT`, MCP, the service layer) or a batch import with
  `UpdateExisting`. The record is replaced, a get returns it whole without
  `load_error`, and evaluation applies it. A malformed document is refused
  and the flagged record stays as it was. Or remove the record with a policy
  delete (every transport). gRPC update cannot repair yet, because it does
  not apply `spec_yaml` (#1406). Authorization for these calls
  is go-qrdb's, unchanged.
- **Policy-level effect and enabled.** An effect other than deny, or
  `enabled` other than true, is refused before any request is sent by the
  SDK gRPC and MCP transports (invalid input, from
  `policyfields.Refuse`), and by the server MCP tools
  (`api.InvalidFormatError` on `policy.effect` or `policy.enabled`, invalid
  input). Embedded create refuses an effect other than deny with
  `UnsupportedPolicyFieldError` (invalid input). `CreatePolicyRequest.Enabled`
  is a plain bool, so embedded and the omitted-when-false SDK encoding cannot
  tell `false` from unset; that policy is stored enabled.
- **Structured SDK fields.** The SDK gRPC and MCP transports carry only
  `spec_yaml`, so a create or update that sets `Rules`, `Targets` or
  `Conditions` fails with invalid input naming the field, and no request is
  sent. Embedded create refuses `Conditions`, `Targets` and each rule's
  `Conditions` and `Subjects` with `UnsupportedPolicyFieldError`, even
  alongside `spec_yaml`. REST sends the whole body.
- **`spec_yaml` on a gRPC update.** The SDK gRPC transport refuses it with
  invalid input until the server's `UpdatePolicy` applies it (#1406).
- **Server MCP policy tools.** In the SDK format (no `apiVersion`), a
  `policy` key other than `name`, `description`, `labels`, `priority`,
  `spec_yaml`, `version`, `enabled` and `effect` fails with
  `api.InvalidFormatError` on `policy.<key>` (invalid input). Create and
  validate require `spec_yaml` (`api.RequiredFieldError`), since a rule-less
  policy is never what the caller described. An update without `spec_yaml`
  changes only description, labels and priority.

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

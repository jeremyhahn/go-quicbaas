### Added

- **`Policy.Validate` checks the policy name against `metadata.name`
  (go-quicbaas#1401).** `ValidatePolicyRequest` carries `name = 3`. The
  server compares it with the document's `metadata.name` on gRPC, REST
  (`?name=` on `POST /api/v1/tenants/{tenant_id}/policies/validate`) and
  MCP (a top-level `name` argument); a mismatch returns `valid: false`
  with `*iam.PolicyNameMismatchError`'s text. An empty name is not
  checked. The SDK sends `policy.Name` on gRPC and Unix, REST and QUIC,
  and MCP, and omits it when empty. The embedded transport does not serve
  `Policy.Validate`.

### Fixed

- **The MCP `quicbaas_iam_policy_validate` tool is registered.** The SDK
  mapped `Policy.Validate` onto it, but the server registry never
  declared it. It is now declared with `tenant_id`, `name` and `policy` in
  its schema, and is default-denied like the other policy tools until
  go-qrdb registers a policy write permission family.

### Fixed

- **Policies with nested ABAC conditions are refused, not stored in part
  (go-quicbaas#1401).** go-qrdb's ABAC engine evaluates flat expressions
  only. The `and`, `or` and `not` sub-conditions of a `when` or `unless`
  entry used to be dropped when a policy was synced to qrdb or simulated,
  so the policy granted more than it said. Create, update and import now
  refuse them on every transport with `ErrConditionNested`
  (`ErrValidation`; `*iam.NestedConditionError` names the field and
  index), and nothing is stored. REST returns 400, gRPC `InvalidArgument`,
  and MCP and embedded return invalid input. The sync and the simulator
  return an error instead of dropping a condition.
- **The embedded transport honours `spec_yaml` and refuses fields it
  cannot translate.** Embedded `CreatePolicy` used to ignore `spec_yaml`,
  structured `Conditions`, rule `Subjects` and `Targets`, and stored a
  broader policy. It now parses `spec_yaml` the way REST and gRPC do. It
  refuses any of those structured fields with `UnsupportedPolicyFieldError`
  (invalid input).
- **The SDK gRPC, Unix and MCP transports refuse structured policy
  fields.** They used to drop `Rules`, `Targets` and `Conditions` and send
  only `spec_yaml`. A create or update that sets any of them now fails
  with invalid input naming the field, before any request is sent.
- **Policy-level `effect: allow` and `enabled: false` are refused.** Stored
  policies are enabled and deny by default; the SDK gRPC, Unix, MCP and
  embedded transports used to drop these values. They now fail with
  invalid input.
- **Policy YAML refuses unknown keys.** A misspelt or unsupported key in
  `spec_yaml` used to be ignored; it now fails with `ErrPolicyInvalid`.
- **The MCP policy tools refuse SDK keys they cannot translate**, and
  create and validate require `spec_yaml` instead of storing a policy with
  no rules.
- **SDK policy updates carry `spec_yaml`.** REST sends it and MCP now
  delivers updates in the shape the server tool reads (they used to fail
  with "policy must be an object"). gRPC and Unix refuse a `spec_yaml`
  update, which the server would ignore.

### Changed

- **`spec_yaml` is read as a full policy document only when it sets both
  `apiVersion` and `kind`.** Otherwise it is read as the `spec`. REST,
  gRPC, MCP and embedded now share one parser, `iam.ParsePolicyYAML`.
  Before this change MCP took any YAML with an `apiVersion` as a full
  document. The tenant and name always come from the request. YAML that
  does not parse is refused with `ErrPolicyInvalid`.

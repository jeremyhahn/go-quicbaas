### Changed

- **SDK API change: a policy `version` is refused, not ignored (go-quicbaas#1409).**
  Policies are not versioned and nothing compares and sets on a version.
  Before, every transport accepted a `version` and dropped it silently.
  - SDK `client/iam` `PolicyClient.Update` refuses a non-zero
    `UpdatePolicyRequest.Version` with `PolicyVersionUnsupportedError`
    before sending, on every transport. The doc comments on
    `UpdatePolicyRequest.Version` and `Policy.Version` no longer promise
    optimistic concurrency.
  - REST `POST /policies/` and `PUT /policies/{policy_name}` answer 400
    when the body carries a non-zero `version`, at the top level or inside
    a `policy` object.
  - MCP `quicbaas_iam_policy_create` and `quicbaas_iam_policy_update`
    refuse a non-zero `version` argument or `policy.version` with
    `PolicyVersionUnsupportedError`, and nothing is written.
  - A zero or absent `version` creates and updates as before. gRPC has no
    version field on the wire.

### Fixed

- **Policy writes answer with the stored policy (go-quicbaas#1406).**
  - gRPC `IAMService/UpdatePolicy` parses `spec_yaml` and keeps the stored
    spec when it is absent. Before, a metadata-only update failed
    validation and no update could land. A bad `spec_yaml` is
    `InvalidArgument`, and an unknown policy is `NotFound`.
  - gRPC `CreatePolicy` and `UpdatePolicy`, REST `POST /policies/` and
    `PUT /policies/{policy_name}`, and MCP `quicbaas_iam_policy_update` now
    answer with the policy read back from the store, not the request. The
    response carries the stored timestamps and drops anything the store
    did not keep.
  - MCP `quicbaas_iam_policy_update` targets the policy the request's
    `name` names. Before, it updated whichever policy the document named.
    A document that names a different policy is refused with
    `PolicyNameMismatchError`, and nothing is written.

### Fixed

- **gRPC `ValidatePolicy` decodes with the same strict parser as
  `CreatePolicy` (go-quicbaas#1401).** It used to decode `spec_yaml` with
  a loose YAML unmarshal that dropped unknown keys without a word, so it
  could report a document as valid even though `CreatePolicy` refused it.
  It now uses `iam.ParsePolicyYAML`. A document that the parser refuses
  (an unknown key, `none` next to an expression, a rule-level `subjects`)
  is reported invalid, and the reason names the key.

### Changed

- **Stored policy records load strictly and fail closed
  (go-quicbaas#1401).** A policy record read back from storage is decoded
  with known keys only. A record with unknown keys, or one that does not
  decode, is not applied, so it denies: the ABAC sync does not load it and a
  simulate on it denies. `GetPolicy` returns `*iam.StoredPolicyDecodeError`
  (`ErrPolicyRecordInvalid`), and an export of the tenant is refused with
  that error.
- Each load of such a record is logged at WARN with the tenant, the policy
  name and the unknown keys.
- `ListPolicies` returns the record as a flagged entry in its place. Clients
  see its policy name and that it has no rules. The reason and the keys are
  not on the entry; they are in the WARN log.
- A flagged record is never skipped by the ABAC sync: until it is repaired or
  deleted, every request in its tenant is denied (a record whose tenant is
  unknown denies in every tenant), and the WARN log names the policy.
- A repair or delete of a flagged record that is rolled back after a failed
  ABAC sync restores the record's original stored bytes, so it stays flagged.
- Policy validation with a name and no policy reports the policy as invalid
  instead of panicking.
- MCP policy validate accepts the record metadata an SDK policy carries
  (`tenant_id`, `created_at`, `updated_at`) and still refuses unknown keys
  by name.
- REST policy validate of an SDK-format body checks the `spec_yaml`
  document's own `metadata.name` against `?name=`.
- Update and delete still reach the record, so an operator can repair it
  with an update or remove it with a delete.
- A well-formed stored record (with `when` and `unless` conditions,
  metadata and variables) loads whole and makes the same decisions as the
  same policy built in Go.

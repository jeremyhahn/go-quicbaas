### Added

- **Flagged policy records carry `load_error` on every transport
  (go-quicbaas#1411).** A policy whose stored record does not decode
  strictly is flagged and is not applied. Its get and list entries now carry
  `load_error`: the `reason` and the `unknown_keys` (key paths only, never
  values). This holds on REST, gRPC (new `Policy.load_error` proto field,
  message `PolicyLoadError`), MCP, and the embedded transport. The Go SDK's
  `iam.Policy` gains `LoadError` (`*iam.PolicyLoadError`), and the SDK
  REST, gRPC, MCP and embedded transports all fill it in.
- An operator repairs a flagged record with a policy update carrying a
  corrected document, or with a batch import of the same name and
  `UpdateExisting`. Either one replaces the record, which then loads clean.
  A delete removes it. Service-layer, REST and gRPC delete tests pin this. A
  malformed corrected document is refused, and the flagged record is left
  as it was.

### Changed

- **Policy get returns the flagged entry (go-quicbaas#1411).** The policy
  engine's `GetPolicy` returns the flagged entry (name, no rules, `LoadError`
  set) instead of the decode error, the same entry the list returns. A REST
  get of a flagged record now answers 200 with `load_error`.
- **Update refuses a flagged entry (go-quicbaas#1411).** `UpdatePolicy`
  refuses a policy whose `LoadError` is set, so a get-then-patch writer
  cannot write a flagged entry back over the stored record. A batch import
  now sees a flagged record as existing, so without `UpdateExisting` it
  reports the policy as already present and leaves the record untouched.

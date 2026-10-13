### Changed

- **gRPC sends `rate_limited` as `RESOURCE_EXHAUSTED` (go-quicbaas#158).**
  Before this change a `rate_limited` error reached gRPC clients as
  `INTERNAL`. Every gRPC service now maps `pkg/errors` codes through one
  table; the scheduler, messaging, orchestrator, KMS, IAM, MFA, PEP and
  federation services each used a shorter list of their own. These codes no
  longer fall through to `INTERNAL` anywhere: `not_allowed` and
  `system_keyspace_violation` (`PERMISSION_DENIED`), the
  `missing_*_context` and `missing_justification` codes
  (`INVALID_ARGUMENT`), `not_implemented` (`UNIMPLEMENTED`) and
  `circuit_open` (`UNAVAILABLE`). Two codes change status: `configuration` is
  now `INTERNAL` (was `FAILED_PRECONDITION`) and `out_of_range` is now
  `INVALID_ARGUMENT` (was `OUT_OF_RANGE`), which matches REST.
  The full map is in the gRPC status table of the multi-protocol API
  reference. gRPC carries no retry-after hint.
- **The Go SDK's gRPC transport returns typed errors (go-quicbaas#158).**
  Errors from every gRPC service now carry an SDK error code, so
  `errors.Code` and `errors.IsRetryable` work on them. Before this change
  most of them came back as `unknown`. A `rate_limited` error is the
  retryable `rate_limited` code on gRPC as on REST. `rate_limited` is now
  retryable in the SDK, so the gRPC transport retries it within its
  `MaxRetries` budget. The REST transport does so only for calls with no
  dedicated route handler.

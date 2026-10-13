### Fixed

- **The IAM service wrapper judges a create or update before it looks for
  the identity store (go-quicbaas#335).** On a server with no identity store,
  a malformed `Security().IAM()` user create or update was refused as
  "identity store not available" rather than for its field. The wrapper now
  runs the same field validation as `pkg/iam` (now exported as
  `iam.ValidateCreateUserOptions` / `iam.ValidateUpdateUserOptions`) first.
  A well-formed request with no identity store is still refused as
  unavailable.
- **`make test-iam` no longer times out before its tests finish.** Under
  `-race`, `pkg/iam` takes about 16 minutes, more than `go test`'s default
  10-minute timeout. `test-iam`, `test-iam-coverage` and `test-verbose`
  (which includes `pkg/iam`) now set `-timeout=30m`.

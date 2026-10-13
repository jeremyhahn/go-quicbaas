### Fixed

- **An IAM request with a malformed field answers 400 / `InvalidArgument`,
  not 500 / `Internal` (go-quicbaas#335).** A field the identity store
  refuses (email, username, display name, tenant ID, user ID, role ID, role
  name, and the batch import/export request fields) now answers 400 on REST
  and QUIC and `InvalidArgument` on gRPC and Unix, and the embedded transport
  hands back an `ErrInvalidInput`-coded error. The error names the field, and
  `errors.As` still reaches the `*iam.ValidationError` behind it. The
  validators in `pkg/iam` now report through the same coded error every
  transport already maps, so no transport needed a case of its own.

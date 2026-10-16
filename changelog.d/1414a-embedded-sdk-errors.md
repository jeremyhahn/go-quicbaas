### Fixed

- **Embedded-transport errors carry the same SDK code as on the network
  transports.** Every unary and stream handler error now crosses one boundary
  (`classifyServiceError`) that turns a `pkg/errors` (go-qrdb) error into an
  `sdk/go/errors.QBError` with the code a gRPC caller reads, plus the same Op
  and message. `Unwrap` still returns the original error. An SDK error passes
  through unchanged, the transport's own typed errors and context
  cancellation keep their codes, and any other error is `CodeInternal` with
  the original as its cause. Before this fix, `errors.As(err,
  *sdkerrors.QBError)` found nothing on an embedded call, and the caller read
  INTERNAL for a NOT_FOUND (#1414).

### Changed

- **`sdk/go/transport/grpc.StatusErrorCode`** exports the gRPC-status to SDK
  error-code table. Both the gRPC client and the embedded transport use it,
  so the two transports cannot drift apart.

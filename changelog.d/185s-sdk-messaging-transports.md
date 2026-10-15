### Added

- **The SDK serves messaging on every transport (go-quicbaas#185).**
  `Async().Messaging()` previously reached the server only over REST and MCP.
  The gRPC transport (and Unix, which delegates to it) now maps every
  `quicbaas.Messaging/*` method onto `MessagingService`, and the embedded
  transport registers all twelve methods against `svc.Messaging()`, mapping
  messaging errors to SDK codes (NotFound, AlreadyExists, InvalidInput,
  Unavailable, ResourceExhausted). QUIC carries the REST routes over HTTP/3.
  Each transport has a round-trip test and a test that fails when a
  `messaging.Client` method is missing from its table. The matrix is in
  `docs/messaging/api.md`.

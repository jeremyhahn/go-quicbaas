### Fixed

- **Two context-propagation tests in `pkg/service` can now fail
  (go-quicbaas#508).** `TestSecretsService_GetSecret_ContextTimeout` asserts
  `context.DeadlineExceeded`, and its mock's fallback returns an error that
  names the missing propagation. `TestDistributedSearch_ContextCanceled` uses
  a coordinator timeout longer than the shard delay, so only the caller's
  cancellation can end the search, and it asserts the typed timeout error.
- **`TestBootOrder_CARunsAfterStandaloneNodeInit` checks only boot order
  (go-quicbaas#408).** A failure of `initialize()` before the CA seam is now
  reported as an infrastructure failure, with the real error. The test also
  turns off the embedded node's REST, gRPC and MCP listeners, so it binds no
  port and cannot collide with other jobs on a shared CI host.

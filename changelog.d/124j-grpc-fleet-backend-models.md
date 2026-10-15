### Added

- **The Go SDK's `ListBackendModels` now works over gRPC, QUIC and Unix
  (go-quicbaas#124).** `OrchestratorService` gains a `ListBackendModels` RPC.
  It requires `orchestrator:backend_list`, as the REST route
  `GET /api/v1/fleet/{nodeID}/backends/{backendName}/models` does, and is
  served by the same service call. An unknown backend returns `NotFound`, and
  a missing name returns `InvalidArgument`. The SDK's gRPC
  transport, and the Unix transport built on it, now route the verb. QUIC
  already reached it through the REST route, and now has a test that proves it.

### Fixed

- **gRPC orchestrator RPCs answer an unknown backend with `NotFound`
  (go-quicbaas#124).** `SpawnSession`, `PromptSession`, `SessionStatus`,
  `CloseSession` and `RotateSessionAccount` used to return `Internal` when the
  named backend was not registered. They now return `NotFound`, as REST (404),
  MCP (not found) and the gRPC `ListBackendModels` RPC already did, even when
  the service wraps the error. The mapping now lives in one place for every
  orchestrator RPC.

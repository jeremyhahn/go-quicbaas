### Added

- **The Go SDK's workflow CRUD verbs now work on every transport
  (go-quicbaas#124).** `CreateWorkflow`, `GetWorkflow`, `UpdateWorkflow` and
  `DeleteWorkflow` used to be served only over MCP. The REST and QUIC
  transports now map them onto the existing `/api/v1/orchestrator/workflows`
  routes. `OrchestratorService` gains the four RPCs, and the gRPC and Unix
  transports call them. The embedded transport calls the agent service
  directly. Permissions match REST: `orchestrator:workflow_read` for Get and
  `orchestrator:workflow_write` for the rest.

### Fixed

- The REST orchestrator routes now return 400 for an invalid workflow
  definition. They used to return 500.

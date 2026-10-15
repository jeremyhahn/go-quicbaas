### Added

- **The Go SDK's workflow CRUD verbs now work over MCP (go-quicbaas#124).**
  `CreateWorkflow`, `GetWorkflow`, `UpdateWorkflow` and `DeleteWorkflow` had
  no MCP tool. The server now registers `quicbaas_orchestrator_create_workflow`,
  `quicbaas_orchestrator_get_workflow`, `quicbaas_orchestrator_update_workflow`
  and `quicbaas_orchestrator_delete_workflow`. Get requires
  `orchestrator:workflow_read`; the other three require
  `orchestrator:workflow_write`, as the REST routes do. The tools return the
  definition as `workflow`, the same shape as REST, and the SDK's MCP
  transport decodes it into a `WorkflowDefinition`.

### Fixed

- MCP tools now report a missing workflow as not found and a duplicate
  workflow as already exists. Both used to come back as internal errors.

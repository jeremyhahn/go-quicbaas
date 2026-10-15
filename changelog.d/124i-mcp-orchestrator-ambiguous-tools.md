### Added

- **The Go SDK's `GetFleetNode` and `ListBackendModels` now work over MCP
  (go-quicbaas#124).** `GetFleetNode` takes the same arguments as the existing
  `quicbaas_fleet_status` tool, so the SDK's MCP transport now calls that tool
  and reads the `node` field of its result. `ListBackendModels` had no tool.
  The server now registers `quicbaas_orchestrator_list_backend_models`
  (`backend_name`, optional `node_id` and `tenant_id`), which requires
  `orchestrator:backend_list` as the REST route does and returns the same
  `backend` / `models` / `count` shape.

### Fixed

- MCP tools now report a missing fleet node or agent backend as not found.
  Both used to come back as internal errors.

### Fixed

- **Seven Go SDK orchestrator verbs now work over MCP (go-quicbaas#124).**
  `RegisterFleetNode`, `RemoveFleetNode`, `DisableFleetNode`,
  `EnableFleetNode`, `CreateTask`, `DecomposeTask` and `GetTaskArtifact` had
  server tools, but the tools are named after the surface
  (`quicbaas_fleet_register`, `quicbaas_orchestrator_task_create`,
  `quicbaas_task_get_artifact`, ...). The SDK's MCP transport built a different
  name from the RPC, so each call failed with "tool not found". The transport
  now calls each tool by its real name. It also translates each tool's result
  into the SDK response:
  - the RFC 3339 timestamp from decompose;
  - artifact content sent as text, not base64.

  `quicbaas_fleet_register` now takes `model` and `metadata`, which REST and
  gRPC already took. `quicbaas_orchestrator_task_create` now returns the full
  task record under `task`, next to its summary fields. Over MCP, a failed
  create now comes back to the SDK as an error, as it does over REST.
  `ListBackendModels` stays unmapped. `quicbaas_orchestrator_list_backends`
  lists backends, not the models of one backend, so it does not fit that verb.

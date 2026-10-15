### Added

- **Five more Go SDK orchestrator verbs now work over MCP (go-quicbaas#124).**
  `GetTask`, `ClaimTask`, `CompleteTask`, `SubmitTaskArtifacts` and
  `SpawnChildJob` had no MCP tool. The server now registers
  `quicbaas_orchestrator_task_get`, `quicbaas_orchestrator_task_claim`,
  `quicbaas_orchestrator_task_complete`, `quicbaas_task_submit_artifacts` and
  `quicbaas_orchestrator_spawn_child_job`. Each one requires the permission
  that the gRPC method already requires. The SDK's MCP transport calls them by
  name and decodes their results into the SDK responses. Inline artifact
  `content` is base64, as it is on REST.

### Fixed

- The fleet disable and enable tools now return snake_case keys (`node_id`,
  `disabled`, `disabled_at`, `enabled`, `timestamp`). They used to return
  PascalCase. The SDK's translation of the PascalCase keys has been removed.

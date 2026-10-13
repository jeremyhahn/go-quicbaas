### Changed

- **The TypeScript SDK's MCP transport now carries the orchestrator family
  (go-quicbaas#809).** `McpClient.orchestrator` calls the server's
  `quicbaas_orchestrator_*`, `quicbaas_fleet_*` and
  `quicbaas_sdlc_worker_image*` tools. It covers jobs, fleet, sessions, spend
  and cost settings, backends, runners, health, inference, the workflow list,
  task create/readmit/delete, attention, and the worker image. A verb with no
  server tool now rejects with `TransportNotSupportedError` instead of
  `McpNotImplementedError`, and so does an argument the tool does not read.
  The verbs without a server tool are task list/get/claim/complete/decompose,
  workflow create/get/update/delete, `listBackendModels` and the three
  streaming verbs. `PromptSessionRequest`, `CloseSessionRequest` and
  `RotateSessionAccountRequest` gain an optional `tenant_id`, which the MCP
  session tools require. The session, job, fleet and workflow page listings
  pass their filters, `limit`, `offset`, `sort` and `q` to the tool. They
  return the server's page, with its `total` and `next_offset`, as REST does.
  The verb list is in `docs/software-factory/api.md`.

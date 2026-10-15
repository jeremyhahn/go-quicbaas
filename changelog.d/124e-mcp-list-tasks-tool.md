### Fixed

- **The Go SDK's `ListTasks` works over MCP (go-quicbaas#124).** The server
  now registers `quicbaas_orchestrator_list_tasks`, the tool name the SDK's MCP
  transport derives for `ListTasks`; before, the call failed with "tool not
  found". The tool takes the same filters, paging and facets as
  `GET /api/v1/orchestrator/tasks` and calls the same `ListTasksPage` service
  method. It requires `orchestrator:task_list`, the permission the gRPC
  `ListTasks` RPC declares. The cross-transport facet parity test now checks
  tasks over MCP too.

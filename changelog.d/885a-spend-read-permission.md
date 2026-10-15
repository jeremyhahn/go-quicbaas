### Security

- **The spend report takes its own permission, `spend_report:read`
  (go-quicbaas#885).** `GET /api/v1/orchestrator/cost`, gRPC/Unix
  `OrchestratorService/GetSpendReport` and MCP `quicbaas_orchestrator_cost`
  were gated on `orchestrator:status`, the verb for reading one session's
  status, so a role holding only that read the tenant's whole attribution
  tree and its pooled-account roster. They now require `spend_report:read`,
  registered tenant-scoped and granted to Admin and Operator, the holders of
  the account roster read `agent_accounts:list`. A custom role that holds
  `orchestrator:status` and should keep the report needs `spend_report:read`
  added.

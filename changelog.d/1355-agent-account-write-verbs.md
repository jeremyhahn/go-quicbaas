### Fixed

- **A tenant admin can manage its own tenant's agent-account pool.** The
  account writes no longer borrow go-qrdb's cross-tenant `credentials:*`
  family, which no tenant principal could satisfy. They take QuicBaaS's own
  tenant-scoped verbs, held only by the tenant-admin role:
  `agent_accounts:write` (create, update, set-default, revoke),
  `agent_accounts:rotate` (rotate) and `agent_accounts:delete` (delete).
  REST, gRPC and MCP all map to them. Revoke is a write, because it keeps the
  record and its usage history. Operator and User are refused, and a tenant
  admin still cannot write another tenant's pool (go-quicbaas#1355).

### Fixed

- **`barrier enable-encryption --transport rest` works, and `--tenant`
  migrates that tenant.** The go-qrdb wire mount
  `POST /v1/barrier/enable-encryption` was bound to the quicbaas `/api/v1`
  handler, so go-qrdb's REST transport — the client behind the CLI verb and
  `Security().Barrier().BarrierEnableEncryption` — could not drive it: the
  handler required `target_strategy` where go-qrdb sends `strategy`
  (`validation error: target_strategy is required`), answered `204` where the
  client decodes `{status, scope}` (a JSON EOF even on success), and silently
  dropped the body `tenant_id`, so a tenant migration ran against the SYSTEM
  barrier. The mount now has its own handler that speaks go-qrdb's wire
  verbatim: `strategy`, body `tenant_id` as the scope (empty = system), and
  `200 {"status":"migration_started","scope":"system"|"tenant"}`. It
  authorizes the scope the body selects: `seal:enable_encryption` against the
  system tenant, or `tenant_barrier:enable_encryption` against the named
  tenant. The decision is the embedded qrdb node's own authorizer, so role
  grants and a platform app token's `scp` scopes are both honoured. A tenant
  admin can migrate its own barrier, but not the system barrier or another
  tenant's. The route is registered identity-gated, and the
  service still binds a tenant-scoped migration to the caller's own tenant
  (#990).
- **`POST /api/v1/barrier/enable-encryption` rejects a body `tenant_id`.**
  The route's scope is its path — the system barrier — and it used to drop a
  body `tenant_id` silently and migrate the system barrier anyway. It now
  answers `400` without touching the barrier; per-tenant migrations use
  `/api/v1/tenants/{tenant_id}/barrier/enable-encryption` (#990).

### Changed

- **`quicbaas barrier enable-encryption --tenant` refuses a wrong-scope
  answer.** If the server reports it migrated any scope other than `tenant`,
  the verb fails with `BarrierScopeMismatchError` instead of reporting another
  barrier's migration as success.
- The quicbaas-owned surfaces are unchanged by owner ruling: `/api/v1` system
  and per-tenant routes, gRPC `api/proto/barrier.proto` and the MCP tools keep
  `target_strategy` and their existing responses. `docs/barrier/cli.md`,
  `docs/barrier/api.md`, `docs/barrier/engineering.md` and
  `docs/multi-tenancy/engineering.md` document both shapes.

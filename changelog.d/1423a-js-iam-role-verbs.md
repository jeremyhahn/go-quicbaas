### Added

- **JavaScript `IAMClient.assignRole` / `revokeRole` (#1423).** The JS SDK's
  only role-assignment verbs lived on `RBACClient`, whose `/api/v1/rbac/*`
  routes quicbaasd never served. `db.iam().assignRole(tenantId, userId,
  roleId)` now sends `POST /api/v1/tenants/{tenant_id}/users/{user_id}/roles`
  with `{"role_id": roleId}`, and `db.iam().revokeRole(tenantId, userId,
  roleId)` sends `DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/roles/{role}`,
  the routes the server serves and the TypeScript SDK already uses. Missing
  arguments are refused with `ValidationError` before any request is sent.

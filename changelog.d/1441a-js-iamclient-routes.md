### Fixed

- **The JavaScript SDK's `IAMClient` calls routes the server serves
  (go-quicbaas#1441).** Every method built its path from `/api/v1/iam/tenants/...`,
  which no server route matches. Tenant, user, role, policy, app, session and
  GDPR methods now call `/api/v1/tenants/{tenant_id}/...`, and `getAuditLog`
  calls `/api/v1/iam/audit`. Request bodies now carry the fields the REST
  handlers read, and list methods page with `limit`/`offset`. Signatures that
  changed to take a field the handler requires: `createTenant(tenantId, name,
  options)`, `createUser(tenantId, email, options)`, `createApp(tenantId, name,
  redirectUris, options)`, `createSession(tenantId, userId, tokenHash,
  options)` and `listSessions(tenantId, userId, options)`.

### Removed

- **`IAMClient` group methods** (`createGroup`, `getGroup`, `updateGroup`,
  `deleteGroup`, `listGroups`) and the `Group` typedef. The server serves no
  group routes.

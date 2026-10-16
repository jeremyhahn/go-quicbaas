### Fixed

- **The Go SDK's RBAC verbs work over REST** (#1400). The REST transport sent
  `CreateRole`, `ListRoles`, `DeleteRole`, `AssignRole` and `RevokeRole` to
  `/api/v1/rbac/*`, which the server does not serve, so every call returned
  404. They now reach the tenant-scoped IAM routes under
  `/api/v1/tenants/{tenant_id}/`. The tenant comes from the request context,
  then from `rest.WithDefaultTenant`. `CreateRole` uses the role name as the
  role ID, and `AssignRole` / `RevokeRole` need the numeric user ID as
  `Username`; a missing tenant or a non-numeric username is `INVALID_INPUT`.
  Known gap: the JavaScript SDK's `RBACClient` (`sdk/javascript/src/clients/rbac.js`)
  still calls `/api/v1/rbac/*` and is unchanged here; moving it onto the
  tenant routes changes its method signatures and waits on an architecture
  decision.

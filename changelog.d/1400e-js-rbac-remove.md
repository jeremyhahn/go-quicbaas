### Removed

- **The JavaScript SDK's `RBACClient` and `db.rbac()` are gone** (pre-v1
  removal, go-quicbaas#1400). They called `/api/v1/rbac/*`, which quicbaasd
  does not serve. Role definitions (`createRole`, `listRoles`, `updateRole`)
  remain on `IAMClient`.
